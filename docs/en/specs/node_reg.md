# Node Registration Feature Design

## 1. Overview

This document describes the design for node registration on the ESP RainMaker Neo platform.
Registration is the act of admitting a hardware node — identified by a unique
node ID and an X.509 client certificate — into a RainMaker deployment so that
end users can claim, control, and manage it.

Registration is exposed as an **asynchronous bulk job**: the operator uploads
a CSV (one row per node) to S3 and submits a job request. A long-running ECS
Fargate task drains the CSV in parallel, calls AWS IoT Core / DynamoDB / IoT
Device Shadow for each row, and writes per-job aggregates and per-failure
details back to DynamoDB. The job also emits a re-uploadable, cert-bearing
CSV of the failed rows to S3. Operators poll a status endpoint and, if some
rows failed, read the audit failure list from a companion endpoint and
download the failed-rows CSV via a presigned URL returned on the status.

The same job machinery — same container image, same job/failure tables, same
status surface — also powers **update jobs**, which mutate metadata (tags,
admin groups) on already-registered nodes and replace mistakenly-registered
certificates.

### Key Design Decisions

1. **Async bulk jobs, not synchronous APIs.** A single job can carry thousands
   of nodes. Lambda's 29 s timeout is not the right budget; Fargate is.
2. **Separate endpoints per intent — registration jobs vs. update jobs.**
   Each has different preconditions, IAM, and failure semantics. Flags on a
   single endpoint (`?force=true`, `?update_nodes=true`) would create a mode
   matrix where the same CSV column has different validity rules in different
   cells.
3. **Two-layer retry posture.** Transient AWS errors are absorbed by an
   SDK-level retryer (with adaptive client-side rate limiting) configured
   once in the shared AWS-client configuration. Per-step idempotency inside
   the per-node registration routine makes re-uploading a CSV safe.
4. **Per-node failure visibility via a shared DynamoDB table.** One row per
   failed node, partitioned by `request_id`. No truncation, no per-job cap.
   The same table backs both registration and update jobs.
5. **Re-uploadable failed-nodes CSV, written eagerly to S3.** At end-of-job
   the container writes the original input CSV filtered to the failed
   `node_id`s — certs and all columns intact — to S3, and records its key on
   the job row. Retry is then a two-call workflow with no dedicated retry
   endpoint: download the CSV, re-submit it. Writing it once at job time (not
   regenerating it per request through a Lambda) removes the response-payload
   ceiling that bounds an in-Lambda export, so failure sets of any size round
   -trip cleanly.
6. **The job record carries `job_type`.** `register` or `update`. The status
   and list endpoints are shared; the action surface (`POST /registration-jobs`
   vs. `POST /update-jobs`) is split.

---

## 2. Background

### 2.1 What a node is

A node is a hardware device addressed by a unique `node_id` (the CN of its
X.509 client certificate). Supported certificate algorithms today: RSA-2048,
ECC NIST P-256 / P-384 / P-521, RSA-PSS 2048. Each node is materialised as:

- An IoT Core **Thing** named after `node_id`.
- A registered **X.509 certificate** attached to that Thing.
- A row in the `node_details` DynamoDB table.
- Membership in one or more **admin groups** (used for IAM scoping of
  operator-facing tooling).
- An IoT Device Shadow named `tags` carrying the operator-supplied metadata
  for the node (a typed `key:value` form), used by downstream dashboards.

Once registered, the node can be claimed by an end user and routed through
the rest of the platform (assume-role, MQTT, schedules, group control, etc.).

### 2.2 Input — CSV and request envelope

Registration takes a CSV uploaded to S3 plus a JSON envelope:

```json
{
  "cert_file_s3_path": "s3://...",
  "admin_group_names": ["..."],
  "admin_parent_group_name": "...",
  "tags": ["k:v"]
}
```

The CSV has the following columns:

| Column | Required | Meaning |
|---|---|---|
| `node_id` | yes | Unique node identifier — must match the CN of the cert |
| `certs` | yes | PEM of the X.509 client cert |
| `admin_groups` | no | Comma-separated admin group names this row joins |
| *(any other)* | no | Treated as a `<column>:<cell>` tag on the node |

Example:

| node_id | certs | city      | type   | model | subtype |
| ------- | ----- | --------- | ------ | ----- | ------- |
| node1   | cert1 | Amsterdam | Light  | Led   | RGB     |
| node2   | cert2 | Barcelona | Switch | basic |         |

Tags / admin-groups in the JSON envelope are applied to every row; CSV
columns are per-row overrides. The CSV either originates from manufacturing
(pre-provisioned cert files) or is generated on the fly by the operator
ahead of registration.

### 2.3 The per-node registration routine

The per-node registration routine drives the per-node work. It calls 5–6
AWS APIs in sequence:

1. `iot:RegisterCertificate` (or `RegisterCertificateWithoutCA`).
2. `iot:CreateThing` with `node_id` as the Thing name.
3. `iot:AttachThingPrincipal` to bind cert to Thing.
4. `iot:AddThingToThingGroup` for each admin group on the row.
5. `dynamodb:PutItem` on `node_details`.
6. `iot:UpdateThingShadow` for the `tags` shadow.

The routine is multi-step and not transactional. If step *k* succeeds and
step *k+1* fails, the node is left half-registered in IoT Core; the design
in §3.4 makes this safe.

