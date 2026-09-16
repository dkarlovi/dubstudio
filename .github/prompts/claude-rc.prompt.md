---
description: "Use when migrating deterministic business logic from the Python PoC into the Go codebase with parity validation"
name: "claude rc"
argument-hint: "feature name or migration target"
agent: "agent"
---
Continue the Go migration from the current branch state, using the repo’s migration plan as the execution guide.

Context:
- Start from the current working branch, not a fresh rewrite.
- Use [MIGRATION_PLAN.md](../../MIGRATION_PLAN.md) as the authority for the process.
- Treat the Python PoC in the sibling repo as the temporary compatibility oracle while migration is in progress.
- The final goal is a Go-only runtime with the PoC retired after parity is complete.

Required workflow for the feature you are migrating:
1. Confirm the branch state and working tree are sane.
2. Validate the Go baseline with `go test ./...` and `go build ./...`.
3. Inspect the equivalent Python behavior in the PoC repo if it exists.
4. Write a Go test for the exact behavior first and confirm it fails before implementation.
5. Implement the smallest Go fix that satisfies the test.
6. Re-run the relevant Go tests and confirm they pass.
7. Run the parity bridge against the equivalent PoC behavior.
8. Confirm the PoC behavior passes through the bridge or document the exception explicitly.
9. Record the feature in the migration tracking list with the test name, parity check, and comparison rule.

Hard rules:
- This is not a broad rewrite; keep scope to one ported feature at a time.
- TDD is required for every feature.
- Business logic must be deterministic and byte-equivalent when the same fixture is run in both implementations.
- Non-deterministic behavior must be explicitly listed as an exception rather than silently ignored.
- Keep validation evidence: command output, pass/fail status, and exact test names.
- Do not drift into unrelated refactors or cleanup.

When the task is ambiguous, prefer the smallest testable migration slice and state the assumption you are making.

Examples of migration targets:
- subtitle parsing and normalization
- speaker resolution and tag stripping
- merge logic and merge-cap behavior
- overlap detection and tolerance
- speed override handling
- cache key generation and naming
- output path generation and Unicode safety

If you need to do work before implementation, start with a baseline validation and a single failing Go test for the target behavior.
