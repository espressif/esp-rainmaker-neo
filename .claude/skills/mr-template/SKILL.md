---
name: mr-template
description: Generate a review-optimized MR/PR description from the diff between the current branch and main, following the repo's git-flow conventions. Use when opening a merge request, drafting a PR description, or summarizing a branch for review.
allowed-tools: Bash
---

Generate a merge-request description for the **current branch** relative to `main`. Return the template only. Do not create commits, push, or open an MR.

## Scope: this branch vs main

Read the full branch-to-main delta before writing:

1. `git rev-parse --abbrev-ref HEAD`: confirm the current branch (never operate on `main`).
2. `git log HEAD --not main --pretty=format:%s`: commit subjects unique to this branch (the "what changed" spine).
3. `git diff main...HEAD --stat`: which files changed and the change size.
4. `git diff main...HEAD`: the actual hunks. Base the description solely on these.
5. If the branch has no commits ahead of `main` (empty step 2), stop and tell the user there is nothing to describe.

Do not run `git add`, `git commit`, `git push`, or any MR/PR-creation command. Output text only.

## Core principle: manage the reviewer's attention

A description optimizes for **review speed**. Orient the reviewer in 30 seconds: answer *what changed, why, and where do I start reading?* before they open the diff. A small PR with a long description is the fastest way to make a reviewer skip it. Scale the description to the diff.

## Title

Active voice, present tense, full scope. Match the `type(scope):` vocabulary from the branch commits when it fits.

| Good | Bad |
| --- | --- |
| Add user authentication | Added user authentication |
| Fix memory leak in cache | Fixing memory leak |
| Use Redis for session lookup instead of DB query | Update session.py |

## Template

Emit exactly these sections. Drop a section only when the branch has nothing for it (e.g. no tests changed: keep Testing but state what was run).

```
[Title]

## TL;DR

One or two sentences. What changed and why, at a glance.

## Problem/Task

The problem or task this branch addresses. If a ticket fully explains it, link it and write one sentence.

## Change Overview

The design of the change: the shape of the solution, key decisions, and where to start reading. Not a file list.

## Testing

How the change was verified: commands run, cases covered, manual checks.
```

## Writing rules

- **Active voice, present tense.** "X overrides Y", not "Y is overridden by X."
- **Front-load keywords.** Put the most important word in the first two words of each paragraph and header.
- **Omit needless words.** Cut "in order to", "the fact that", and hedges ("rather", "quite", "very").
- **Concrete beats abstract.** "showed 71/113 (63%) instead of 71/160 (44%)" lands harder than "showed an inflated percentage."
- **Paragraphs 2 to 4 lines.** Long blocks get skipped; one-line fragments read as noise.
- **Bold sparingly.** At most one bolded headline per bullet. Bold everything and nothing stands out.
- **No em-dashes.** Use a comma, colon, parentheses, or two sentences instead.
- **No emojis.** Anywhere.
- **No jargon or buzzwords.** Skip "leverage", "robust", "seamless", "utilize", and any internal shorthand a reviewer would not recognize. Plain words.
- **No exclamation marks.** State facts, not enthusiasm.

## The diff-complement test

The description is the **complement** of the diff, not a summary. For every sentence ask: *could a reviewer learn this by reading the diff?* If yes, cut it.

Cut every time:
- **File-by-file narration.** "In foo.py changed X, in bar.py changed Y." The stat and diff already show this.
- **Implementation play-by-play.** "First I added a helper, then called it from ..." Describe the design, not your steps.
- **Motivation the reviewer already knows.** If the ticket explains it, link it and write one sentence.
- **Restating type/signature changes.** Say *why* a signature changed, not that it did.
- **Defensive disclaimers.** "First pass", "open to suggestions." Put specific asks in Change Overview as a focus area.
- **Commit-message archaeology.** "In the first commit, then in the second ..." Describe the final state.

## Never

- No `Co-Authored-By` trailers or any authorship note.
- No "Generated with Claude Code" or any AI/Claude/agent/assistant attribution, anywhere.
- No mention of Claude, AI, agents, or assistants.
- Do not open with "This PR introduces/adds/implements ...", "In this pull request ...", or "This change ...". Start with the problem, the action, or the component name.
- No "In conclusion", "Overall", or "In summary". End with a next step or a final fact.

## Self-review before output

Re-read the draft and revise until clean:
- Run the diff-complement test on every sentence; cut what the diff already shows.
- Title is active voice, present tense, full scope.
- No forbidden opener, no summary closer, no AI attribution.
- No em-dashes, emojis, exclamation marks, or filler jargon.
- Length proportional to the diff. Trim a verbose description on a small change.

## Output

Print the final MR description in a fenced code block, ready to paste. Do not create commits or open an MR unless the user explicitly asks. Even then, per `.claude/rules/git-flow.mdc`, all changes land via MRs off `main`. Never commit directly to `main`.