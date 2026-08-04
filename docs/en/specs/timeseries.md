# Timeseries

## 1. Overview

This document describes the design for the ESP RainMaker Neo platform's **Timeseries**
feature: the pipeline that captures numeric (and coercible) device metrics
over time, aggregates them into fixed windows, and serves both the raw
samples and the rolled-up aggregates back to clients through the node
service API.

The feature has two independent halves that never call each other directly:

1. **Ingestion (write path).** A device publishes a data point to an MQTT
   topic. An AWS IoT Core topic rule projects the message into a raw
   DynamoDB table. That table's DynamoDB stream drives a Lambda that folds
   each new sample into per-window aggregates (hourly / daily / weekly /
   monthly) stored in a second DynamoDB table. There is no synchronous
   write API — data enters only through MQTT.

2. **Query (read path).** A client calls the node service under
   `/v1/groups/{groupId}/nodes/{nodeId}/timeseries/...` to read raw samples,
   the latest sample, or aggregates for a parameter. These handlers read
   directly from the two DynamoDB tables; they never touch the stream or
   the IoT rule.

### Key design decisions

1. **Ingestion is rule-driven, not Lambda-driven.** The IoT topic rule
   (`node_ts_rule`) writes device samples straight into DynamoDB with a
   `DynamoDBv2` action. No Lambda sits on the hot ingest path, so ingest
   scales with IoT Core and DynamoDB rather than with Lambda concurrency.
   The write path explicitly rejects synchronous writes: the topic is the
   only ingress.

2. **Raw and aggregated data live in separate tables.** `raw_ts_data`
   is an append-only log of individual samples; `processed_ts_data`
   holds the running and historical window aggregates. The split lets each
   table carry a key schema tuned to its own query pattern and lets the
   stream fan the raw table into the processed one asynchronously.

3. **Aggregation is stream-triggered and incremental.** The processed table
   is not recomputed from scratch. A DynamoDB stream on `raw_ts_data`
   delivers each `INSERT` to the aggregation Lambda, which folds
   the single new sample into the current window aggregates and, when a
   window boundary is crossed, archives the completed window as a historical
   row.

4. **A single partition key format threads the whole pipeline.**
   `node_key_dt = "{node_id}.{key}.{data_type}"` is computed by the IoT rule
   (`concat(topic(3), '.', k, '.', dt)`), used as the raw-table partition
   key, carried through the stream, and reused as the processed-table
   partition key. One parameter of one node maps to one partition on each
   side.

5. **Cumulative metrics are first-class.** The processor detects cumulative
   parameters (e.g. energy meters) and derives per-window *consumption* from
   the difference between successive readings, including meter-reset
   handling.

---

## 2. Background

### 2.1 What a data point is

A timeseries data point is one numeric reading for one named parameter of one
node at one instant. As stored in the raw table it carries:

| Field | Meaning |
|---|---|
| `node_id` | The publishing node |
| `key` | Parameter name (e.g. `temperature`, `energy`) |
| `dt` | Data type string (e.g. `int`, `float`, `bool`, `string`) |
| `value` | The reading — any JSON scalar |
| `ts` | Sample timestamp (see §2.4 on units) |
| `tz` | Optional IANA timezone identifier (e.g. `UTC`, `Asia/Kolkata`) |
| `cumulative` | Optional; `true` for monotonically-increasing counters |
| `topic_name` | The trailing topic segment the sample arrived on |

`value` is coerced to a floating-point number for aggregation: numbers pass
through, numeric strings are parsed, and booleans map to `1.0` / `0.0`. A
value that cannot be coerced fails aggregation for that sample.

### 2.2 The partition key: `node_key_dt`

Every table in the pipeline is partitioned by
`node_key_dt = "{node_id}.{key}.{data_type}"`. It is assembled once, by the
IoT rule's SQL (`concat(topic(3), '.', k, '.', dt)`), and never recomputed on
the write path. The query layer rebuilds the identical string from the
node ID, `key`, and `data_type` before every read. This is why **both `key`
and `data_type` are required on every query** — without both, the partition
cannot be addressed.

### 2.3 Time windows

Aggregation is defined over four fixed window types: `hourly`, `daily`,
`weekly`, `monthly`. The window-boundary helper computes the `[start, end)`
boundary for a given instant and window:

- **hourly** — top of the hour to the next hour.
- **daily** — local midnight to next local midnight.
- **weekly** — back to the configured week-start weekday at midnight,
  spanning seven days.
- **monthly** — first of the month to first of next month.

All boundary math is done in the sample's own timezone (`tz`), so a "day"
means the device's local day, not a UTC day.

