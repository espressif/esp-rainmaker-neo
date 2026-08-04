---
name: run-tests
description: Run make test to green first, then make itest; report all failures and re-run failing tests one by one to form an RCA and fix where the cause is clear.
allowed-tools: Bash, Read, Edit, Grep, Glob
---

Run the repo's test surface **in order — unit tests to green before integration tests** —
then drive any failures to a root cause.

## Stage 1 — unit tests (`make test`) must pass first

1. `make test` — Ginkgo unit tests (rmng module plus the `TEST_SUBMODULES`:
   `cloud-components`, `src/mcp_stack`). Capture the output; do not stop at the
   first failure.
2. **Report** every unit failure before touching anything: test name, file, and the
   one-line reason.
3. If there are failures, work them with the **Isolate → RCA → fix** loop below until
   `make test` is fully green. **Do not run `make itest` while unit tests are red** —
   integration tests are slow, hit real AWS, and a unit-level bug will just reproduce
   there as noise that is harder to diagnose.
4. Re-run the full `make test` once at the end of this stage to confirm green.

## Stage 2 — integration tests (`make itest`)

Only once Stage 1 is green:

1. `make itest` — pytest integration tests (`test/itest/` plus optional itest dirs,
   HTML report at `build/tests/report.html`). These hit real AWS resources, so they
   need a deployed stack and valid AWS credentials; if the environment clearly isn't
   set up (no credentials / no stack outputs), report that instead of a wall of errors.
2. **Report** every failure, then work them with the same loop below.

If the user explicitly asks for itest only, skip Stage 1 but say that you did.

## Isolate → RCA → fix loop

For each failing test, one at a time:

1. **Re-run it in isolation** to confirm it fails alone (rules out ordering/parallelism):
   - Go: `go clean -testcache && ginkgo --tags "$(grep -m1 GO_BUILD_TAGS Makefile | cut -d= -f2)" --focus "<It description>" <package-dir>`
     (or `TEST_ARGS='--focus "<desc>"' make test` when the tags/env plumbing matters).
   - itest: `pytest <file>::<test> -v -s --capture=tee-sys`
     (add `ITEST_ARGS='-k <expr>' make itest` when the Makefile env matters).
2. **RCA**: read the failing assertion and the code under test; distinguish
   (a) product bug, (b) stale/wrong test, (c) environment/infra issue (missing deploy,
   throttling, eventual consistency). State which one and why, citing the evidence.
3. **Fix** when the cause is unambiguous:
   - Product bug → fix the code, honouring `.claude/rules/go-rules.mdc` / `backend.mdc`.
   - Stale test → update the test to the intended behaviour (never weaken an assertion
     just to make it pass — if intended behaviour is unclear, ask instead).
   - Environment issue → don't "fix" code; report what the environment needs.
4. **Verify**: re-run the fixed test in isolation, then re-run the enclosing package/file
   to catch regressions.

## Output

End with a table per stage: test → failure → RCA → action taken (fixed / needs user
decision / environment). State plainly whether `make test` reached green and whether
`make itest` was run or skipped, and why. Do not commit anything.
