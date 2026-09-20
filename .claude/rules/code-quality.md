---
description: Code Quality Guidelines
---
# Code Quality Guidelines

## Verify Information
Always verify information before presenting it. Do not make assumptions or speculate without clear evidence.

## Structure
Structure code consistently. Suggest improvements if you see inconsistencies. Group code by feature when it improves clarity and cohesion. Keep logic decoupled from framework-specific code.

## Maintainability
Write maintainable code. Avoid code duplication — refactor when the same logic appears more than once.

## Parallelise
Parallelise wherever possible (Use util/parallel.go).

## Industry Standards & AI-Friendly Code
Follow industry-standard patterns. Keep code AI-compatible: self-documenting names, small focused functions, explicit types, predictable structure.

## Commenting
Comment sparingly and only where it earns its place: a non-obvious *why*, a constraint or gotcha a reader would otherwise trip over, or a reference (doc, spec, ticket, upstream bug). Everything else — what the code does, how it does it, section banners, restated names — gets no comment. Most functions and most lines need none; a file where comments appear every few lines is over-commented, and deleting the surplus is part of the change.
Be crisp: two lines of wrapped text is the ceiling, one sentence the norm. Say the non-obvious point in as few words as it takes and stop; no restating the code, no hedging, no filler. If the *why* genuinely needs a paragraph, it belongs in the commit message or a doc, not the source.
Existing long comments in this repo are not the standard to match. Do not lengthen a comment to fit its surroundings.
Do not hard-wrap comments. Editors have word-wrap on, so write each comment as one logical line and let it wrap; never insert manual line breaks to fit a column width.

On a function whose behaviour is not obvious from its signature — a non-trivial transform, a parsing or naming convention, a surprising edge case — put one concrete input and its output in the doc comment. An example is worth more than a paragraph of prose and it shows the edge case the signature hides. Functions whose signature already says everything get no such example.

```go
// SplitChildNode splits a child node ID into its parent and suffix: "node123--light1" -> ("node123", "light1"). A plain ID returns ("", "") — the "--" separator is what marks a child.
func SplitChildNode(id string) (parent, suffix string)
```

Delete — restates the code:
```go
// Loop over the nodes and delete each one.
for _, n := range nodes {
```

Delete — narrates the change or the reasoning that belongs in the commit message:
```go
// Refactored this to use QueryPaginated instead of Query, which is safer and avoids the issues we saw earlier with large tables silently truncating results when the data grew past the page size.
items, err := db.QueryPaginated(ctx, input)
```

Keep — a constraint the reader cannot see from the code:
```go
// DynamoDB caps a single call at 1 MB, so a bare Query truncates silently as the table grows.
items, err := db.QueryPaginated(ctx, input)
```

Keep — a reference:
```python
# Bootstrap without --app: the alexa app reads rmng-outputs.json, which does not exist on a first deploy (see docs/en/specs/deploy.md).
```