Windows are named by a stable `interval_key` string, e.g.
`hourly#2025-01-04T14`, `daily#2025-01-04`, `weekly#2025-01-04`,
`monthly#2025-01`. The `interval_key` doubles as the processed table's sort
key, which makes range queries over history a direct key-condition query
rather than a scan-and-filter (§4.2).

### 2.4 Timestamp units (a cross-layer subtlety)

The raw table's `ts` is documented and handled as **milliseconds** in the
service layer. Two consequences fall out of this and both are load-bearing:

- The stream processor divides by 1000 before doing timezone/window math.
- The raw query handler normalizes the caller-supplied `start_time` /
  `end_time` to **milliseconds** via `NormalizeTimestampMs`: a value already
  at millisecond precision passes through unchanged, while a shorter value
  (e.g. Unix seconds) is scaled up by powers of ten until it reaches
  millisecond width.

So: devices/publishers put milliseconds on the wire, API callers are expected
to pass milliseconds on the query string (seconds are tolerated and
auto-scaled), and the raw `ts` is stored and returned in milliseconds.
Aggregate window boundaries (`window_start` / `window_end`) are stored and
returned in **seconds**.

### 2.5 Timezones — per-sample, and what a change does

Timezone is a **per-sample** attribute, not a per-node setting.

- Each raw data point may carry its own `tz` (an IANA identifier such as
  `Asia/Kolkata`). It is **optional**: an absent or unparseable `tz` falls back
  to **UTC** (the timezone loader returns UTC on any load failure, including the
  empty string).
- Timestamps themselves are absolute epoch values; `tz` affects only how a
  sample is *bucketed* into the day/week/month windows. Boundary math for
  `daily` / `weekly` / `monthly` is done in the sample's `tz`, so "a day" is the
  device's local day. (`hourly` uses the same path; for whole-hour offsets it
  coincides with UTC hour boundaries, for half-hour zones it does not.)
- The processed **current** row records the `tz` seen when it was first created.
  That stored `tz` is used only to format the date string in an archived
  window's `interval_key`; it is **not** refreshed when a later sample arrives
  with a different `tz`.

**What happens if a node's `tz` changes mid-stream.** Because each sample's
window boundaries are recomputed from *that sample's* `tz`, a sample arriving
with a different `tz` than the running window generally produces a different
window-start instant. The boundary-change check then fires, so the processor
**archives the in-progress window as if it had completed and opens a fresh
one** — even though no real calendar boundary was crossed. Practically:

- The window archived at the switch can be **short/partial** — it closes at the
  `tz` change, not at the natural boundary.
- Because the current row's stored `tz` is frozen at first-seen, an archived
  window's `interval_key` date label is formatted with that original `tz`, which
  can disagree with the boundary actually computed under the new `tz` — so
  historical labels may be slightly off after a change.
- **Raw and `latest` queries are unaffected**: each raw point stores and returns
  its own `tz`, so they always reflect what the device actually sent.

In short, changing a node's `tz` is not a clean re-bucketing of history — expect
one partial/misaligned window at the switch and historical keys that keep the
first-seen zone. (Weekly windows are additionally Monday-aligned regardless of
`week_start`; see §4.6.)

---

## 3. Design — ingestion (write path)

### 3.1 Topic and IoT rule

A device publishes a data point to:

```
rainmaker/nodes/{node_id}/ts/{topic_name}
```

The IoT topic rule `node_ts_rule` subscribes to `rainmaker/nodes/+/ts/+` and
projects the message with SQL version `2016-03-23`:

```sql
SELECT
    topic(3) as node_id,           -- the {node_id} segment
    topic(5) as topic_name,        -- the {topic_name} segment
    k as key,
    dt,
    tz,
    t as ts,
    v as value,
    cumulative,
    concat(topic(3), '.', k, '.', dt) as node_key_dt
FROM 'rainmaker/nodes/+/ts/+'
```

The message payload therefore supplies `k` (key), `dt` (data type), `t`
(timestamp), `v` (value), and optionally `tz` and `cumulative`; the node ID
and topic name are lifted from the topic itself. The rule's single action is
a `DynamoDBv2` `PutItem` into the raw table (`raw_ts_data`), assuming an IoT
rule role granted only `dynamodb:PutItem` and `dynamodb:UpdateItem` on that
table. A CloudWatch Logs error action captures rule-level failures.

Because the rule uses `DynamoDBv2` with a computed `node_key_dt` and the
message's own `t` as the sort key, each published sample becomes exactly one
raw-table item.

### 3.2 Raw table — `raw_ts_data`

