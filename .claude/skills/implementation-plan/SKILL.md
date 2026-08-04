---
name: implementation-plan
description: Analyse design/brainstorming docs, compare with implementation, create/update comprehensive implementation plan with progress tracking
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, Task
---

Create or update a comprehensive implementation plan for the current project.

## What This Skill Does

1. Discovers and reads ALL design, brainstorming, planning, and requirements documents in the project `docs/en/specs/`
2. Compares planned features against actual implementation in the codebase
3. Creates or updates a structured implementation plan at `misc/plans/IMPLEMENTATION_PLAN.md`
4. Tracks progress by marking phases, tasks, and sub-tasks as DONE/IN-PROGRESS/PARTIAL/PENDING/BLOCKED
5. Answers "what should I work on next?" by reading the current plan

## Arguments

Handle `$ARGUMENTS` as follows:
- **No arguments / "update"**: Full scan — read all docs, audit codebase, create or update the plan
- **"status"**: Quick summary — read the existing plan and summarize progress, don't rescan
- **"next"**: Read the plan and tell the user what to work on next
- **"phase N"**: Deep-dive into phase N — audit that phase's implementation in detail
- **"mark TASK_DESC as done"**: Find the matching task and mark it complete with today's date

## Execution Steps

### Step 1: Discover Design Documents

Search the project for planning/design/brainstorming documents:
- `misc/specs` `docs/api` `misc/aws_resources.puml` 
- `*.md` files in `docs/` root
- `ARCHITECTURE.md`, `DESIGN.md`, `ROADMAP.md`, `TODO.md` in project root
- `CLAUDE.md` and memory files for project conventions and prior decisions
- Summary files first (e.g. `summary-*.md`, `overview-*.md`) for quick orientation, then detailed docs

Read ALL discovered documents to build a complete picture of the project's intended scope.

### Step 2: Audit Current Implementation

For each feature/system described in the design docs, check the actual codebase:
- Walk the source tree structure to understand what exists
- Check for implemented modules, routes, components, tests, configs
- Use grep/glob to find evidence of implementation
- Check git log for relevant commits

For each feature, determine its status:
- **DONE**: Fully implemented and working
- **PARTIAL**: Started but incomplete — note what's done and what remains
- **IN-PROGRESS**: Actively being worked on (evidence in recent commits or branches)
- **PENDING**: Not started yet
- **BLOCKED**: Cannot proceed — note the blocker

### Step 3: Organize Into Phases

Group related work into logical phases based on what the design docs describe.
Order phases by dependency — foundational work first, features that depend on it later.
Each phase should have a clear goal ("what does completing this phase achieve?").

### Step 4: Write the Plan

Create or update `misc/plans/IMPLEMENTATION_PLAN.md` using this structure:

```markdown
# Implementation Plan

> Last updated: YYYY-MM-DD
> Overall progress: X/Y phases complete, Z% of tasks done

## How to Use This Document
- Run `/implementation-plan` to refresh status from codebase
- Run `/implementation-plan next` to see what to work on
- Each task has a status: DONE / IN-PROGRESS / PARTIAL / PENDING / BLOCKED
- Commit references and file paths link to actual work

## Phase 1: [Phase Name]
> Status: DONE | IN-PROGRESS | PENDING
> Goal: [What completing this phase achieves]
> Dependencies: None | Phase N

### 1.1 [Feature/Task Group]
> Status: DONE | PARTIAL | PENDING

- [x] Task description — `commit: abc1234` or `file: path/to/impl`
- [x] Task description — `commit: def5678`
- [ ] Task description
  - [x] Sub-task done
  - [ ] Sub-task pending
- [ ] **Tests**: [what needs testing]
- [ ] **Verify**: [verification criteria]

### 1.2 [Next Feature/Task Group]
...

## Phase 2: [Phase Name]
...

---

## What's Next

> Priority list based on current status

1. **Immediate**: [highest priority incomplete task with context]
2. **Next up**: [second priority]
3. **After that**: [third priority]

## Blockers & Decisions Needed

- [ ] [Blocker/decision description and what it affects]
```

### Step 5: Cross-Reference Commits

Use `git log --oneline` (and `git log --oneline --all` for branches) to find commits
relevant to completed work. Link commits to tasks as evidence.

### Step 6: Generate "What's Next"

Prioritize based on:
- **Dependencies**: What must be done before other things can start
- **Momentum**: What's partially done and easy to finish
- **Testing gaps**: Implemented features that lack tests
- **Recent activity**: What the user has been working on (recent commits, current branch)

## Rules

- Be thorough — read every design doc, not just summaries
- Be honest — don't mark DONE unless it's actually implemented and verified in the codebase
- Include test tasks — every feature needs tests (unit, integration, E2E as appropriate)
- Include verification tasks — build checks, smoke tests, manual verification steps
- One source of truth — the plan lives at `misc/plans/IMPLEMENTATION_PLAN.md`
- Always update "Last updated" timestamp when modifying the plan
- When marking things done, include commit hash or file path as evidence
- The plan should be useful to someone with no context — include enough detail per task
- Don't invent phases or tasks that aren't in the design docs — derive everything from what's written
