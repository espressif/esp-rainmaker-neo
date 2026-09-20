# ESP RainMaker Neo — Claude Instructions

This repository's working rules live in [.claude/rules/](.claude/rules/) and load automatically — no need to open them.

`code-quality.md`, `assistant-behaviour.md` and `git-flow.md` load every session. `api-rules.md`, `backend.md`, `go-rules.md` and `aws-rules.md` carry `paths:` frontmatter, so they load when a task touches the files they cover.

The dashboard has its own rules at [dashboard/.claude/rules/](dashboard/.claude/rules/), which load when work touches that directory. Cursor reads the same file through a symlink at `dashboard/.cursor/rules/admin-dashboard.mdc` — edit the real file under `.claude/`.

## Conflict resolution

If a rule in one file conflicts with another, the more specific rule wins (`go-rules.md` over `backend.md` for `.go` files; `aws-rules.md` over `backend.md` for CDK code).