### 2.4 Bulk execution model

A bulk job is one `POST /v1/admin/nodes/registration-jobs` call from an
admin tool. The Lambda:

1. Writes a `node_reg_reqs` row with `status=requested` and a fresh
   `request_id`.
2. Kicks off an ECS Fargate task (RunTask on the shared task definition),
   passing the request ID, the S3 path, and the envelope params as env vars.
3. Returns `202 Accepted` with `{request_id}`.

The bulk container then:

1. Streams the CSV from S3.
2. Fans the rows out across a pool of worker goroutines — four per CPU,
   ~8 on a 2-vCPU Fargate task — using the shared parallel-processing
   helper. Each worker runs the per-node registration routine for one row.
3. Counts successes and failures under a single shared counter mutex.
4. At end of job, writes `status=completed` plus aggregate counts back to
   `node_reg_reqs`.

### 2.5 Operational gaps this design addresses

The bulk model above produces four kinds of friction in practice; the
design in §3 closes each of them as an orthogonal change:

1. **Transient AWS failures cause avoidable partial failures.** Inside a
   single task, 8+ goroutines can momentarily exceed IoT Core control-plane
   TPS limits or DynamoDB partition throughput. Independent of concurrency,
   individual API calls also see occasional 5xx errors and connection
   blips. Without per-call retry, each of those bubbles up as a counted
   failure even though a retry seconds later would succeed.
2. **The operator can't tell which nodes failed.** The job record stores
   only `success_count` / `failed_count`. Failing `node_id`s land in
   CloudWatch logs — impractical for an admin dashboard or programmatic
   remediation.
3. **Retrying failed registrations is unsafe.** A row that partially
   succeeded (cert registered, but tag write failed afterwards) cannot be
   retried by re-uploading the CSV — `RegisterCertificate` returns
   `ResourceAlreadyExistsException` on the second attempt.
4. **There is no path to update existing nodes.** Operators need to update
   tags / metadata of registered nodes, and replace mistakenly-registered
   certificates, without re-registering the device. (Cert update here means
   cloud-side correction — nothing pushes a new cert to the device.)

---

## 3. Design

### 3.1 Endpoints

The action surface is split by intent; everything else is shared.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/admin/nodes/registration-jobs` | Submit a bulk register job |
| `GET`  | `/v1/admin/nodes/registration-jobs` | List jobs |
| `GET`  | `/v1/admin/nodes/registration-jobs/{requestId}` | Aggregate status of one register job |
| `GET`  | `/v1/admin/nodes/registration-jobs/{requestId}/failed-nodes` | Paginated audit failure list |
| `POST` | `/v1/admin/nodes/update-jobs` | Submit a bulk update job |
| `GET`  | `/v1/admin/nodes/update-jobs/{requestId}` | Aggregate status of one update job |
| `GET`  | `/v1/admin/nodes/update-jobs/{requestId}/failed-nodes` | Paginated audit failure list |

All endpoints are gated by super-admin.

Two things the shape does **not** do:

- **The `{requestId}` sub-trees are flow-isolated, not shared.** Each status
  read declares the `job_type` it expects, and a job whose stored `job_type`
  does not match is reported as **404** rather than returned. An update job's
  `request_id` queried under `/registration-jobs/` therefore looks like it does
  not exist — deliberately, because `request_id` is not flow-scoped and the
  tracking table is shared.
- **`GET /registration-jobs` takes no `job_type` filter.** The list endpoint
  exists only under `registration-jobs` (there is no list under `update-jobs`)
  and returns **both** register and update jobs; clients filter client-side on
  the `job_type` field in each row.

There is no failed-nodes *export* endpoint. The re-uploadable CSV is written
to S3 by the container at end-of-job (§3.5.5); the status response
(`GET .../{requestId}`) carries its S3 key and a presigned download URL. The
paginated `failed-nodes` list above is the audit surface (node ID, code,
reason).

The `update-jobs` `{requestId}` sub-tree mirrors the `registration-jobs` one —
same status and failed-CSV semantics, same DB tables — with the cross-flow 404
described above keeping the two sets of jobs from leaking into each other. Only
the list endpoint is not duplicated.

### 3.2 Job request and storage

Request body for both `POST` endpoints:

```json
{
  "cert_file_s3_path": "s3://...",
  "admin_group_names": ["..."],
  "admin_parent_group_name": "...",
  "tags": ["k:v"]
}
```

The `cert_file_s3_path` points at an S3-resident CSV. (A future `GET` to
obtain an upload URL is captured under Future work.)

Both job types persist into the existing `node_reg_reqs` DynamoDB table,
augmented with a `job_type` attribute (values `register` or `update`).

Existing rows have `job_type=""` and are interpreted as `register` for
backward compatibility. New registration jobs explicitly set
`job_type="register"`; new update jobs set `job_type="update"`. The list
endpoint can optionally filter by `job_type`.

The row gains one more attribute, `failed_file_s3_path`, written by the
container at end-of-job when the run produced at least one failure. It is
the S3 location of the re-uploadable failed-rows CSV (§3.5.5), of the form
`s3://.../<requestId>_failed_node_certs.csv`. It is absent on jobs with zero
failures, and absent (with a flagged status message) if the S3 write itself
failed.

Per-failure audit detail lives in a new shared table — see §3.5.

### 3.3 Async lifecycle

