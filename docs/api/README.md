# API docs site

Static [Swagger UI](https://github.com/swagger-api/swagger-ui) build plus the OpenAPI/AsyncAPI specs it serves. No bundler, no build step — the files here are published as-is.

Three pipelines in [`.gitlab-ci.yml`](../../.gitlab-ci.yml) read this folder, all on `main` only:

| Job | What it publishes | Where |
|---|---|---|
| `sync_swagger` | this folder, minus `MQTT_*.yaml` / `mqtt_*` / `Push_*.yaml` | `s3://esp-rainmaker-neo-api` → https://api.docs.neo.rainmaker.espressif.com |
| `sync_mqtt` | `MQTT_*.yaml` rendered through `@asyncapi/html-template`, plus `mqtt_index.html` as the index | `s3://esp-rainmaker-neo-mqtt` → https://mqtt.docs.neo.rainmaker.espressif.com |
| `sync_events` | `Push_User.yaml` (notification/event payloads) rendered through `@asyncapi/html-template` | `s3://esp-rainmaker-neo-events` → https://events.docs.neo.rainmaker.espressif.com |

`sync_swagger` uses `aws s3 sync --delete`, so the bucket is an exact mirror of this folder. Deleting a file here deletes it from the live site on the next run; `git revert` puts it back. There is no state outside this repo.

Separately, [`scripts/upload_rmng_outputs.py`](../../scripts/upload_rmng_outputs.py) publishes `*.yaml` from this folder to the per-account public assets bucket for self-hosted deployments. It globs YAML only and never touches the UI assets.

## Provenance

Vendored from **`swagger-ui-dist@4.12.0`**, Apache-2.0 — see [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE), both copied verbatim from that release. Added in commit `8441806f` (Sep 2024).

> **Don't delete `LICENSE` as a duplicate of the repo-root one.** The two files are byte-identical — both are the stock Apache-2.0 text — but they are not interchangeable. The root `LICENSE` covers Espressif's own code and never leaves the repo; `sync_swagger` publishes only the contents of this folder to `s3://esp-rainmaker-neo-api`, so a copy has to live *here* or the published Swagger UI ships with no license text at all. [`NOTICE`](./NOTICE) carries the part that is genuinely not at the root: the SmartBear copyright for the vendored bundles.

## Local modifications

Exactly two files differ from upstream. Re-apply both on any refresh.

- [`index.html`](./index.html) — one added line, `<link rel="stylesheet" type="text/css" href="index.css" />`.
- [`swagger-initializer.js`](./swagger-initializer.js) — rewritten for the two-spec `urls` array and an allow-listed `?urls.primaryName=` deep link. This is deliberately **not** `queryConfigEnabled`: that flag also honours `?url=` and `?configUrl=`, which would let anyone point the live site at a spec they control. Keep that comment and the allow-list intact — the security posture below depends on it.

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
