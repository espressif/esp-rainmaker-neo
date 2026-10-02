# Testing

Three test layers, and a contribution should cover the ones its change
touches. All commands run from the repo root.

## Go unit tests

[Ginkgo](https://onsi.github.io/ginkgo/) v2 + Gomega, in `*_test.go` files
alongside the code they test.

```shell
go install github.com/onsi/ginkgo/v2/ginkgo@latest
make test
```

Expectations:

- New behavior needs the happy path **and** the negative cases: error paths,
  permission denials, malformed input.
- AWS SDK calls are mocked through the interfaces in `src/utils` /
  `src/mock` — unit tests never touch real AWS.
- `make test` also runs submodule suites.

## Python integration tests

pytest suites under `test/itest/` exercise a **deployed** stack end-to-end —
they need a `make deploy`ed environment and AWS credentials.

```shell
make itest-setup  # One-time: setup integration test environment (deploy test infrastructure)
make itest                              # full suite
make itest ITEST_ARGS='-k notifications'  # subset by keyword
```

The shared harness and fixtures live in `test/itest/conftest.py`; new itests
should reuse them rather than building their own clients. `pytest.ini` roots
the run at the repo top (`pythonpath = .`).

### What an integration test is for

Unit tests own **decisions**; integration tests own **wiring**. A rule — is a
wrong PKCE verifier refused? does an expired hint verify? — belongs to a Ginkgo
spec and must not be re-asserted over the network, where it becomes a slower,
flakier copy of a test that already passes.

An itest earns its place only when a deployment can break something a unit test
cannot see:

- a header a gateway strips on the way out,
- an index a mock does not model,
- a cookie's wire format, which the browser judges and the handler never sees,
- a document served from S3 rather than built in memory,
- a **sequence** that spans requests, or two clients sharing one browser.

If a proposed itest is none of those, the assertion belongs one layer down.

### The ESP User suite

The authorization server's tests are marked `espuser` and can be run alone:

```shell
make itest ITEST_ARGS='-m espuser'
```

They share `py_sdk/espuser_oauth.py` (a `Browser`, which is a cookie jar, and a `WebClient`, which is a client registration — several of either, which is what makes single sign-on and sign-out testable at all) and the fixtures in `test/itest/conftest.py`.

| Flow | Where |
|---|---|
| Discovery documents, cache lifetimes, served security headers | `test_oidc_discovery.py` |
| Single sign-on, `prompt`/`max_age`, the device list, RP-Initiated Logout | `test_user_auth_sso.py` |
| Authorization code, refresh, `client_credentials` and audience-scoped tokens | `test_user_auth.py` |
| The browser leg in a real Chromium | `test_user_login_ui.py` |

Two conventions worth keeping:

- **Assert the consequence, not the status code.** "Did the sign-out work" is
  answered by trying to refresh afterwards, never by a 302. Every sign-out
  defect this project has had returned a perfectly good 302.
- **Sessions always exist**, so a session test needs no setup step and no
  fixture of its own. The suite runs with `-n 12`, and anything that reached in
  and changed the session layer would be changing the deployment under every
  other test at the same time.

## Dashboard tests

The admin dashboard uses [Vitest](https://vitest.dev/); tests are
`*.test.ts(x)` files alongside the component.

```shell
cd src/admin/dashboard
npm install
npm test                       # vitest run — full suite, once
npm run test:watch             # vitest watch mode, for development
npm test -- path/to/file.test.tsx   # a single test file
```

`npm run typecheck` and `npm run lint` cover the rest of what CI expects from
dashboard changes.