```
POST /…/registration-jobs (or /update-jobs)
        │
        ▼
  Lambda writes node_reg_reqs(status=requested, job_type)
        │
        ▼
  Lambda RunTask on shared Fargate task definition
        │  env: REQUEST_ID, CERT_FILE_S3_PATH, JOB_TYPE, …
        ▼
  Container loads CSV → parallel processing → per-row dispatch
        │            ┌── job_type=register → register path
        │            └── job_type=update   → update path
        ▼
  Container: BatchWriteItem failures → node_reg_failed_nodes  (audit: node_id, code, reason)
  Container: PutObject failed-rows CSV → s3://…/<requestId>_failed_node_certs.csv  (certs + all columns)
  Container: Update node_reg_reqs(status=completed, success_count, failed_count, failed_file_s3_path, message)
```

`GET .../{requestId}` returns aggregate counts. `GET .../failed-nodes`
returns per-node detail. A job is observable while running (status
progresses `requested → started → data_loaded → completed`), but failure
detail is written in one batch at end-of-job — polling `failed-nodes`
mid-run returns an empty list. Documented behavior; live progress writes
are deferred to Future work.

### 3.4 Transient failure absorption — SDK retryer

**Goal.** Eliminate transient throttling / 5xx / connection failures at
the SDK level — where retries are per-API-call and idempotent — uniformly
across every binary that uses the shared AWS-client configuration (all
Lambdas plus the bulk container).

**Why SDK retry is the right layer.** The throttling pressure that matters
most originates *inside* a single ECS task, not across tasks. The parallel
loop runs ~8 goroutines concurrently on a 2-vCPU Fargate task, each fanning
out 5–6 AWS calls per node. Peak rate easily brushes against IoT Core
control-plane TPS limits and DynamoDB partition throughput. Independent of
that, every binary hits occasional 5xx and connection errors regardless of
load — Lambdas benefit from the same resilience, not just the container.

AWS SDK Go v2's standard retry mode already classifies these as
retryable. **Adaptive** mode is a strict superset: same retry classes,
plus a client-side rate limiter that shrinks the in-process request rate
when the SDK observes throttling responses. That limiter is the part that
smooths the bulk container's goroutine-pool burstiness; for a Lambda
doing one request per invoke it is effectively a no-op, so adaptive is
neutral there and beneficial for the container.

**Implementation.** A single change where the shared AWS-client
configuration loads the default AWS SDK config: install a custom retryer
using **adaptive** retry mode with the maximum backoff capped to 5 seconds.
Adaptive mode retries connection errors, HTTP 5xx, request timeouts, and
the throttling 4xx responses; it does not retry the non-throttling 4xx
class (conditional-check-failed, validation, access-denied, …) or context
cancellation. The backoff is capped so retries stay well under the API
Gateway 29 s ceiling.

**Why these knobs.**

- Max attempts left at the SDK default of 3 (initial + 2 retries). The
  throttling-smoothing comes from adaptive mode's client-side rate limiter, not
  from extra attempts — a 4th or 5th attempt almost never recovers something the
  3rd didn't, and §3.6's per-step idempotency means the application layer can
  safely retry an entire row when the SDK gives up. The default also keeps the
  worst-case wall-clock retry budget tight (≤~300 ms of backoff, against
  ≤~1.5 s at five attempts).
- Max backoff of 5 seconds. Bounded specifically because Lambdas
  behind API Gateway have a 29-second response ceiling. Without a cap,
  exponential backoff can stretch to tens of seconds and burn the budget
  on a single retried call.
- Adaptive mode over standard mode. Strict superset of standard's
  behavior; the only incremental cost is a small in-process rate-limit
  data structure, and the bulk container is the place where that
  smoothing actually pays off.

No CDK changes, no env vars. The retryer is process-scoped, so every
binary that loads the shared AWS-client configuration picks it up
automatically — Lambda cold starts, warm invocations, and the long-running
ECS task all use the same policy.

**What it does and does not absorb:**

| Failure mode | Absorbed at SDK layer? |
|---|---|
| Transient throttling on IoT Core / DynamoDB during parallel loop | Yes (retry + adaptive client-side rate limiting) |
| Random 5xx from AWS | Yes (retry attempts × ≤5 s backoff each gives multi-second headroom) |
| Connection errors (TLS, DNS, idle drops) | Yes (standard retry covers connection-class failures, inherited by adaptive) |
| Hard quota saturation lasting minutes | No (lower the worker-pool size or request a quota increase) |
| Non-transient errors: malformed cert, duplicate node ID, unregistered CA | No — surfaced to the operator via §3.5 |

### 3.5 Per-node failure visibility

**Goal.** Record every failure (node ID, code, reason) durably for audit and
expose it via a paginated API. The re-uploadable, cert-bearing CSV of failed
rows is a separate artifact the container writes to S3 (§3.5.5) — DynamoDB
deliberately holds no cert material, only the audit triple.

**Storage.** A new shared DynamoDB table — partitioned by `request_id`,
one row per failed node. Each failure is its own item, so the 400 KB-per
-item limit never applies regardless of failure volume. No truncation. No
caps.

#### 3.5.1 `node_reg_failed_nodes` table

