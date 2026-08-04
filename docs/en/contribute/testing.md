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
