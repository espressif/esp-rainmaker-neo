# API docs site

Static [Swagger UI](https://github.com/swagger-api/swagger-ui) build plus the OpenAPI/AsyncAPI specs it serves. No bundler, no build step — the files here are published as-is.

Four pipelines in [`.gitlab-ci.yml`](../../.gitlab-ci.yml) publish the reference site, all on `main` only. All four publish to **one** bucket and **one** CloudFront distribution, each under its own path prefix. Three read this folder; `sync_mcp` reads `docs/mcp/` instead (see below):

| Job | What it publishes | Where |
|---|---|---|
| `sync_swagger` | this folder, minus `MQTT_*.yaml` / `mqtt_*` / `Push_*.yaml` / `landing_index.html`, plus `landing_index.html` as the root index | `s3://esp-rainmaker-neo-api/http/` → https://api.docs.neo.rainmaker.espressif.com/http/ |
| `sync_mqtt` | `MQTT_*.yaml` rendered through `@asyncapi/html-template` | `s3://esp-rainmaker-neo-api/mqtt/node/` and `/mqtt/user/` |
| `sync_events` | `Push_User.yaml` (notification/event payloads) rendered through `@asyncapi/html-template` | `s3://esp-rainmaker-neo-api/events/` → https://api.docs.neo.rainmaker.espressif.com/events/ |
| `sync_mcp` | `docs/mcp/rainmaker-mcp.json` rendered by [`scripts/generate_mcp_reference.py`](../../scripts/generate_mcp_reference.py), plus the raw catalogue | `s3://esp-rainmaker-neo-api/mcp/` → https://api.docs.neo.rainmaker.espressif.com/mcp/ |

[`landing_index.html`](./landing_index.html) is the root index at https://api.docs.neo.rainmaker.espressif.com — it links to all six reference sites. It is uploaded by `sync_swagger` with `aws s3 cp` rather than riding the folder sync, because it belongs at the bucket root and not under `http/`, and it has to survive that sync's `--delete`. Same for the two favicons, which it references with `./` from the root.

Each job's `--delete` is scoped to its own prefix, so the four are safe to run in any order and none can remove another's output. **Keep it that way** — widening any of these syncs to the bucket root would make whichever job ran last wipe the other two sites and the landing page.

`sync_swagger` uses `aws s3 sync --delete`, so `http/` is an exact mirror of this folder. Deleting a file here deletes it from the live site on the next run; `git revert` puts it back. There is no state outside this repo.

Raw specs are now at `/mqtt/MQTT_Node.yaml`, `/mqtt/MQTT_User.yaml`, `/events/Push_User.yaml` and `/mcp/rainmaker-mcp.json`.

## MCP tool reference

The MCP surface is documented in two places, because it has two halves.

Its HTTP endpoint and OAuth 2.0 proxy are OpenAPI, so they live here as [`MCP_Api_Swagger.yaml`](./MCP_Api_Swagger.yaml) and render as the **MCP API** tab of `/http/` like any other spec.

Its *tools* are not. `docs/mcp/rainmaker-mcp.json` is an MCP tool catalogue — generated from the Go tool registry and byte-compared by `TestToolCatalogMatchesSnapshot` — and MCP standardises the wire format of `tools/list` but no documentation rendering. Neither Swagger UI nor `@asyncapi/html-template` can display it, and there is no standard generator that can: `@modelcontextprotocol/inspector` needs a live authenticated server and emits no static HTML, and `openapi-mcp-generator` runs the opposite direction (OpenAPI → server code). Hence [`scripts/generate_mcp_reference.py`](../../scripts/generate_mcp_reference.py), which renders the catalogue as a static page at `/mcp/`.

The catalogue deliberately stays the only copy in the tree — it is **not** duplicated into this folder. A second copy would drift, since `make update-mcp-schema` regenerates only the canonical path. `sync_mcp` uploads it from `docs/mcp/` at publish time instead, which is also why that job's `changes:` rules watch `docs/mcp/rainmaker-mcp.json` and the generator script rather than `docs/api/**`.

Preview a description change locally with:

```sh
python3 scripts/generate_mcp_reference.py   # writes build/docs/mcp/index.html
```

The output is gitignored and never committed; CI regenerates it.

The distribution serving this bucket needs a default root object of `index.html` and, for the `/http/`, and `/mqtt/node/`-style prefixes to resolve without a trailing filename, subdirectory index handling — a CloudFront Function or `index.html`-appending behaviour. S3 website endpoints do this natively; an OAC/REST origin does not.

Separately, [`scripts/upload_rmng_outputs.py`](../../scripts/upload_rmng_outputs.py) publishes `*.yaml` from this folder to the per-account public assets bucket for self-hosted deployments. It globs YAML only and never touches the UI assets.

## Provenance

Vendored from **`swagger-ui-dist@4.12.0`**, Apache-2.0 — see [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE), both copied verbatim from that release. Added in commit `8441806f` (Sep 2024).

> **Don't delete `LICENSE` as a duplicate of the repo-root one.** The two files are byte-identical — both are the stock Apache-2.0 text — but they are not interchangeable. The root `LICENSE` covers Espressif's own code and never leaves the repo; `sync_swagger` publishes only the contents of this folder to `s3://esp-rainmaker-neo-api`, so a copy has to live *here* or the published Swagger UI ships with no license text at all. [`NOTICE`](./NOTICE) carries the part that is genuinely not at the root: the SmartBear copyright for the vendored bundles.

## Local modifications

Exactly two files differ from upstream. Re-apply both on any refresh.

- [`index.html`](./index.html) — two changes: the added `<link rel="stylesheet" type="text/css" href="index.css" />`, and a `.rmng-home` banner (inline `<style>` plus a `<div>` above `#swagger-ui`) linking back to the root landing page. The banner is static markup rather than an addition to Swagger UI's own topbar, which the bundle renders itself.
- [`swagger-initializer.js`](./swagger-initializer.js) — rewritten for the three-spec `urls` array and an allow-listed `?urls.primaryName=` deep link. This is deliberately **not** `queryConfigEnabled`: that flag also honours `?url=` and `?configUrl=`, which would let anyone point the live site at a spec they control. Keep that comment and the allow-list intact — the security posture below depends on it.

## Not vendored

The 4.12.0 tarball also ships the following, all removed here. Don't reinstate them by copying a fresh `swagger-ui-dist` wholesale.

| Left out | Why |
|---|---|
| `swagger-ui-bundle.js.map`, `swagger-ui-standalone-preset.js.map`, `swagger-ui.css.map` | devtools-only; 2.3 MB published to a public site for no runtime benefit |
| `swagger-ui.js` + `.map` | referenced by no HTML or JS here |
| `swagger-ui-es-bundle.js` + `.map` | ESM variant, unused |
| `swagger-ui-es-bundle-core.js` + `.map` | ESM variant, unused |
| `index.js`, `absolute-path.js`, `package.json`, `README.md` | npm-consumer entry points; meaningless for a static site |

Dropping the maps is 6.9 MB off the folder and off every deploy. **Known side effect:** the three served files still end with `//# sourceMappingURL=…`, so a visitor with devtools open gets three 404s. Stripping those lines would mean editing vendored bytes, which we avoid on purpose — the 404s are cosmetic and devtools-only.

Upstream ships no `*.LICENSE.txt` files in this package, even though `swagger-ui-bundle.js` and `swagger-ui-standalone-preset.js` open with `/*! For license information please see …LICENSE.txt */`. That pointer dangles in the upstream tarball too; it is not something we dropped. `LICENSE` + `NOTICE` are the attribution.

## Known stale — upgrade trigger

**4.12.0 is from June 2022.** It bundles **DOMPurify 2.3.3** (Sept 2021), which carries known advisories:

| Advisory | Severity | Fixed in DOMPurify |
|---|---|---|
| CVE-2024-48910 — prototype pollution | Critical | 2.4.2 |
| CVE-2024-45801 — prototype pollution tampering | High | 2.5.4 / 3.1.3 |
| CVE-2024-47875 — nesting-based mXSS | High | 2.5.0 / 3.1.3 |

No advisory applies to `swagger-ui` itself at 4.12.0 (checked against OSV for both `swagger-ui` and `swagger-ui-dist`). The well-known Swagger UI DOM XSS affects the 3.x line and does not reach this build.

**Why this is tolerated for now:** these are all sanitiser bypasses, so they need attacker-controlled HTML to reach DOMPurify. Here that means spec descriptions — and the specs are repo-controlled, served from our own bucket, with `queryConfigEnabled` off so the UI cannot be pointed at a foreign spec. The remaining path is malicious markdown merged into `Api_Swagger.yaml`, which is already an MR-review gate.

**Revisit if** that hardening is relaxed, if spec content ever originates outside this repo, or as routine maintenance.

**Upgrade target: `swagger-ui-dist@5.17.12` or newer** — 5.17.10 still ships DOMPurify 3.1.2, and 5.17.12 is the first release on 3.1.4. Current `5.32.x` ships DOMPurify 3.4.x and is the better target. A 4.19.x bump would *not* fix this (it ships DOMPurify 3.0.2). 5.x is a major bump, so smoke-test both spec tabs; the APIs this folder uses — `StandaloneLayout`, `urls`, `urls.primaryName`, `DownloadUrl` — all still exist in 5.x.

## Refreshing Swagger UI

```bash
npm pack swagger-ui-dist@<version>
tar -xzf swagger-ui-dist-<version>.tgz
```

Copy only these from `package/`: `swagger-ui.css`, `swagger-ui-bundle.js`, `swagger-ui-standalone-preset.js`, `index.css`, `oauth2-redirect.html`, `favicon-16x16.png`, `favicon-32x32.png`, `LICENSE`, `NOTICE`.

Then re-apply both local modifications above and update the version in this file.

Verify over HTTP — `python3 -m http.server 8088` from this folder. Opening `index.html` from disk will not do: the spec fetches are blocked from a `file://` origin. Both specs should appear in the dropdown and render, `?urls.primaryName=User%20API` should open on the User API tab, and the console should be clean with the only failed requests being the three `.map` 404s.
