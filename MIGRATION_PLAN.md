# DubStudio migration plan

This plan assumes the current Go project is the successor to the Python PoC and that PR #50 is the starting point for the port. Treat the current branch as the work-in-progress base, and continue from there in a fresh session.

Goal: replace the Python PoC with a single Go codebase named dubstudio while preserving behavior and pushing all business logic into the Go implementation.

Important operating assumptions:
- The Python project at `../dub-studio` is the PoC baseline.
- The Go project in this repo is the new canonical implementation.
- The final state is a pure Go codebase with no Python test dependency remaining.
- During migration, the PoC test suite is a temporary compatibility oracle.
- The current branch should be treated as the pre-merge baseline for the port, not as the final long-lived branch name.

---

## 1. Session start checklist

At the start of a fresh session:

1. Confirm the branch and working tree.
   - `git status -sb`
   - `git branch --show-current`
2. Ensure the starting point is the current PR #50 state or an equivalent checkpoint.
3. Open the PoC repo for baseline behavior checks.
   - `cd ../dub-studio`
   - verify the relevant tests and fixtures exist before starting
4. Confirm the Go project can build on the current branch.
   - `go test ./...`
   - `go build ./...`
5. Keep a list of the PoC features that are already covered by the Go branch and a list of features still needing migration.

---

## 2. Working rules for the migration

These are hard requirements for every feature ported into Go:

1. TDD is required for each feature.
   - Write the Go test first.
   - Confirm the new Go test fails before implementation.
2. The Go implementation must satisfy its own test.
3. The equivalent Python PoC test(s) must also pass against the ported behavior through the parity bridge.
4. A feature is not considered ported until both checks pass.
5. No feature is ported “informally” or by manual spot-check alone.
6. The PoC suite is a temporary validation tool only; it is not part of the final architecture.
7. The final end state is: Go-only tests, Go-only runtime, PoC retired.

---

## 3. Definition of business logic vs incidental behavior

Business logic must be treated as exact and deterministic when the same input is used in both implementations.

Business logic includes:
- subtitle parsing and normalization
- cue merging rules
- same-voice and cross-voice overlap detection
- speaker resolution and tag stripping
- speed override decisions
- cache key generation
- cache file naming and path templating
- deterministic report text
- output naming and artifact generation rules
- merge-cap decisions and timing-window logic

Incidental behavior should be listed explicitly and excluded only when it is inherently not byte-identical by design.

Examples of incidental behavior:
- wall-clock timestamps
- filesystem timestamps
- ordering sensitive to runtime scheduling
- non-deterministic logging output
- external-provider audio bytes that vary by generation time
- environment-specific metadata or local tmp paths

Rule: if the behavior is a pure function of the input and config, it must be byte-identical when the same fixture is run in both implementations. If it is not deterministic by definition, it must be explicitly documented as an exception.

---

## 4. Parity bridge design

The parity bridge is how the PoC suite drives the Go implementation during migration.

Preferred mechanism: a deterministic Go CLI parity mode plus the PoC tests as the harness driver.

Design:
- The Go project exposes a dedicated mode such as:
  - `go run . parity --fixture <path> --out-dir <dir>`
  - or an equivalent explicit command that runs one deterministic feature path and writes canonical results to disk
- The Python PoC tests call this mode with a fixture and compare the resulting files and exit code against the PoC expectations
- The Go parity mode should be intentionally narrow and deterministic, not UI-heavy or interactive
- The mode should operate on fixtures, configs, and output directories to keep parity checks stable and repeatable

Alternatives considered and rejected:
- “Just shell out and eyeball the output” — not rigorous enough
- “Write a second parallel test suite in Go” — duplicates work and fails the goal of reusing PoC coverage
- “Directly import Go code from Python” — unnecessary coupling and poor separation between the projects

Chosen approach:
- Keep the PoC test suite as the source of truth while it still exists
- Use the Go parity CLI to exercise the same behavior in a controlled way
- Require the PoC tests to pass against the ported behavior as a hard acceptance gate for each feature

---

## 5. Parity harness structure

The parity harness should be implemented as a reusable bridge, not a one-off script.

Suggested structure:

1. Fixture catalog
   - input subtitle files
   - config files
   - expected output directory structure
   - edge-case fixture names and metadata
2. Python test wrapper
   - runs a fixture through the PoC in normal mode
   - runs the same fixture through the Go parity mode
   - compares stdout, stderr, exit code, and output artifacts
3. Output normalizer
   - canonicalizes timestamps and ephemeral metadata only where explicitly allowed
   - preserves exact bytes for deterministic business logic
4. Strict compare stage
   - exact byte comparison for business-logic outputs
   - explicit exemptions for known non-deterministic areas
5. Exemptions registry
   - central list of fields or outputs that are allowed to differ by design
   - must be reviewed and documented rather than silently ignored

The goal is to maximize reuse of the PoC suite and minimize new test-writing.

---

## 6. Fixture strategy

The first parity fixtures should be small but feature-rich and deterministic.

Required fixture categories:

1. Basic parse fixtures
   - standard subtitle input
   - multi-line cues
   - speaker tags
   - timing edge cases
2. Merge behavior fixtures
   - adjacent same-voice cues
   - gap threshold checks
   - merge-cap behavior
   - cross-voice boundary protections
3. Overlap fixtures
   - same-voice overlap
   - cross-voice overlap
   - tolerance flag behavior
4. Speed fixtures
   - configured speed
   - per-cue speed override
   - out-of-range behavior
5. Cache fixtures
   - cache hit and miss
   - same text different keys
   - duplicate name collisions
6. Path generation fixtures
   - basename conversion
   - Unicode handling
   - long text trimming
   - duplicate file resolution