| Attribute | Type | Role |
|---|---|---|
| `request_id` | string | Partition key |
| `node_id` | string | Sort key (a job won't have two failures for the same node) |
| `code` | string | Coarse failure classification (`DUPLICATE_NODEID`, `INVALID_CERT`, `SERVER_ERROR`, `UNKNOWN`) — lets dashboards filter without parsing `reason` |
| `reason` | string | Full wrapped error chain rendered by the shared error-chain formatter — joins each error layer's message with `": "` so the root cause survives, not just the top-level wrapper text |
| `recorded_at` | number | Unix seconds |

The code is produced by a failure classifier at the container layer when
the failure entry is built. The classifier inspects the error chain for
AWS-origin API errors and falls back to a small text match for cert-parse
errors that never reached an AWS call. The set is intentionally minimal —
extend it only when a real filtering or aggregation need surfaces.
Unrecognised errors degrade to `UNKNOWN`; the operator still sees the full
`reason` text.

Capacity mode: on-demand, matching `node_reg_reqs`. No TTL — failure
rows are retained indefinitely; batch failures are rare and operators
may need them later to debug issues. The table name is added to the
shared table-name constants. The same table backs registration and
update jobs — failures look the same regardless of job type.

#### 3.5.2 DB layer

A new DB-layer package, modeled on the existing job-request DB layer,
exposes two operations:

- A record-failures operation that persists a batch of failure entries for
  a given `request_id`.
- A list-failures operation that returns a page of entries plus a
  continuation key.

Each failure entry carries `request_id`, `node_id`, `reason`, and
`recorded_at`.

- Record-failures chunks into batches of 25 and uses `BatchWriteItem`.
  Standard SDK retry covers unprocessed items; the loop also re-submits
  any unprocessed items returned in the response.
- List-failures uses the same paginated-query helper the existing job-list
  endpoint uses, with the standard pagination-token encoding.
- RBAC: writes require the node-admin add permission, reads require the
  node-admin registration-status permission — same gates as the parent
  job record.

#### 3.5.3 Container — failure capture

Inside the bulk container:

1. **Package-level failure slice** alongside the existing counters,
   protected by the same counter mutex.
2. **Reset at container entry** alongside the counter reset.
3. **Register-path failure branch.** On error from the per-node
   registration routine, take the node ID it reports; if that is empty
   (cert parse failed before an ID was known), fall back to the CSV
   `node_id` value. Under the counter mutex, increment the failure count
   and append a failure entry (`request_id`, the resolved node ID, the
   full error text, and the current timestamp), then log the error. No
   truncation. No cap.
4. **After the parallel loop returns**, before the final job-record
   update, snapshot the failures under the mutex and persist them via the
   record-failures operation. The job is still marked `completed` with
   accurate counts even if the failure-detail write fails — the operator
   sees a flagged `message` and can check container logs as a fallback.

#### 3.5.4 `GET .../failed-nodes` — paginated JSON

The handler sources page size from the shared page-size parser — same
helper, same `page_size=20` default, and same lack of a hard upper
cap as the existing `GET /v1/admin/nodes/registration-jobs` list. The
implicit upper bound is the Lambda 6 MB sync-response payload limit:
because each failure carries the **untruncated** error text (often
1–5 KB for wrapped AWS errors with request IDs, retry attempts, etc.),
the realistic ceiling for `page_size` is in the low hundreds rather
than four-digit values. Callers requesting more receive whatever fits;
oversize responses error at the Lambda boundary the same way they
would for any other list endpoint.

`request_id` is the path parameter, so it is not echoed in the body —
matching every other list endpoint in the codebase.

The response carries the page of failure entries (`failed_nodes`), a
`page_total` count, and an optional `next_key` continuation token.

Behavior at scale (e.g. 2,000 failures of 50,000): a dashboard at the
default `page_size=20` issues 100 calls, but the UI typically only
fetches the first one or two pages. This endpoint is the audit view
(reasons + codes); it is not the path for bulk retry — for that the
operator downloads the failed-rows CSV the container already wrote to S3
(§3.5.5), which carries the certs and every input column.

Edge cases:

- *Job not found:* 404. Distinguishing from "zero failures" matters.
- *Job exists, zero failures:* 200 with `failed_nodes: []`. The
  dashboard hits this constantly.
- *Job still running:* returns empty list (failures are flushed in one
  batch at end-of-job). Documented; live progress is Future work.

#### 3.5.5 Re-uploadable failed-nodes CSV (eager, S3-backed)

The re-uploadable CSV is produced **once, by the container, at end-of-job**
and written to S3 — not assembled on demand in a Lambda export. Two
properties drive the choice: it carries the certificate material (so
re-submission is a true round trip), and because the bytes go straight to S3
it is bound by S3 size, not by the Lambda sync-response payload limit. There
is consequently no failed-nodes export endpoint.

**What the container writes.** A strict subset of the job's original input
CSV, filtered to the failed `node_id`s, preserving the header row and **all**
columns (certs and every extra tag column) verbatim. The container already
holds both halves in memory at end-of-job: the parsed input rows (from the
CSV-loading routine) and the failed-`node_id` set (the same failure entries
it persists to DynamoDB). The match key is the operator-supplied `node_id`
column — the same identifier recorded as the DB failure key — so the round
trip is consistent even for rows that failed during cert parsing.

The CSV-loading routine returns the parsed rows as a list of column-keyed
maps, which does not retain column order, so it also returns the header
list; the writer emits the header first, then each failed row in that
column order.

**Where.** Same bucket and key-prefix as the input CSV, filename
`<requestId>_failed_node_certs.csv` (e.g. input
`s3://…/system/05c514ff_node_certs.csv` →
`s3://…/system/05c514ff_failed_node_certs.csv`). The full key is recorded on
the job row as `failed_file_s3_path` (§3.2). The key is deterministic in the
`request_id`, so there is no overwrite ambiguity — each job owns a distinct
file.

**When.** Only when the run produced ≥1 failure. Zero-failure jobs write no
file and leave `failed_file_s3_path` unset. The write happens alongside the
DynamoDB failure flush, before the final job-record update: the audit triple
goes to DynamoDB and the cert-bearing failed rows go to S3 under the
deterministic key.

The S3 write is **non-fatal**, mirroring the DynamoDB-detail path: the job is
still marked `completed` with accurate counts, the DynamoDB audit list
remains authoritative, and the operator sees a flagged `message`. A failed
CSV write simply leaves `failed_file_s3_path` unset on the status.

**Download — presigned GET, surfaced on the status.** `GET .../{requestId}`
returns, in addition to the aggregate counts, the failed-file S3 key
(`failed_file_s3_path`) and a presigned GET download URL
(`failed_file_download_url`, valid 15 minutes).

When `failed_file_s3_path` is set, the status handler mints a presigned GET
URL for that single object (15-minute expiry) and returns it inline. The URL
is generated fresh on every status call, so it is never stale; the client
(dashboard or script) fetches the bytes **directly from S3**, bypassing the
Lambda/API-Gateway response path entirely. Access control is preserved
end-to-end: the URL is minted only for a caller who passed super-admin RBAC
on the status endpoint, is scoped to the single object, and expires in 15
minutes. This requires a presigned-GET helper (the existing presign helper
is PUT-only). No new read-side IAM is required — the registration/update
Lambda role already holds `s3:GetObject` on the bucket.

**No payload ceiling.** Because the CSV never traverses a Lambda sync
response, the ~6 MB cap that an on-demand Lambda export would hit (≈5K
cert-bearing rows for RSA-2048, fewer for heavy error chains) does not apply
here. Failure sets of any size round-trip: the file is S3-sized, and the
re-init job reads it back through the same CSV-loading path it already
uses for any input CSV.

**Lifecycle.** The failed CSV lives in the same bucket as the input and is
subject to the same lifecycle rules. If it is lifecycled or deleted before the
operator re-submits, the re-init job fails at CSV load the same way any
missing input does — no special-casing.

### 3.6 Per-step idempotency in the registration routine

**Goal.** Make the registration path safe to retry. Re-uploading the
same CSV (in part or in whole) should never fail because of state
created by an earlier partial run.

Without this, the failed-nodes CSV re-upload workflow above only works for
nodes that failed *before* any AWS state was created. For nodes that
partially registered, the operator would have to clean up IoT state
manually before retry.

The IoT-core steps inside the per-node registration routine are modified to
absorb duplicate-resource errors and continue:

| Step | Without idempotency | With idempotency |
|---|---|---|
| `RegisterCertificate` (or `…WithoutCA`) | Fails on `ResourceAlreadyExistsException` | Catch, treat as "cert already registered with this fingerprint", continue |
| `CreateThing` | Fails on `ResourceAlreadyExistsException` | Catch, treat as "Thing already exists with this name", continue |
| `AttachThingPrincipal` | Already idempotent in IoT | Unchanged |
| `AddThingToThingGroup` (per group) | Already idempotent in IoT | Unchanged |
| Node-details add | Error already ignored at call site | **Not idempotent** — this step is what rejects a re-registration |
| Tag-shadow update | Already overwrites; idempotent | Unchanged |

The operator semantic becomes: **re-running a row resumes any step that had
not completed, and a node that is already registered is reported rather than
silently re-registered.**

The `node_details` row is the authority for "is this node registered", and
rejecting there is deliberate. It keeps the IoT-step idempotency above
meaningful: a run interrupted partway through still completes on retry,
because registration is not keyed on the IoT resources.

One consequence. The rejection happens after the IoT-core steps, which have
therefore already re-run. For the case this protects — the same certificate
submitted twice — each of those is a no-op. A *different* certificate
carrying the same CN is the exception: it is registered and attached before
the rejection, leaving the node with a second active credential. Use update
jobs (§3.7) to replace a certificate; that path deactivates the previous
ones.

Concretely, the IoT-core registration step calls `RegisterCertificate`,
`CreateThing`, and `AttachThingPrincipal` in sequence; a duplicate-resource
error from either of the first two is treated as an already-done step and
swallowed (optionally verifying a fingerprint match on the existing cert),
while `AttachThingPrincipal` is already idempotent in IoT and needs no
special handling. The duplicate-resource detection is a reusable helper in
the shared error utilities — it recognises the AWS
`ResourceAlreadyExistsException` code anywhere in the error chain.

**Safety considerations.**

- **Cert fingerprint validation.** Before treating a duplicate-cert
  error as a no-op, optionally verify the existing cert matches the cert
  in the request. For the retry-with-same-CSV case this is always true;
  the check protects against an operator accidentally re-using a node
  ID with a different cert. Mismatch → a distinct error (`cert mismatch
  for existing node`), surfaced as a real failure.
- **Thing attribute drift.** If an existing Thing has different
  attributes than the request would set, current code silently
  overwrites. That matches the registration-is-strict-but-idempotent
  semantic. Stricter checking can be added later.

**Resulting retry workflow:**

1. Bulk job runs; some rows fail. The container records the audit triple
   in `node_reg_failed_nodes` and writes the cert-bearing failed-rows CSV
   to S3, recording its key on the job row.
2. Operator polls `GET .../{requestId}`, reads `failed_file_download_url`,
   and downloads the filtered CSV directly from S3.
3. Operator calls `POST .../registration-jobs` again with that CSV (its S3
   key is already a valid `cert_file_s3_path`, so no re-upload is even
   strictly necessary).
4. Each retry row either resumes from where it left off (partial state
   absorbed) or restarts cleanly (no prior state). A row whose node was
   already registered is reported as `DUPLICATE_NODEID` rather than silently
   succeeding — including rows that failed at the tag-shadow or admin-group
   step. Finish those with an update job (§3.7).

No retry endpoint, no flags, no operator-visible idempotency knob, no
client-side join. For just the reasons, the operator reads the paginated
audit list (`GET .../failed-nodes`) instead.

### 3.7 Update jobs

**Goal.** Support bulk updates of metadata (tags, admin groups, plus
any extra CSV columns treated as `key:value` tags) and cert *updates*
(correcting mistakenly-registered certificates) on existing nodes,
without conflating with registration semantics. Cert update here is
**not** key rotation — nothing pushes a new cert to the device. The use
case is operator correction: "I registered the wrong PEM for this node,
replace it with the right one."

#### 3.7.1 Why a separate endpoint

Update operations differ from registration in every meaningful way:

| Aspect | Register | Update |
|---|---|---|
| Per-row precondition | Node must NOT exist | Node MUST exist (or fail — see §3.7.5) |
| Required CSV columns | `node_id`, `certs` | `node_id`; everything else optional |
| Cert handling | Required, registers new cert | Optional column; if present, replaces the registered cert |
| IoT IAM | `RegisterCertificate`, `CreateThing` | `UpdateCertificate`, `UpdateThing`, `DescribeThing`, `DetachThingPrincipal` |
| What "success" means | Node now exists with correct state | Existing node updated to requested state |
| Non-existent node | N/A (this is the goal) | Distinct outcome (failure) |

Modeling this as flags on the registration endpoint produces a 4-cell
mode matrix where the same field (`certs` column) has different
validity rules in different cells. A separate endpoint gives each
operation a single, clean contract.

#### 3.7.2 Endpoint and CSV

`POST /v1/admin/nodes/update-jobs` — same request body as
`registration-jobs`, minus cert-related top-level fields. Tags /
admin-groups in the body are common defaults applied to all rows;
per-row overrides come from CSV columns.

CSV schema:

- `node_id` — required.
- `certs` — optional. If present and non-empty, replace the cert
  currently registered for the node with this PEM.
- `admin_groups` — optional, comma-separated.
- Any other column — treated as a tag (`column:value`).

Response shape and async behavior are identical to `registration-jobs`:
returns `202 Accepted` with `{request_id}`. Status, list, and
failed-nodes endpoints are shared.

#### 3.7.3 Container — mode-aware dispatch

One container task definition, one image, one entry point. The Lambda
passes a new env var `JOB_TYPE` when launching the Fargate task. The
container reads it from its env-config at startup and dispatches per row:
`register` runs the existing register path, `update` runs a new update
path that calls the per-node update routine.

#### 3.7.4 The per-node update routine

The per-node update routine takes the node ID, an optional replacement cert
(empty when there is no cert update), the admin group names, and the tags.

Per-row behavior:

1. Verify the node exists (via a `node_details` lookup). If missing, return
   a node-not-found error — the container records it in the failed-nodes
   table with that reason.
2. If a replacement cert is provided, replace the registered cert: register
   the new cert (idempotent — recovers the ARN if already registered),
   attach it to the Thing, attach the default policy, then detach and
   deactivate any previously-attached certs. Replace-and-deactivate fits the
   correction use case — leaving the old (wrong) cert active alongside the
   new one would defeat the operator's intent. The device must already be
   holding the new cert (or the operator must arrange that out of band) for
   connectivity to recover.
3. Add to admin groups (idempotent, same operation as registration).
4. Update the `node_details` row (upsert).
5. Update the `tags` shadow (idempotent overwrite, same operation as
   registration).

Steps 3–5 reuse the existing registration helpers — only step 1 (existence
check) and step 2 (cert update) are new.

Re-running the same row is a clean no-op: an already-attached cert is
skipped in the detach loop, and tags/groups overwrite idempotently.

#### 3.7.5 Missing-node semantic

`node not found` is recorded as a **failure** (not a silent no-op).
This serves as an integrity check — "I expected all 1000 nodes to be
deployed; tell me which aren't" — and the failure surfaces in the
failed-nodes table with that reason. Consistent across all update jobs
(no flag).

#### 3.7.6 Validation

Reject the request up-front (Lambda layer) when:

- The CSV is empty.
- `node_id` column is missing.
- The body contains no defaults AND the CSV has no metadata columns
  (the job would be a no-op for every row).

Per-row validation happens in the container.

#### 3.7.7 Lambda packaging

A new update Lambda function (parallel to the registration Lambda)
handles the update endpoints. Keeps IAM scoped per intent (the
registration Lambda doesn't need cert-update IAM, and vice versa) and
keeps the route table per binary clean. Reuses the same shared container
image.

### 3.8 Design intent recap

Four orthogonal pieces — SDK retryer (§3.4), failure visibility (§3.5),
per-step idempotency (§3.6), update jobs (§3.7) — independent in scope and
concern. The registration endpoint is not overloaded with mode flags; each
operation with different intent, validation, IAM, and failure semantics has
its own endpoint. The retry workflow falls out of the idempotency fix rather
than a dedicated retry endpoint.

---

## 4. IAM and CDK

### 4.1 Shared infrastructure

- Declare the `node_reg_failed_nodes` table next to `node_reg_reqs`:
  on-demand capacity, no TTL (failure rows are retained indefinitely).
- Add the table name to the shared table-name constants.

### 4.2 Bulk container task role

- `dynamodb:BatchWriteItem` on `node_reg_failed_nodes`.
- `s3:GetObject` on the files bucket to read the input CSV, and
  `s3:PutObject` on the same bucket to write the eager failed-rows CSV
  (§3.5.5).
- For **update mode**, additional IoT permissions:
  - `iot:DescribeThing` on `*`
  - `iot:UpdateCertificate` on `*` (or scoped to certs in this account)
  - `iot:DetachThingPrincipal`

  Cert update reuses `iot:UpdateCertificate` and `iot:DetachThingPrincipal`
  already granted by the existing node-registration IAM policy, so it
  requires no additional IAM.

### 4.3 Registration Lambda

- `dynamodb:Query` on `node_reg_failed_nodes` (for the failed-nodes
  audit endpoint).
- `s3:GetObject` on the input bucket — already granted by the existing
  node-registration IAM policy. Used both for reading the input CSV and for
  signing the failed-CSV download URL (the presign step only signs; the
  read happens client-side under this same permission), so no new IAM.
- API Gateway resources under the existing `{requestId}` resource — just the
  `failed-nodes` audit list (a `GET` method plus a CORS preflight). There is
  no `export` sub-resource.

### 4.4 Update Lambda (new)

Parallel to the registration Lambda:

- New Lambda role: writes on `node_reg_reqs`, `dynamodb:Query` on
  `node_reg_failed_nodes`, `ecs:RunTask` on the shared task definition.
- New update Lambda function.
- API Gateway resources under `/v1/admin/nodes/update-jobs/...`
  mirroring the registration tree.

The Fargate task definition is shared between register and update
jobs. The container image is the same.

---

## 5. Security Analysis

### 5.1 Super-admin gating

All registration and update endpoints sit behind super-admin RBAC (the
node-admin add permission for writes, the node-admin registration-status
permission for reads). The same gates apply to `failed-nodes` reads — a
tenant who cannot see the job record cannot see its failure list.

### 5.2 CSV provenance

The CSV is fetched from S3 using credentials of the bulk container task
role. The container only reads from `cert_file_s3_path` recorded in the
job row at submission time; an operator cannot post-hoc swap input by
overwriting an unrelated S3 path.

### 5.3 Cert mismatch detection

The optional fingerprint check inside the idempotent cert-registration
path (§3.6) rejects an operator who attempts to re-register a `node_id`
with a different cert. Without that check, the duplicate would silently
no-op against the existing cert — a usability hazard.

### 5.4 Cert replacement (update jobs)

The update path's cert-replacement step detaches and deactivates
the prior cert in the same job. There is never a window where two valid
certs are attached to the same Thing on the cloud side.

### 5.5 Failure-row blast radius

Each failure is its own DynamoDB item (max ~1 KB). Even pathological
jobs (50,000 failures) cannot wedge a single partition because the
partition key is `request_id`, and writes are chunked into batches of
25 with `BatchWriteItem`.

---

## 6. Tests

### 6.1 DB layer

New unit tests for the failed-nodes DB layer verify:

- Record-failures writes the expected rows; chunking >25 entries works.
- List-failures returns rows for the right `request_id`, paginates via
  the continuation key, and returns empty on an unknown ID.
- RBAC: writes without the node-admin add permission and reads without
  the node-admin registration-status permission are rejected.

### 6.2 Bulk container

Container tests verify:

- Mixed success/failure CSV (register mode): success/failure counts, and
  that the failures table has the expected rows with full untruncated
  reasons and the right `request_id`.
- All-fail CSV: every row appears in the failures table, in any order.
- Failed-rows CSV write: an object lands in the mock S3 at
  `<requestId>_failed_node_certs.csv`, containing the header plus exactly
  the failed rows (certs and all columns intact, original column order),
  and `failed_file_s3_path` is set on the job row.
- Zero-failure job: no CSV object is written and `failed_file_s3_path`
  stays unset.
- S3 write failure (mock PutObject error): job still completes with
  accurate counts, `failed_file_s3_path` unset, status `message` flagged.
- `JOB_TYPE=update` cases mirroring the register-mode coverage.

### 6.3 Per-step idempotency

Extend the existing registration-routine specs:

- Re-running a previously successful registration is rejected (HTTP 409 on
  the single-node endpoint, `DUPLICATE_NODEID` on a bulk row) and leaves the
  existing admin groups and tags untouched.
- A run interrupted partway through still completes when retried.
- A row that fails at the tag-shadow step is *not* completable by re-running
  registration; it is reported as a duplicate and finished via an update job.
- Cert fingerprint mismatch (if implemented): an existing cert with the
  same CN but a different fingerprint produces a distinct error code.
- Update-routine specs: happy path, missing node, cert rotation, partial
  failures.

### 6.4 Endpoint tests

Registration endpoint tests verify:

- `GET .../failed-nodes` happy path: seed rows via the DB layer, hit
  the handler, assert page contents and pagination.
- `GET .../failed-nodes` job-not-found vs job-with-zero-failures:
  distinct response codes and shapes.
- `GET .../{requestId}` download URL: on a job row with
  `failed_file_s3_path` set, the status response carries a
  `failed_file_download_url` that is a presigned GET against that key
  (the presign mock records the operation/key); both fields are **absent**
  when `failed_file_s3_path` is unset.

Endpoint-level tests for the update Lambda cover `POST /update-jobs`,
`GET /update-jobs/{requestId}` and its failed-nodes endpoint, plus the
cross-flow isolation: a register job's `request_id` read under `/update-jobs/`
(and vice versa) must 404. There is no list endpoint under `update-jobs`.

### 6.5 SDK retryer verification

1. Deploy.
2. Run a bulk job sized to have produced partial failures previously.
3. Confirm `success_count == total_nodes` and that CloudWatch metrics
   show retries on the IoT and DynamoDB clients.
4. Confirm Lambda p99 latency (visible on existing CloudWatch
   dashboards) hasn't regressed — the 5 s max-backoff cap is the
   safeguard, but the metric should confirm.

---

## 7. What Does Not Change

| Component | Why |
|---|---|
| `node_details` schema | Registration writes the same row shape as before |
| Existing aggregate status endpoint `GET .../{requestId}` | Still returns counts only; per-node detail is on the new `failed-nodes` endpoint |
| Existing list endpoint `GET .../registration-jobs` | Continues to return all jobs of both types |
| Existing callers of the per-node registration routine outside the bulk container | Assisted claiming only registers nodes it has not registered before, so it never hits the duplicate rejection |
| `tags` shadow shape | Unchanged |
| AWS IoT policies attached at registration time | Unchanged |
| Container image, Fargate task definition | Same image, same task def — `JOB_TYPE` env switches the per-row code path |

---

## 8. Out of scope

- **Application-level retry around the registration routine.** The routine
  is multi-step. Retry belongs at the per-API-call layer (SDK retry,
  §3.4) and at the per-step idempotency layer (§3.6) — not as an outer
  wrapper.
- **Dedicated retry endpoint.** The eager failed-rows CSV in S3 (§3.5.5)
  plus per-step idempotency makes "download failed CSV, re-submit" a
  reliable workflow. Revisit only if programmatic retry of an entire job
  becomes a real need.
- **Live progress counters during job execution.** Counts and failure
  rows are written at job completion. Adding mid-run progress is a
  separate, additive change.
- **Skipped-row tracking.** When operations leave some rows untouched
  (e.g., update of a non-existent node, depending on the chosen
  semantic), skipped is derivable from `total_nodes - success_count
  - failed_count`. No `skipped_count` column.
- **Device-side cert rotation.** §3.7's cert update is cloud-side
  correction only. Pushing a new cert to the device is a separate
  problem.

---

## 9. Future work

- **GET upload URL endpoint.** A `GET` to obtain a presigned PUT URL
  for the input CSV, parameterised by `file_type` and a unique id (with
  filename convention `node_cert_<ts>.csv`). Today operators upload to
  S3 out of band.
- **Claiming hooks.** End-user claim is downstream of registration;
  pre-claim hooks (e.g., notifications) are not in scope.
- **CLI / UI front-end for bulk jobs.** Admin tooling beyond the raw
  API.
- **Live progress writes.** Switch the container to incremental
  `BatchWriteItem` flushes during the loop instead of one batch at
  end. Trades extra DynamoDB calls for a meaningful `failed-nodes` view
  while a job is running.
- **`failed` terminal status.** A container crash before the final
  job-record update leaves the row stuck at `started` or `data_loaded`.
  A stale-row watchdog or CloudWatch alarm is a small, separate change.
- **Programmatic retry endpoint.** Revisit only if the download-and
  -re-submit workflow proves insufficient (e.g. for fully automated
  remediation pipelines that don't want a two-call flow).
- **Audit CSV download.** The audit triple (node_id, code, reason) is
  available only as paginated JSON (`GET .../failed-nodes`). If a CSV of
  the *reasons* is later wanted for compliance/dashboards, the container
  could write a second audit-shape CSV to S3 alongside the cert-bearing
  one — same eager pattern, no in-Lambda generation. Not built; the JSON
  list covers the need today.
- **Strict cert-mismatch detection on registration.** If operationally
  desired, extend §3.6's existence check to compare cert fingerprints
  and reject mismatches with a distinct error.
- **Bulk delete jobs.** A future `delete-jobs` endpoint would slot in
  with the same container, schema, and failure-visibility patterns.
- **Server-side `?job_type=` filter on `/registration-jobs` list.**
  Today the list endpoint returns both registration and update jobs;
  clients can filter by the `job_type` field. A query-param filter on
  the DB layer would be cheaper at scale.
- **First-class `type`/`model`/`subtype` fields.** Treated as
  tag-formatted CSV columns today. If the operator needs them as Thing
  attributes or distinct schema fields, that is a small additive
  change once the data model is decided.
