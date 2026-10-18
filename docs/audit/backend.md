# Backend audit (step 2 of 7)

Date: 2026-10-18. Scope: the backend shipped in step 1 — store, incident
state machine, public page renderer, subscribers (SMTP + webhook), the
Statuspage dry-run report, and the CLI/serve wiring. Full battery re-run
after every fix: `gofmt -l .`, `go vet ./...`, `go test ./... -count=1`,
`go test -race ./internal/store/ ./internal/serve/ ./internal/subscribers/`,
plus both fuzz targets (20s each: `FuzzAnalyze` 1.2M execs,
`FuzzSignVerify` 430k execs, zero crashes). All green at the end.

## Bugs found and fixed

1. **Component names were not trimmed on incident/maintenance link**
   (P1). `outpage incident open --components "API, Web"` splits on commas
   and yields `" Web"` — a false `not found` for a component that exists.
   The same untrimmed name twice also hit the `incident_components`
   UNIQUE constraint and failed the whole insert. Fix: trim + skip-empty
   in both `OpenIncident` and `OpenMaintenance`, and `INSERT OR IGNORE`
   for the link so a repeated name is a no-op instead of an error.
   Proof: `TestIncidentComponentNamesTrimmed`
   (internal/store/concurrency_test.go).

2. **No committed golden for the migrate report** (P1, contract gap).
   The jq gate in CI pinned scalar counts, but a field rename or ordering
   change inside `mappings`/`risks` would have passed silently. Fix: a
   committed golden (`testdata/golden/statuspage-report.json`) plus
   `TestGoldenReportStable` asserting byte-for-byte equality, and an
   in-repo generator (`go run ./cmd/gentestdata <fixture>`) so the golden
   is regenerated in the same change that intentionally changes the
   contract. CI now diffs the golden instead of trusting counts alone.

3. **The no-call-home rule was not enforced by any gate** (P2, rule
   gap). Outpage must never dial anywhere except the user's SMTP relay
   and user-configured webhook URLs. Fix: `TestNoCallHome` walks all
   non-test Go sources: `net/http` may be imported only by
   `internal/serve` (inbound), `internal/subscribers` (delivery to
   user-configured targets), and `cmd/outpage` (serve wiring); no
   `http.Get/Post/Head/DefaultClient` anywhere; hardcoded remote URLs
   (regex on real URL tokens) are rejected unless example/loopback. Two
   false positives in the first gate cut (error-string prose containing
   `https://`) taught the gate to match URL tokens, not prose.

4. **Negation-window weakness in the billing walk, pre-empted** (P2).
   The donate-exact test's own banned-vocabulary list ("stripe", "contact
   sales", "subscription", "invoice") made it trip the repo-wide billing
   walk. The walk now skips exactly that one test file — the narrowest
   skip list, not a rule change.

5. **Race audit of the store's write paths** (verification, no bug).
   `TestConcurrentAddUpdateSingleWinner` (16 workers racing one legal
   transition: exactly 1 winner, 15 `ErrIllegalTransition`, final state
   consistent) and `TestConcurrentAddComponent` (50 distinct adds land;
   50 identical-name adds collapse to exactly 1 row via the UNIQUE
   constraint — no check-then-insert window) both pass under `-race`.
   Timestamps are INTEGER unix nanos everywhere (no TEXT-ordering trap).

## Gates now standing in CI

- donate-exact: both addresses byte-for-byte, exactly once each
- migrate fixture gate: jq contract on counts/keys/risks
- golden drift: `diff` against the committed report
- billing walk: no payment vocabulary in shipped files
- call-home walk: no dials beyond user-configured targets
- fixture hygiene: no live secrets in checked-in fixtures
- serve smoke: boot + page + /donate + 404-no-admin
- race detector on store/serve/subscribers

## Noted, not fixed here (out of step-2 scope)

- Webhook retry/backoff policy (single attempt, recorded; policy is a
  product decision for a later step).
- Rate limiting on the public page (operator's reverse proxy owns this
  — documented in docs/custom-domain.md).
