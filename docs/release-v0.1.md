# Outpage v0.1 — release notes (draft)

Outpage is a self-hosted public status page: one Go binary, one SQLite
file, no runtime dependencies. This is the first feature-complete cut:
the public page, the incident lifecycle, subscriber notifications, the
Statuspage migration report, and three operator surfaces (CLI, desktop
GUI, terminal UI) over one data model.

## What shipped

- **Public status page** — server-rendered HTML, readable without
  JavaScript: components with live status, active incidents with full
  timelines, scheduled maintenance, past-incident history, `/donate`.
  An enforced status state machine (investigating → identified →
  monitoring → resolved; resolved is terminal) and a fixed five-word
  component vocabulary.
- **Three surfaces, one store** — `outpage` CLI (scriptable),
  `outpage desktop` (Wails v2 GUI; build with `-tags
  desktop,production,webkit2_41`), and `outpage tui` (Bubble Tea,
  keyboard-only). Cross-surface parity is gate-tested: the same
  incident created from any surface renders the same public HTML, and
  the migrate report is byte-identical everywhere.
- **First-run setup wizard** in both the GUI and TUI — page name, first
  component, first incident, optional subscriber, optional migration
  preview — reopenable any time (GUI: Help; TUI: `?`).
- **Subscribers** — email through your own SMTP relay (skipped and
  recorded if unconfigured — never guessed), webhooks with
  HMAC-SHA256-signed payloads. No SMS gateway, ever. Addresses never
  appear in public HTML; the GUI list masks them.
- **Statuspage migration report** — `outpage migrate --from=statuspage
  --dry-run --format=json`: machine-stable mapped/partial/unmapped
  classification with named dropped fields and operator risks, pinned
  by a committed golden and a jq CI gate. Nothing is mutated; dry-run
  is the only mode in v0.1.
- **Deploy** — one binary; `docker build` → scratch container; custom
  domain via your existing reverse proxy (docs/custom-domain.md).
  Cold start measured at 15 s from a clean tree.

## Funding

Outpage is free, MIT-licensed, and donation-funded only — no paid tier,
no subscription, no gated features, ever.

- ETH / USDC (ERC-20): `0x85ee7E71f762d772599cbF1EC20E651B30657521`
- BTC: `bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg`

## Quality gates (all green at tag time)

- `gofmt` / `go vet` clean (default and desktop builds)
- `go test ./...` — 8 packages, including race on the store/serve/
  subscribers/cmd paths
- Desktop binding tests (`-tags desktop`) and the 15-case frontend
  suite (real `app.js` against the binding contract)
- Fuzz targets clean: migrate analyzer (1.2M execs), webhook
  sign/verify (430k execs)
- Repo gates: donate-exact, no-billing-surface walk, no-call-home walk,
  fixture hygiene, no-on-call-product walk, still-a-Wails-app check
- Golden migrate report drift gate + jq contract
- End-to-end script (`scripts/e2e.sh`): build → lifecycle → real-HTTP
  page assertions → migrate contract → TUI non-TTY — PASS

## Deliberately not built

Incident command/on-call (escalations, rotations, alert ingestion),
uptime probing, an SMS gateway, billing of any kind, and any hosted
multi-tenant tier. Status page only — see AGENTS.md scope locks.

## Install

```bash
go build -o outpage ./cmd/outpage        # or the release binary
./outpage component add --name "API"
./outpage serve                          # http://127.0.0.1:8080/
```

Desktop GUI and full docs: [README.md](../README.md),
[docs/gui-setup.md](gui-setup.md).
