---
name: commit-message
description: Generate a precise, crisp commit message from the STAGED changes only, following the repo's type(scope): description convention.
allowed-tools: Bash
---

Generate a commit message for the currently **staged** changes.

## Scope: staged changes ONLY

Consider only what is staged. Never look at unstaged or untracked changes.

1. `git diff --cached --stat` — see which files are staged and the change size.
2. `git diff --cached` — read the actual staged hunks; base the message solely on these.
3. If `git diff --cached` is empty, stop and tell the user nothing is staged (do not fall back to unstaged changes).

Do not run `git add`. Do not consider `git status` working-tree (unstaged) or untracked entries.

## Match the recent history

Before writing, check the history for coherence:

4. `git log -1 --format=%B` — read the **latest commit's full message** (subject AND body). This is the most important reference.
5. `git log -n 15 --pretty=format:%s` — scan recent subjects for the type/scope vocabulary and phrasing.

Use the latest commit to:
- **Avoid duplication/contradiction**: if the staged changes continue or fix work the latest commit just landed, phrase the new message as that continuation — do not re-announce what is already committed (e.g. don't say "add X" if the latest commit already added X; say "refine X" / "fix X").
- **Stay consistent**: reuse its scope naming, casing, and imperative style so the new message reads as the next entry in the log, not an outlier.

Then obey the format and rules below.

## Format

`type(scope): description`

- **type** — one of: `feat`, `fix`, `docs`, `refactor`, `test`, `chore` (per `.claude/rules/git-flow.mdc`).
- **scope** — the primary package/area touched (e.g. `group`, `node`, `db`). Pick the dominant one from the staged paths.
- **description** — imperative mood, lowercase start, no trailing period.

Optional body (only when the change spans multiple distinct things):
- A blank line after the subject, then a bullet list.
- One bullet per distinct change, each starting with `- `.
- A bullet may lead with a short `label:` framing when it aids clarity (e.g. `backward compatibility: ...`).

## Rules

-**Why**: Define Why more than What in description
- **Precise & crisp**: state exactly what changed, no filler ("various", "some", "improvements", "update code").
- **Describe observable behavior, not plumbing**: name user-facing things (endpoints, response fields/keys, capability values, config keys). Omit internal mechanics — helper/function names, parameter or signature changes, and call-chain threading — unless that mechanic *is* the change. e.g. "derive matter_node_id from node_id", not "derive it via MatterNodeIDFromThingName threaded through AddNode".
- **Do not break lines**: never hard-wrap text. The subject is one line; each bullet is one line (however long). No mid-sentence newlines.
- **Subject ≤ ~72 chars** when achievable without losing precision; bullets have no length limit (one line each).
- **List allowed**: use bullets only when there are multiple independent changes; for a single change, a one-line subject (no body) is preferred.
- Derive everything from the diff — do not invent rationale that isn't evident in the staged changes.

## Self-review before output

Re-read the draft against the Rules and the latest commit, and revise until flawless:
- Cut every plumbing detail (helper/function names, signature/param changes, call-chain threading) and all filler.
- Cut any bullet that restates work the latest commit already landed.
- Ensure each bullet is exactly one distinct, observable change; merge near-duplicates, split "and"-joined ones.
- Confirm the type and scope match the history, the subject is imperative and ≤ ~72 chars, and no line is hard-wrapped.

## Output

Print the final commit message in a fenced code block, ready to paste. Do not create the commit unless the user explicitly asks; if they do, branch off `main` first per `.claude/rules/git-flow.mdc` (no direct commits to `main`).

