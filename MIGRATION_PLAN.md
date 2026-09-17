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

## Handoff summary — read this first

Everything in this section is current as of the last commit below. If it
disagrees with sections 1-13 further down, trust this section and the
progress log (section 14) — sections 1-13 are the *original* plan
written before any of it was executed, and some of it (mainly section
8's execution order) turned out to be wrong in ways the progress log
corrects. Read this section, skim the progress log, then start work —
you shouldn't need sections 1-13 for anything except historical rationale.

**Branch:** `feat/dubstudio-migration`, 10 commits ahead of `upstream/main`
(`dacd1a4`, i.e. `dkarlovi/dubstudio` post-PR-#50-post-rename — see
CLAUDE.local.md's "Branch policy" for how that upstream history got
there). Latest commit `b069adf`, **plus uncommitted working-tree changes**
for the `/reduce` port (item 5 below) — not committed because commits are
made only when Matko explicitly asks, per session instructions; ask him
before assuming it's fine to commit. **Not pushed anywhere, no PR opened.**
Nothing on `origin` corresponds to it.

**What's done, each with Go tests and (except items 4-5) a PoC-side parity
bridge test — see section 14 below for exact file names per feature:**

1. Parity bridge infrastructure: `parity` and `autofix-parity` CLI
   commands, so the Python PoC's test suite can shell out to the real
   Go binary and assert byte-for-byte agreement.
2. Four ported PoC business-logic features: mid-cue speaker-tag
   violation detection, mechanical filler/hedge cleanup, auto-fix
   scheduling (real timing budgets + leading-gap absorption — the
   biggest one, full parity across all 10 of the PoC's own scheduling
   fixtures), and cueTagLine/writeSessionVTT (cue re-serialization).
3. A files-first orchestration layer: `Service`/`SessionStore`
   interfaces, `LocalService`/`FileSessionStore` implementations,
   running the whole upload -> cleanup -> generate -> autofix -> export
   workflow in-process against the engine (no more shelling out to a
   separate binary and regex-scraping its report). CLI: `session-upload`,
   `session-cleanup`, `session-generate`, `session-autofix`,
   `session-export`, `session-update-cue`, `session-show`, `session-reset`.
4. An HTTP API (`serve` command): a route-for-route equivalent of
   dub-studio's `app.py` (at the time, minus `/reduce`), **live-verified
   end-to-end with a real browser (Playwright) driving dub-studio's
   actual, unmodified `static/index.html`** against it — a real
   ElevenLabs generate/autofix/export round trip producing a valid,
   downloadable WAV.
5. **`/reduce` (AI-assisted line shortening), added 2026-09-17 — a native
   Go port, not a parity port** (reverses the earlier "deliberately out of
   scope" call below, once Matko decided to retire `app.py` entirely — see
   "Not yet done" item 1). Calls the Anthropic Messages API directly over
   `net/http` (forced tool use, no new dependency), live-verified against
   the real API. New CLI command `session-reduce`; new flags
   `--anthropic-api-key`/`--reduce-model` on `serve`/`session-reduce`. A
   real bug was found and fixed during live verification — see section 14
   entry 5 for what it was and why it likely also exists, unnoticed, in
   the PoC's own Python implementation.
6. **Frontend embedded into the Go binary, added 2026-09-17.**
   `~/dub-studio/static/index.html` (a single self-contained file — inline
   CSS/JS, only a Google Fonts CDN link as an external reference, no local
   JS/CSS/image assets to bring along) is now vendored into this repo at
   `commands/webassets/index.html` and baked into the binary via
   `//go:embed` (`commands/webassets.go`). `serve` now works with **zero**
   flags pointing at the Python repo -- live-verified: `serve --config
   ~/dubbing/config.yaml --work-dir <anywhere>`, no `--static-dir` at all,
   serves the full working UI. `--static-dir` still exists but now only as
   an explicit override for iterating on the frontend locally without a
   rebuild; the embedded copy is the default and, going forward, the
   canonical one -- edit `commands/webassets/index.html`, not
   `~/dub-studio/static/index.html`. Confirms Matko's 2026-09-17
   requirement: dubstudio must end up completely standalone and
   self-contained, with every frontend asset living in the Go repo, so the
   Python PoC can eventually be deleted in full.

**Superseded — kept for context, not current:** the paragraph below
described `reduce_text` as deliberately out of scope. That call held until
2026-09-17, when Matko decided `app.py` should be retired entirely rather
than kept alive just to serve `/reduce`; item 5 above is the reversal.
> `reduce_text` (Claude-drafted line shortening) — it's an LLM call, not
> deterministic business logic, so it doesn't fit this plan's
> byte-identical parity model. No `/reduce` route exists on the new API.

**Not yet done — the real next steps:**
1. dub-studio (`~/dub-studio/app.py`) has **not** been switched to call
   this new Go HTTP API, and hasn't been retired yet either. It still runs
   its own orchestration and shells out to a separately-built binary the
   old way. The new `serve` command now covers every route `app.py` has,
   including `/reduce` as of 2026-09-17 (item 5 above), and now embeds the
   frontend itself (item 6 above) — nothing routing- or asset-wise blocks
   retiring `app.py`/`core.py`/`config.py` anymore. Matko has decided to
   replace `app.py` entirely (not keep it as a thin proxy, not split
   traffic between the two), confirmed 2026-09-17: the end state is
   dubstudio (Go) fully standalone and self-contained, with every frontend
   asset living in this repo, and the Python PoC deleted in full,
   eventually. That replacement/deletion itself hasn't happened yet --
   `~/dub-studio` still exists and its `bin/srt11` is still what gets
   rebuilt after every change (see CLAUDE.local.md).
2. No PR opened, nothing pushed to `origin` or `upstream`. Decide scope
   (one PR vs. split) before opening one — see CLAUDE.local.md's PR
   procedure section for dkarlovi's review norms, though note that section
   predates this branch and its numbered send-order is explicitly marked
   historical/superseded, not a template to follow literally.
3. A pre-existing UI quirk was found and *deliberately left as-is*
   (faithfully reproduced, not a bug in the port): the legacy frontend's
   Export button gates on a broader "flagged" condition than real
   overlaps, so it can show disabled even when `POST /export` would
   actually succeed (verified directly via a raw API call in that exact
   state). Worth fixing in the frontend someday; out of scope for this
   migration.

**Verify the current state from scratch:**
```sh
cd ~/srt11 && git status -sb && git log --oneline upstream/main..HEAD
go build ./... && go vet ./... && go test ./... && gofmt -l .
go build -o ~/dub-studio/bin/srt11 .   # mandatory after any change — see CLAUDE.local.md
```

**Try the HTTP server (frontend is embedded, no `--static-dir` needed):**
```sh
~/dub-studio/bin/srt11 --config ~/dubbing/config.yaml serve \
  --work-dir /some/scratch/dir --addr :8099
```
Add `--static-dir ~/srt11/commands/webassets` (or any directory with your
own working copy of `index.html`) only if you're iterating on the
frontend itself and don't want to rebuild the Go binary for every edit.
Then open `http://localhost:8099/` in a browser (it's dub-studio's real
UI), or drive it headlessly with Playwright. No Playwright
MCP tool was available in this session; `npm --cache <scratch-dir>
install --no-save playwright` plus `npx playwright install chromium`
into a scratch prefix worked around a broken global npm cache — don't
fight that same npm cache again, just scope around it the same way.

**Gotchas already found and fixed, don't rediscover them:**
- The `parity` command's fixture arg is a positional `console.Arg`:
  read it with `c.Args().Get("fixture")`, not `c.String("fixture")`.
- `core.auto_fix_durations` mutates its cues in place — when writing a
  parity bridge test, snapshot the pristine input *before* calling the
  Python side, or Go ends up fed the already-mutated result.
- Top-level `let`/`const` in a classic (non-module) `<script>` tag, like
  dub-studio's `index.html` uses for its `S` session variable, is not a
  `window` property — access it as the bare identifier `S` from
  `page.evaluate`/`page.waitForFunction`, not `window.S`.
- Forcing a Claude tool call for structured output needs per-field
  `description`s in the `input_schema` whenever a field's meaning depends
  on another field's value (e.g. `text` means something different when
  `action` is `"shorten"` vs `"decline"`) — bare field names alone aren't
  enough signal, confirmed live: the model put the original text in `text`
  and the actual rewrite inside `reason`'s prose until descriptions were
  added. See section 14 entry 5.

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
  "mid_cue_tag_violations": [...], "mechanical_cleanup_edits": [...]}`)
  and never touches the network, so it's safe to shell out to from the
  PoC's test suite despite that suite's general "never shell out to the
  real binary" rule (see the docstring in `tests/test_parity_bridge.py`
  in the PoC repo).
- The `parity` command's fixture arg is a positional `console.Arg`, not a
  flag -- read it with `c.Args().Get("fixture")`, not `c.String("fixture")`
  (found live: the command errored "Missing --fixture path" on every
  invocation until fixed).
- Not every item in section 8's execution order needs porting: most of it
  (speaker resolution/tag stripping, cue merging, merge-cap, overlap
  detection + tolerance, speed override, cache key generation, output
  path generation) was already implemented in the Go engine before this
  migration started (landed via PR #50). The PoC never duplicates that
  logic itself -- it shells out to the Go binary for it. What's actually
  missing from Go, and worth porting, is business logic the PoC's own
  Python layer (`core.py`) implements independently: `find_midcue_tag_violations`,
  `mechanical_cleanup`, `auto_fix_durations` (below), and, out of current
  scope, the AI-assisted `reduce_text` (non-deterministic, an LLM call --
  not portable in the byte-identical sense this plan requires) and the
  Flask `app.py`/`Session` orchestration layer (CLI compat / HTTP server /
  static assets / E2E, section 8 items 10-13 -- large, separate design
  effort, not started).

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

2. **Mechanical cleanup (deterministic filler/hedge stripping)** — done.
   - PoC source: `core.mechanical_cleanup`/`core._clean_text`, tested by
     `tests/test_vtt_and_cleanup.py::TestMechanicalCleanup`.
   - Go test: `TestCleanText`/`TestMechanicalCleanup` (`commands/cleanup_test.go`).
   - Go implementation: `cleanText`/`mechanicalCleanup` (`commands/cleanup.go`).
   - Parity bridge: `parity`'s `mechanical_cleanup_edits` field;
     `tests/test_parity_bridge.py::TestMechanicalCleanupParity`.
   - Comparison rule: exact match on `index` and `after` text.
   - Known exceptions: none. Deliberately NOT wired into `run`'s default
     pipeline -- whether/where this runs automatically in the real
     pipeline is an app-integration decision for a later step (CLI
     compat / HTTP server), not this one.

3. **Auto-fix scheduling (real timing budgets + leading-gap absorption)** — done.
   - PoC source: `core._real_budgets_ms`, `core._leading_gaps_ms`,
     `core._occupied_end_ms`, `core.auto_fix_durations`, tested by
     `tests/test_scheduling.py` (13 subtests).
   - Go test: `TestRealBudgetsMs`/`TestAutoFixDurations`/`TestLeadingGapAbsorption`
     (`commands/autofix_test.go`).
   - Go implementation: `AutoFixDurations` (`commands/autofix.go`).
   - Parity bridge: dedicated `autofix-parity` command (structured JSON
     cues + policy config in, JSON result out -- the fixture-based
     `parity` command doesn't fit here, since the input is already-
     measured `audio_ms`/`overlap_flagged` data, not raw subtitle text);
     `tests/test_parity_bridge.py::TestAutoFixDurationsParity` drives all
     10 scheduling fixtures through both implementations.
   - Comparison rule: exact match on `sped`/`basketed`/`retimed` entries
     and each cue's resulting `start_ms`/`speed`/`needs_human`.
   - Known exceptions: none. `reduce_text` (AI-assisted line shortening)
     is explicitly NOT ported -- it's an LLM call, not deterministic
     business logic, so it doesn't fit this plan's byte-identical parity
     model. It stays a Python/app-layer concern.

4. **cueTagLine / writeSessionVTT (session cue re-serialization)** — done.
   - PoC source: `core._cue_tag_line`/`core._ms_to_vtt_ts`/`core.write_srt11_vtt`,
     tested by `tests/test_srt11_io.py` (`TestCueTagLine`, `TestMsToVttTs`,
     `TestWriteSrt11Vtt`).
   - Go test: `TestCueTagLine`/`TestMsToVTTTimestamp`/`TestWriteSessionVTT`
     (`commands/sessionvtt_test.go`).
   - Go implementation: `cueTagLine`/`msToVTTTimestamp`/`writeSessionVTT`
     (`commands/sessionvtt.go`) -- generalized from Python's hardcoded
     matko/hana pair to an arbitrary configured default speaker name,
     since the engine already supports arbitrary speaker names.
   - Parity bridge: none yet (no PoC test drives this through the real
     binary); the ported unit tests reproduce the PoC's exact fixtures
     and expected byte output directly.
   - Known exceptions: none.

**Files-first orchestration service (section 8 items 10-11, started).**
Built the app-level orchestration layer dub-studio's `app.py`/`core.py`
currently provide (upload -> cleanup -> generate -> autofix -> export) as
a `Service` interface with a `LocalService` implementation, calling the
engine's own parsing/generation/overlap-detection/mixing functions
in-process instead of shelling out to a separately-built binary and
regex-scraping its printed report. Session state persists through a
`SessionStore` interface (`FileSessionStore`, JSON file per session). Both
interfaces exist specifically so an HTTP/API layer, or a different storage
backend, can be swapped in later without touching callers -- per-session
policy (merge/overlap/normalize/run-cap/autofix knobs) is threaded through
`ServiceConfig` rather than hardcoded, for the same reason.

New domain model: `Session`/`SessionCue` (`commands/session.go`, JSON-tagged
throughout for a consistent API-ready shape), built from a subtitle file by
`NewSessionFromSubtitleFile`/`sessionCuesFromSubtitles` (`commands/upload.go`,
generalizes `core.parse_vtt`'s hardcoded voice detection to the engine's own
config-driven speaker resolution, and -- since the engine already supports
it -- honors an authored per-line `[Name@speed]` tag at upload time instead
of silently ignoring it the way Python's simplified parser does).
`rebuildCuesAfterGenerate` (`commands/generate.go`) collapses a merged group
of cues into one post-generate, ported from `core._cues_from_report`'s
reasoning but adapted from report-text scraping to direct struct access.

CLI commands: `session-upload`, `session-cleanup`, `session-generate`,
`session-autofix`, `session-export`, `session-update-cue`, `session-show`,
`session-reset` (`commands/session_cli.go`).

**HTTP API (section 8 item 11) — done, live-verified against the real,
unmodified frontend.** `commands/http.go` is a route-for-route equivalent
of dub-studio's `app.py` (GET `/session`, POST `/upload`/`/cleanup`/
`/generate`/`/autofix`/`/reduce`/`/cue/{index}`/`/export`, GET
`/export/download`, POST `/reset`, plus `/` and `/static/` when
`--static-dir` is given) -- `/reduce` was ported later, see the dedicated
entry below; at the time this milestone landed it was still excluded. New
`serve` CLI command. Depends only on `Service`/`SessionStore`, not on `LocalService`/
`FileSessionStore` concretely -- realizing the whole point of building
those as interfaces in the previous milestone. Response shape is adapted
at this transport boundary (`cueDTO`/`sessionDTO`) to exactly match what
`~/dub-studio/static/index.html` already expects (lowercase `voice` for
its hardcoded matko/hana check, tolerance-gated `flagged`/`est_flagged`/
`dirty` per cue, `first_pass_report` for phase detection, `cap`/`mode`/
`tolerance_ms`), kept separate from the CLI's own simpler DTO -- one
domain model, two transport-specific shapes. `Session.FirstPassReport`
added; `Session.Flagged` now takes an explicit `toleranceMs` (dub-studio's
core.py treats the overlap-gating tolerance and the "is this cue flagged"
threshold as literally the same constant).

Live-verified end-to-end with Playwright driving the actual unmodified
`static/index.html` against this server (no Playwright MCP tool was
available; installed the `playwright` npm package + Chromium into a
scratch prefix to dodge a broken global npm cache, per Matko's "we'll use
playwright" direction): upload -> cleanup -> generate (real ElevenLabs,
`sample_205.vtt`, 7 merged cues billed, overlap correctly detected) ->
autofix (correct speed bumps and basket reasoning, byte-for-byte matching
the ported logic) -> edited the two basketed lines via the real "edit
this line" UI flow -> re-generate (only the 4 dirty cues re-billed) ->
autofix again (surfaced a genuine ElevenLabs speed-parameter nonlinearity:
a cue needed further speeding after regenerating at its already-bumped
speed -- expected iterative behavior the loop phase exists for, not a
bug) -> one more edit+regenerate round -> export -> downloaded a valid
4.1MB stereo 16-bit 44.1kHz WAV with correct headers via `/export/download`.
One real bug found and fixed during this verification: dead code in
`handleUpload` checked a header that was never set instead of using
`save()`'s own return value.

5. **AI-assisted line shortening (`/reduce`)** — done, native Go port (not
   a byte-identical parity port -- see below), live-verified against the
   real Anthropic API.
   - PoC source: `core.reduce_text`/`_REDUCE_PROMPT`/`_LineShortenResult`
     (`~/dub-studio/core.py`). No PoC-side test exercises this against the
     real API either (it's inherently non-deterministic), so there is no
     parity bridge and none is planned -- this reverses the earlier
     "deliberately not ported" call once Matko decided to retire `app.py`
     entirely rather than keep a Python process alive just for this route.
   - Go test: `TestReduceTargetWords`/`TestReduceText`/
     `TestAnthropicHTTPClient_ShortenLine` (`commands/reduce_test.go`),
     `TestLocalService_Reduce` (`commands/service_test.go`). The
     AnthropicClient interface seam means every test but one scripts
     Claude's response through a fake -- no network, no spend. The
     remaining test (`.../live:_the_real_API_accepts_this_request_shape`)
     is a real call, skipped automatically when `ANTHROPIC_API_KEY` isn't
     set in the environment; run explicitly to verify the request shape
     against the live API when touching this file.
   - Go implementation: `ReduceText`/`reduceTargetWords`/`buildReducePrompt`
     (`commands/reduce.go`) reproduce the PoC's target-word-budget math and
     prompt text verbatim, reusing the existing `realBudgetsMs`/
     `toAutoFixCues` from the auto-fix port rather than recomputing budgets
     a second way. `anthropicHTTPClient` calls the Messages API directly
     over `net/http` (forced `tool_choice`, the Go equivalent of the Python
     SDK's `messages.parse(output_format=...)`) -- no new dependency, same
     reasoning as the `--normalize-target-db` decision to avoid a fresh
     external package for one call shape.
   - **Real bug found during live verification, fixed before landing:**
     the initial tool schema had no per-field `description`s (mirroring
     the PoC's own undecorated Pydantic model). Against the real API, on
     `action: "shorten"` the model put the *original* unshortened text in
     `text` and buried the actual rewritten line inside `reason`'s prose
     instead -- the bare field names `text`/`reason` don't convey "this
     field's meaning depends on `action`" on their own. Fixed by adding
     explicit descriptions to `text` and `reason` disambiguating their
     meaning per branch; re-verified live and `text` now correctly holds
     the shortened line. This same ambiguity likely exists unnoticed in
     the PoC's Python implementation too (same field names, same lack of
     descriptions, its own structured-output mechanism may or may not
     paper over it) -- worth keeping in mind if `reduce_text` is ever
     revisited there, though the PoC is slated for retirement anyway.
   - New CLI command: `session-reduce`. New flags `--anthropic-api-key`
     (falls back to the `ANTHROPIC_API_KEY` env var, mirroring the PoC's
     `config.py`) and `--reduce-model` (default `claude-haiku-4-5`, same
     as the PoC) on `serve` and `session-reduce`.
   - `SessionCue` gained `AiSuggestion`/`AiNote` fields (previously
     hardcoded to `""` in `cueDTO` with a comment saying this service has
     no AI line-shortening pass -- that comment is now stale and was
     removed); the frontend's existing "AI tried: ..." / "start from AI
     draft" UI, already wired to these exact field names, needed no
     changes.
   - Known exceptions: none functionally, beyond the schema-description
     fix above already being folded in.

Not yet started:
- Retiring the PoC's own `app.py`/`core.py` orchestration in favor of
  this one (section 11) -- the new HTTP API (now including `/reduce`) is
  proven to work standalone, but dub-studio itself hasn't been switched
  over to point at it yet. This is the actual next step, per Matko's
  2026-09-17 decision to replace `app.py` entirely rather than keep it
  running as a thin proxy or split traffic between the two.
- A known pre-existing UI quirk, faithfully reproduced rather than fixed:
  the frontend's Export button gates on a cue's `flagged` count (overage
  against its own subtitle *window*), not just real overlaps/basket
  state, so a cue that's fine by auto-fix's *real budget* metric but
  still over its own window can leave Export disabled in the UI even
  though the `/export` endpoint itself has nothing blocking it (verified
  directly: exported and downloaded successfully via the API in exactly
  that state). This is original `app.py`/`index.html` behavior, not
  something introduced by the port.