- **Partition key:** `node_key_dt` (String).
- **Sort key:** `ts` (Number).
- **Stream:** `NEW_AND_OLD_IMAGES` — this is what feeds aggregation.
- Point-in-time recovery enabled.
- The stream ARN is published to SSM Parameter Store
  (`RAW_TS_DATA_STREAM_ARN`) because a stream ARN embeds a
  creation-time suffix and cannot be hardcoded; the stream-processor stack
  reads it back from SSM.

Items are append-only in normal operation; the only writer is the IoT rule.

### 3.3 Stream processor Lambda

The `ts_stream_processor` Lambda is the aggregation engine. Its event source
is the raw table's DynamoDB stream:

- **Filter:** `eventName == INSERT` only, applied at the event-source-mapping
  level so updates/deletes never reach the function.
- **Batching:** `batch_size = 25`, `max_batching_window = 5s`,
  `parallelization_factor = 1`, `starting_position = TRIM_HORIZON`.
- **Resilience:** `retry_attempts = 3`, `report_batch_item_failures = true`.
- **Timeout:** 1 minute.

The Lambda runs under a **system-actor** context, which bypasses the
per-user RBAC checks that guard the query path — the stream data was already
authorized at ingestion time, and there is no user in a stream event.

For each record it:

1. Takes the record's new image (skips records with no new image).
2. Converts the DynamoDB stream image into a data point,
   validating that `node_id`, `ts`, `key`, and `value` are present.
3. Hands it to the aggregation logic.

A per-record and per-batch metrics line (conversion / aggregation / total
milliseconds) is logged for observability.

### 3.4 Aggregation logic

The aggregation logic folds one raw sample into all four windows:

1. Convert `ts` (ms) → timezone-aware instant.
2. Load the parameter's **current** processed entry
   (`interval_key = "current"`); create a fresh one if none exists.
3. Coerce `value` to a floating-point number.
4. For each of the four windows:
   - If the sample falls in a *different* window than the one currently
     accumulated, archive the completed window as a historical row (a copy
     with `interval_key` set to the window key) and reset the running
     aggregate for the new window.
   - Fold the value into the running aggregate.
5. Stamp `updated_at` (cloud time) and `last_update_time` (device time) and
   upsert the current entry.

**Aggregate fields:**
`count`, `sum`, `min`, `max`, `average`, `first_value`, `last_value`,
`cumulative_value`, `window_start`, `window_end`, `last_data_timestamp`.

**Non-cumulative parameters** aggregate the value directly: count/sum/min/max
and a rolling average.

**Cumulative parameters** (`cumulative = true`, e.g. an energy meter) instead
aggregate *consumption* — the delta between successive readings:

- The first reading of a window seeds `cumulative_value` as a baseline and
  contributes no consumption (you need two readings to measure an interval).
- Subsequent readings add `value - previousValue` to the window's
  count/sum/min/max/average.
- **Meter reset:** if a reading is *smaller* than the previous one, the
  current value itself is treated as the consumption for that step (the
  counter is assumed to have rolled over or reset).
- On a window boundary, the last cumulative reading is carried forward
  as the new window's baseline, so consumption is continuous across
  boundaries.

### 3.5 Processed table — `processed_ts_data`

- **Partition key:** `node_key_dt` (String).
- **Sort key:** `interval_key` (String) — `"current"` for the live rollup, or
  a window key like `hourly#2025-01-04T14` / `daily#2025-01-04` /
  `weekly#2025-01-04` / `monthly#2025-01` for archived windows.
- Point-in-time recovery enabled. **No stream** — this is a terminal
  destination table, not a trigger for further processing.

Two row shapes share the table:

- The **current row** (`interval_key = "current"`) holds four aggregate maps
  at once — `hourly`, `daily`, `weekly`, `monthly` — each the live
  accumulator for the in-progress window.
- A **historical row** (window-keyed `interval_key`) holds a single
  completed window's aggregates plus a `window_type` discriminator, written
  when the processor crosses that window's boundary.

---

## 4. Design — query (read path)

### 4.1 Routing and the service contract

Requests arrive at the node service Lambda. Timeseries is special-cased
ahead of the generic service route because it has sub-paths:

| Resource (API Gateway) | Effect |
|---|---|
| `/v1/groups/{groupId}/nodes/{nodeId}/timeseries/raw` | sets `timeseries_type = "raw"` |
| `/v1/groups/{groupId}/nodes/{nodeId}/timeseries/latest` | sets `timeseries_type = "latest"` |
| `/v1/groups/{groupId}/nodes/{nodeId}/timeseries/aggregates` | sets `timeseries_type = "aggregates"` |

