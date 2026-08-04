# ESP RainMaker Neo — Claude Instructions

This repository's working rules live in [.claude/rules/](.claude/rules/). Read and follow them all — they apply to every task in this repo.

## Rule files

- [.claude/rules/code-quality.mdc](.claude/rules/code-quality.mdc) — code standards (maintainability, DRY, parallelisation, mentor mode, commenting, guidelines folder)
- [.claude/rules/assistant-behaviour.mdc](.claude/rules/assistant-behaviour.mdc) — how Claude/Cursor should communicate (no apologies, no summaries, single-chunk edits, no recap of current code)
- [.claude/rules/backend.mdc](.claude/rules/backend.mdc) — backend architecture, testing, API handler conventions (`ExtractRequestStruct`, single `rmlog` + single `APIGwRespJSON`, swagger, `test_api.py` / `morpheus.py`; DB-vs-handler boundary, paired side-effect calls)
- [.claude/rules/go-rules.mdc](.claude/rules/go-rules.mdc) — Go standards (Uber style guide, Ginkgo unit tests with negative cases, AWS SDK interface/mock layout in `utils`/`awsutils` and `test/mock`; RBAC checks in the DB layer, least-privilege grants, no `*All` wildcards)
- [.claude/rules/aws-rules.mdc](.claude/rules/aws-rules.mdc) — CDK conventions (`cdk/apps` + `cdk/utils` layout, `app_common.py` reuse, per-lambda `stack.py`, IAM-with-every-AWS-SDK-call, DynamoDB patterns)
- [.claude/rules/git-flow.mdc](.claude/rules/git-flow.mdc) — branch names, commit format, PR rules

## Conflict resolution

If a rule in one file conflicts with another, the more specific rule wins (`go-rules.mdc` over `backend.mdc` for `.go` files; `aws-rules.mdc` over `backend.mdc` for CDK code).
