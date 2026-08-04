# Style Guide

The authoritative coding conventions live **in the repository**, in
[`.claude/rules/`](https://github.com/espressif/esp-rainmaker-neo/tree/main/.claude/rules).
They apply to human and AI-assisted contributions alike:

| File | Covers |
|---|---|
| `code-quality.mdc` | maintainability, DRY, commenting |
| `go-rules.mdc` | Go standards, test layout, AWS SDK mocking |
| `backend.mdc` | API handler conventions, DB-vs-handler boundary |
| `aws-rules.mdc` | CDK layout, IAM, DynamoDB patterns |
| `git-flow.mdc` | branches, commits, PRs |

## Go

- Unit tests are [Ginkgo](https://onsi.github.io/ginkgo/) v2 + Gomega, in
  `*_test.go` next to the code. Every behavior needs negative cases too —
  error paths, permission denials, malformed input.
- AWS SDK calls go through the interface/mock layout in `src/utils` and
  `src/mock` so they stay testable.
- RBAC checks belong in the DB layer, not scattered through handlers.

## CDK / Python

- Stacks follow the `cdk/apps` + `cdk/libs` layout; reuse `app_common.py`;
  one `stack.py` per lambda.
- **IAM grants are least-privilege.** Every AWS SDK call needs a matching
  policy statement scoped to the specific resource. Wildcards need a comment
  explaining why the API requires one — `*All` grant helpers are not used.

## General

- **Comment the *why*, not the *what*** — and only when it is non-obvious.
- Match the style of the surrounding code: naming, comment density, idiom.
- Keep changes focused; don't reformat or refactor code unrelated to your
  change in the same commit.