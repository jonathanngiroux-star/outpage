# TUI audit (step 6 of 7)

Date: 2026-10-19. Scope: the Bubble Tea TUI shipped in step 5 — the
Model, the tea adapter, the wizard integration, and launch behavior.
Full battery re-run after every fix: `gofmt -l .`, `go vet ./...`,
`go test ./... -count=1`, `go test -race ./cmd/outpage/ -count=1`, the
15-case frontend suite (still green — the GUI shares the wizard), and
`CGO_ENABLED=1 go test -tags desktop ./cmd/outpage/ -count=1`. All green
at the end.

## Bugs found and fixed

1. **'q' was a dead key (P1).** The model swallowed 'q' as "quit" but no
   quit was ever issued — so 'q' neither quit nor typed. You could not
   name anything containing the letter q ("Queue", "sql") in any input
   line, and the documented quit key did nothing. Fix: the model treats
   'q' as a plain character; the quit decision moved to
   `quitOnKey(input, key)` — 'q' quits only on an empty input line,
   ctrl+c always quits; the tea adapter calls it before dispatching.
   Proof: `TestTUIQTypesWhenInputNonEmpty`, `TestQuitOnKeyDecision`.

2. **'b'/'s' hijacked wizard typing (P1).** Back and Skip fired on any
   keypress, so a page named "Status" triggered Skip at the last
   keystroke and a component named "Bob" triggered Back — the wizard
   input line could never contain those letters. Fix: 'b'/'s' act only
   on an EMPTY input line; with text being typed they are characters.
   Proof: `TestTUIWizardSandBAreTypedWhenInputNonEmpty` (types "Status"
   and "Bob" end-to-end through the wizard).

3. **Fixture re-parsed on every render (P2, performance).** The wizard's
   migrate step and the TUI migrate screen read + JSON-parsed the
   fixture on every `view()` — every keypress in the TUI is a render.
   Fix: `sync.Once` memoization per process with a parse-call counter;
   the audit test pins it at ≤1 across repeated renders of both
   surfaces. Proof: `TestMigratePreviewParsedOncePerProcess`.

4. **Wizard skip refusal now names the step** (P2, UX). "step is
   required" alone forces a README consult; the refusal now reads
   `step "page" is required`. Proof: `TestTUIWizardSkipErrorNamesStep`.

## Verified in this pass (no bug)

- Keyboard coverage: every screen's primary action is reachable
  (tab/shift+tab cycle, enter submits, '?' reopens the wizard); keys
  documented on every screen footer and in the README.
- First launch on an empty data dir enters the wizard; finishing
  creates the page name, component, and incident (store assertions).
- Reopen key works from any main screen.
- Resize to 80×24 does not crash, including mid-wizard (view is a pure
  string function).
- Same fixture numbers as the CLI (mapped=7 partial=3 unmapped=2,
  risks listed) — the migrate screen reads the same analyzer.
- Donate screen carries both addresses byte-for-byte.
- No blocking network on launch (the call-home gate already confines
  net/http to serve/subscribers; the TUI imports neither) and non-TTY
  launch prints a hint and exits 0 (verified live).
- Subscriber addresses never render in the TUI (counts only) — pinned
  by the step-5 tests.

## Noted, not fixed here (out of step-6 scope)

- The TUI shows subscriber counts, not a masked list; a list view is a
  product decision for a later step (the CLI and GUI both list).
- No color styling; plain rendering is deliberate (terminal-agnostic).