The handler first verifies the caller can access `groupId` (a `403` if the
group is not in the caller's accessible list), then dispatches `GET` to the
timeseries query service. Query-string parameters are stashed on the context
(`query_params`) for the service to read. The request/response contract for
the three `GET` paths is documented under the `Node Timeseries` tag in
`docs/api/Api_Swagger.yaml`.

`Put` is unsupported (data enters only via MQTT). `Delete` removes all raw
and processed data for the node — it reads the node config to learn the
parameter list, then batch-deletes each `(key, data_type)` from both tables —
but no API Gateway method exposes it, so only `GET` is reachable in practice.

All read paths independently enforce `NodeGet` authorization on the node,
and the DB layer re-checks it on every query.

### 4.2 Common query parameters

| Param | Applies to | Meaning |
|---|---|---|
| `key` | all | **Required.** Parameter name. |
| `data_type` | all | **Required.** Data type string. |
| `type` | all | `raw` (default) / `latest` / `aggregates`. Overridden by the sub-path when one is used. |
| `window` | aggregates | `hourly` / `daily` / `weekly` / `monthly`. |
| `date` | aggregates | Single historical window: `YYYY-MM-DD`, or `YYYY-MM-DDTHH` for hourly. |
| `start_date` / `end_date` | aggregates | Historical range (same formats). |
| `start_time` / `end_time` | raw | Unix **milliseconds** bounding the raw query; a seconds-scale value is tolerated and auto-scaled (see §2.4). |
| `page_size` | raw, aggregates range | Page size. |
| `start_key` | raw, aggregates range | Opaque pagination token from a prior response's `next_key`. |

If `key` or `data_type` is missing, the request is rejected. If no query
params are supplied at all, the handler returns a self-describing usage
document rather than an error. When the `/raw` sub-path is used,
`start_time` is required; when the `/aggregates` sub-path is used, `window`
is required.

### 4.3 Raw query

`GET /v1/groups/{groupId}/nodes/{nodeId}/timeseries/raw?key=...&data_type=...&start_time=...&end_time=...&page_size=...&start_key=...`

- `start_time` / `end_time` are normalized to milliseconds
  (`NormalizeTimestampMs`; millisecond values pass through, seconds are
  auto-scaled up) and applied as a `ts` sort-key range on the `node_key_dt`
  partition.
- Results are returned newest-first.
- Pagination is **timestamp-based**, not exclusive-start-key-based: the
  `next_key` token encodes the last `ts`, and the next page queries strictly
  older than it. (A deliberate choice to sidestep DynamoDB
  `ExclusiveStartKey` edge cases.)

Response: `{ "data": [ {key, dt, ts, value, tz, cumulative}, ... ],
"page_total": N, "next_key": "..." }` (`next_key` present only when more
data exists).

### 4.4 Latest query

`GET /v1/groups/{groupId}/nodes/{nodeId}/timeseries/latest?key=...&data_type=...`

Runs the raw query with `limit = 1` newest-first and returns a **single
object** (not an array):
`{ "data": { key, dt, ts, value, tz, cumulative } }`.

### 4.5 Aggregates query

`GET /v1/groups/{groupId}/nodes/{nodeId}/timeseries/aggregates?...`
Three sub-modes are selected by which params are present:

**(a) Current aggregates** — neither `date` nor `start_date`/`end_date`:
- With `window`: returns the live rollup for that one window, read from the
  `current` row.
- Without `window`: returns all four windows at once, wrapped with the
  `parameter` (`node.key.dt`) and an `is_cumulative` flag.

**(b) Single historical window** — `date` present, `window` required:
- Parses `date` (`YYYY-MM-DDTHH` first for hourly, else `YYYY-MM-DD`),
  computes the window's `[start, end)`, and reads the archived window row(s).
- Returns the window's `count/sum/min/max/average/first_value/last_value/`
  `cumulative_value` plus RFC3339 `window_start` / `window_end`, or a
  "no data" message if the window was never archived.

**(c) Historical range** — `start_date` and/or `end_date`, `window` required:
- Dates are parsed with the same hourly/daily format tolerance; `end_date`
  is extended to include the whole final day (or hour).
- The query is a **sort-key range on `interval_key`** using the window keys
  for the start/end as bounds, so DynamoDB reads only the matching window
  rows instead of scanning the partition and filtering.
- Paginated (`page_size` / `start_key`), newest-first. Response carries an
  `aggregates` array (each element dated from its `interval_key`),
  `page_total`, a `query_info` echo, and `next_key` when more pages exist.

### 4.6 Week-start configuration

The service loads a timeseries configuration whose only field today is
`week_start` (`monday` | `sunday`). It is initialized to the **Monday**
default, then overlaid from an S3 object with key `timeseries_config.json`
(read through the platform file service) at process init; an invalid or
missing value leaves the Monday default in place. The operator-facing
configuration procedure is documented separately in
`docs/09-timeseries-configuration.md`; this section covers where the value
actually takes effect.

**Known limitation — the write path ignores `week_start`.** The
stream-processor aggregation always buckets weeks from **Monday** when
computing weekly window boundaries and window keys. The configured
`week_start` is consulted only on the read path, and only for the
**single-date** historical weekly query; the historical *range* query also
uses Monday-aligned window keys. In practice this means setting
`week_start = sunday` does not currently change how weekly aggregates are
computed and stored — the stored weekly buckets remain Monday-aligned. This
is a real inconsistency between config, ingestion, and query surfaced here so
integrators are not surprised; reconciling it (threading the configured week
start through the processor and the range query) is listed under Future work.

---

## 5. IAM and CDK

### 5.1 Timeseries stack

- **Raw and processed DynamoDB tables** (`raw_ts_data`, `processed_ts_data`)
  — the raw table has a `NEW_AND_OLD_IMAGES` stream, both have PITR.
- **`RAW_TS_DATA_STREAM_ARN`** — SSM string parameter carrying the raw
  table's stream ARN for cross-stack consumption.
- **`node_ts_rule`** — the IoT topic rule (§3.1), with:
  - an IoT rule role scoped to `dynamodb:PutItem` / `dynamodb:UpdateItem`
    on the raw table only;
  - an error-action role scoped to `logs:CreateLogStream` /
    `logs:PutLogEvents` on the rule's log group.

### 5.2 Stream-processor stack

- Reads `RAW_TS_DATA_STREAM_ARN` from SSM and imports the raw table with that
  stream ARN.
- **`ts_stream_processor`** Lambda with a base role granted:
  - `dynamodb:GetItem` / `PutItem` / `UpdateItem` / `Query` on the processed
    table and its indexes;
  - `dynamodb:DescribeStream` / `GetRecords` / `GetShardIterator` /
    `ListStreams` on the raw table's stream ARN.
- An event-source mapping wires the stream to the Lambda with the `INSERT`
  filter, batch/window/retry settings, and `report_batch_item_failures`
  from §3.3. (The event-source mapping has no physical name, so it can be
  moved between stacks without a naming conflict.)

Least privilege throughout: the ingest role can only write the raw table, and
the processor role can only read the stream and read/write the processed
table — neither can reach the other side's write surface.

### 5.3 Query path

The query endpoints run inside the shared node service Lambda and reuse its
existing role and group/`NodeGet` authorization; the timeseries feature adds
no dedicated query Lambda or query-specific IAM. The routes are wired in the
node service API Gateway as **`GET`-only** methods on
the `raw` / `latest` / `aggregates` sub-resources. The handler also implements
`Delete`, but no API Gateway method exposes it, so it is not a reachable route.

---

## 6. Out of scope

- **A synchronous write API.** `Put` is intentionally unsupported; ingestion
  is MQTT-only through the IoT rule. Nothing in this design accepts data
  points over HTTP.
- **Parameter discovery.** Enumerating a node's timeseries parameters is
  deliberately not supported — it would require a GSI on `node_id`. Callers
  must already know the `key` / `data_type` pairs they want.
- **Backfill / recomputation.** Aggregates are computed incrementally as
  samples stream in. There is no batch job to recompute historical windows
  from the raw table after the fact.
- **Retention / TTL.** Neither table sets a TTL in this design; raw and
  processed data are retained until explicitly deleted (per-node via
  `Delete`).
- **Custom or sub-hourly windows.** Only the four fixed windows (hourly,
  daily, weekly, monthly) exist.

---

## 7. Future work

- **Honor `week_start` end-to-end.** Thread the configured week start into the
  stream processor's weekly boundary/key computation and into the historical
  *range* query so the configured week start actually governs how weekly
  aggregates are stored and read (§4.6). Today only the single-date
  historical weekly query respects it, and ingestion is hardcoded to Monday.
- **Parameter listing via GSI.** A `node_id` GSI would let clients enumerate a
  node's timeseries parameters instead of having to know them a priori.
- **Retention controls.** A configurable TTL (especially on `raw_ts_data`,
  the highest-volume table) would bound storage growth.
- **Backfill tooling.** A path to replay or recompute processed aggregates
  from the raw table would help recover from processor bugs or window-config
  changes without data loss.
