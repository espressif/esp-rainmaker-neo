# Contributing to ESP RainMaker Neo

Thanks for your interest in contributing. We welcome all contributions!

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).

## Reporting security vulnerabilities

**Do not open a public issue for a security vulnerability.** See
[SECURITY.md](SECURITY.md) for the private reporting process.

## What we expect in a change

### Tests

- **Go**: [Ginkgo](https://onsi.github.io/ginkgo/) v2 + Gomega, in `*_test.go`
  alongside the code. New behaviour needs both the happy path and the negative
  cases — error paths, permission denials, malformed input.
- **Python integration tests**: pytest, under `test/itest/`.
- **Dashboard**: Vitest, `*.test.ts(x)` alongside the component.

### Coding standards

The conventions this codebase is written to live in [`.claude/rules/`](.claude/rules/) and apply to human and AI-assisted contributions alike:

Two that come up in review often enough to repeat here:

- **Comment the *why*, not the *what*.** Only when it is non-obvious.
- **New files carry an SPDX header** (`SPDX-FileCopyrightText` +
  `SPDX-License-Identifier: Apache-2.0`). Third-party code keeps its original
  copyright and license, which must be Apache-2.0 compatible.
- **IAM grants are least-privilege.** Every AWS SDK call needs a matching policy
  statement scoped to the specific resource. Wildcards need a comment explaining
  why the API requires one.

### Documentation

- Public API changes must be reflected in `docs/api/` (OpenAPI) or the
  AsyncAPI specs for MQTT. CI validates these.
- Behavioural changes belong in the specs under `docs/en/specs/`. See
  [`docs/README.md`](docs/README.md) for the doc build and layout rules.

## Commits and branches

Branch from `main`:

- `feat/descriptive-name` — new features
- `fix/descriptive-name` — bug fixes
- `hotfix/descriptive-name` — urgent production fixes

Commit messages use `type(scope): description`:

```
feat(node): add bulk tag assignment to the admin API
fix(claim): reject expired attestation certificates
docs(timeseries): document the aggregation window limits
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`.

Keep the subject on one line. The body may use paragraphs or lists.

## Pull requests

- One logical change per PR. Split refactors out from behavioural changes.
- Describe *why*, not just *what* — the diff already shows what.

## Contributor License Agreement

Before we can merge your first contribution, you need to sign Espressif's
Contributor License Agreement (CLA). The CLA confirms that you have the right to
submit the code and grants Espressif the rights needed to distribute it under
Apache-2.0. You keep the copyright in your contribution.

A bot will comment on your pull request with a signing link the first time you
open one. Signing takes a minute and covers all of your future contributions to
this project — you will not be asked again. The full agreement text is in
[docs/en/contribute/contributor-agreement.md](docs/en/contribute/contributor-agreement.md).

If you are contributing on behalf of your employer, make sure whoever signs is
authorised to do so for your organisation.

## Questions

Open a [discussion or issue](../../issues). For architecture questions, the specs
under [`docs/en/specs/`](docs/en/specs/) and [`docs/GLOSSARY.md`](docs/GLOSSARY.md)
are the best starting points.
