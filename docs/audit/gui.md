# GUI audit (step 4 of 7) — Wails desktop

Date: 2026-10-18. Scope: the Wails v2 desktop GUI shipped in step 3 —
the desktopApp binding layer, the embedded vanilla frontend, and the
wizard. Full battery re-run after every fix: `gofmt -l .`,
`go vet ./...`, `go vet -tags desktop ./cmd/outpage/`,
`go test ./... -count=1`, `CGO_ENABLED=1 go test -tags desktop
./cmd/outpage/ -count=1`, `node --check cmd/outpage/frontend/app.js`,
`node cmd/outpage/frontend/test/app.test.mjs` (15/15), and
`CGO_ENABLED=1 go build -tags desktop,production,webkit2_41`. All green
at the end.

## Bugs found and fixed

1. **`esc()` was a no-op — operator text reached the DOM raw (P0).**
   The HTML-escape helper had been written with literal entity strings
   (`"&"` → `"&"`), which tooling decoded back into identity replacements
   at write time. Every list interpolated operator names, incident
   titles, and update bodies unescaped. Fix: rebuilt `esc()` with
   `\u0026`-escaped replacement strings (undecodable in source), plus a
   dedicated regression test ("esc() survives entity-decoding
   regressions") asserting `<img src=x onerror=…>` and `a & b < c`
   never appear raw in rendered lists. Proof: frontend tests 6 and 15.

2. **No incident update/resolve UI (P0, step-4 check "resolve incident,
   public page updates").** Step 3 shipped open-only. Fix: every incident
   timeline row now carries an inline update form (status select from
   the legal vocabulary + body + Post update + Resolve); posting
   refreshes the incident list and the public-page preview. The illegal
   transition error names the current state (same contract as the CLI).
   Proof: "incident update/resolve UI posts updates and shows transition
   errors".

3. **First-run wizard was gated behind data loads (P1).** `boot()`
   awaited `refreshAll()` before showing the wizard — a slow store
   blanked the front door. Fix: the wizard shows immediately; list
   refreshes continue in the background and render Loading states until
   the bindings answer. Proof: "fresh boot opens the wizard at welcome"
   (uses a never-resolving mock to pin this exact case).

4. **No loading states (P1).** Lists were empty (indistinguishable from
   "no data") until the first binding answered. Fix: explicit Loading
   state in all three list surfaces, replaced by real data or the
   empty-state note. Proof: same test as above.

5. **No keyboard path for primary actions outside the wizard (P1).**
   Fix: Enter submits the focused view's form (component add, incident
   open, maintenance schedule, subscriber add, timeline update), with
   the wizard overlay owning the keyboard while open. Proof: "keyboard:
   Enter submits the component form".

6. **`go:embed all:frontend` shipped the test directory in the binary
   (P2).** Fix: `noTestFS` wrapper hides `test/` from the embedded asset
   server (Open + ReadDir); tests ship in the repo, not the binary.

## Verification coverage added

- A zero-dependency Node frontend test (`frontend/test/app.test.mjs`)
  drives the REAL app.js in a vm against a mock binding layer that
  mirrors the Go desktopApp contract: 15 cases covering the wizard
  happy path (lands non-empty), validation errors staying put, donate
  addresses byte-for-byte on both the donate view and the last wizard
  step, masked subscriber displays, the migrate viewer's unmapped rows,
  binding errors shown in-view, escaping, keyboard, nav, and Help
  reopen. Runs in CI (`node --check` + the test itself).
- The Go binding layer keeps its 9 desktop-tagged tests (component
  editor, incident lifecycle incl. state-naming errors, maintenance,
  masked subscribers, migrate numbers parity with CLI, donate
  addresses, public-page preview, wizard binding + back).

## Noted, not fixed here (out of step-4 scope)

- Component status editing from the GUI (set via CLI/TUI today); the
  binding exists (`SetComponentStatus`) and will get a control in the
  components view with the TUI step's shared surface work.
- Multi-select component linking in the incident composer (typed
  comma-separated today).