7. Unicode and encoding fixtures
   - em dashes
   - curly quotes
   - accented characters
   - multi-byte boundary cases
8. Error fixtures
   - invalid subtitle contents
   - invalid config values
   - invalid timing ranges

Every feature port should have at least one fixture that exercises the exact PoC behavior being preserved.

---

## 7. TDD loop by feature

For each ported behavior, use this sequence:

1. Identify the PoC behavior under test.
2. Inspect the equivalent Python test and expected output.
3. Write the Go test for the same behavior.
4. Run the new Go test and verify it fails before implementation.
5. Implement the smallest Go change needed.
6. Run the Go test again and verify it passes.
7. Run the equivalent PoC test(s) through the parity bridge against the Go behavior.
8. Confirm the PoC test(s) pass.
9. Only then mark the feature as ported.
10. Add a note in the feature tracking list documenting:
   - feature name
   - Go test name
   - PoC test name(s)
   - parity bridge mechanism used
   - exact output comparison rule
   - known exceptions if any

This is the canonical migration loop.

---

## 8. Feature-by-feature execution order

The preferred order is to start with deterministic, pure logic before moving into server and runtime integration.

Recommended order:

1. Subtitle parsing and normalization
2. Speaker resolution and tag stripping
3. Cue merging logic
4. Merge-cap logic
5. Overlap detection and tolerance
6. Speed override and timing calculation
7. Cache key generation and cache naming
8. Output naming and path generation
9. Report generation and deterministic logging
10. CLI compatibility layer
11. HTTP server and `/api` surface
12. Static asset serving and HTML layer
13. End-to-end integration across the full stack

This order maximizes parity coverage early while the PoC is still available as the oracle.

---

## 9. Concrete porting checklist per feature

For each feature, track:

- [ ] Feature name
- [ ] PoC source behavior
- [ ] Equivalent Go test written
- [ ] Go test fails before implementation
- [ ] Go implementation added
- [ ] Go test passes
- [ ] PoC test(s) executed against Go via parity bridge
- [ ] PoC tests pass
- [ ] Output comparison rule documented
- [ ] Known exceptions listed
- [ ] Feature marked complete

This checklist should live in the migration tracking document or issue tracker.

---

## 10. Acceptance criteria for the migration

The migration is considered complete only when:

1. The Go project has its own full test suite in place and passing.
2. Every ported feature has passed its own Go test and the relevant PoC tests through the bridge.
3. Business logic that is intended to be deterministic is byte-identical when both implementations run the same input.
4. Non-deterministic areas are explicitly documented and not silently ignored.
5. The PoC tests are no longer needed as the permanent test oracle.
6. The PoC repo can be retired with the Python implementation.
7. The Go project can serve as the single runtime for CLI, server, and API workflows.

---

## 11. Repository cleanup and final state

After parity is complete:

- remove the Python PoC as active runtime
- retire the Python test dependency from the migration path
- keep only the Go test suite as project-wide validation
- ensure CLI remains compatible where needed
- ensure `/api` and HTTP server are the primary operational mode for the unified project
- rename the codebase and documentation to dubstudio as part of the final upstream cleanup

---

## 12. Fresh-session execution script

This is the exact working loop to use in the next session:

1. Start from the current working branch state.
2. Build the Go project and confirm the baseline is clean.
3. Select the first unported deterministic feature.
4. Write the Go test for that feature.
5. Run the Go test and confirm it fails.
6. Implement the Go behavior.
7. Run the Go test and confirm it passes.
8. Run the equivalent PoC test(s) through the parity bridge against the Go implementation.
9. Confirm the PoC checks pass.
10. Repeat until all features are ported.
11. Once all ported features are validated, retire the PoC suite and remove the PoC repo from the active path.

---

## 13. Non-goals for this session

This plan does not include starting the feature implementation itself. It is a migration blueprint for a fresh session and should be used as an execution guide, not as a broad rewrite proposal.

The main focus is:
- preserve the PoC contract
- port each feature under TDD and parity validation
- end with a single Go implementation
- retire the PoC only after feature parity is complete

---

## 14. Progress log

Corrections to this plan discovered while executing it:
- The PoC lives at `../dub-studio` (hyphenated), not `../dubstudio` as
  originally assumed in sections 1 and elsewhere.
- The "parity bridge" (section 4) is implemented as the `parity`
  subcommand in `commands/parity.go`, invoked with a fixture path and
  `--out-dir`; it writes `parity-summary.json` (`{"cues": [...],
  "mid_cue_tag_violations": [...]}`) and never touches the network, so
  it's safe to shell out to from the PoC's test suite despite that
  suite's general "never shell out to the real binary" rule (see the
  docstring in `tests/test_parity_bridge.py` in the PoC repo).

Ported features:

1. **Mid-cue speaker-tag violation detection** — done.
   - PoC source: `core.find_midcue_tag_violations` (`~/dub-studio/core.py`),
     tested by `tests/test_vtt_and_cleanup.py::TestMidcueTagViolations`.
   - Go test: `TestFindMidCueTagViolations` (`commands/speaker_test.go`).
   - Go implementation: `findMidCueTagViolations` (`commands/speaker.go`) —
     flags any `[Name]`/`[Name@speed]` tag that isn't the very first thing
     in a cue, mirroring `speakerTagRE`'s front-anchor rule.
   - Parity bridge: `commands/parity.go`'s `mid_cue_tag_violations` field;
     PoC-side bridge test `tests/test_parity_bridge.py::TestMidCueTagViolationsParity`
     runs the real `srt11` binary on the same three fixtures as the PoC's
     own test and asserts identical `(index, tag)` pairs.
   - Comparison rule: exact match on `index` and `tag` per violation
     (deterministic, pure function of input text).
   - Known exceptions: none.
