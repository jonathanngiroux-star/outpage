# Outpage

Outpage is a self-hosted public status page: one Go binary, one SQLite
file, no runtime dependencies. It replaces the weekly chore of keeping a
status page current — components, incident timelines, scheduled
maintenance, and subscriber notifications — with a CLI you already know
how to script.

Teams commonly pay for hosted status-page products priced for enterprise
procurement. Outpage is the boring alternative: a single binary you run
next to your own services, with your data in one SQLite file on your own
disk. It is free, MIT-licensed, and funded by donations only.

## What it does

- **Public status page** — server-rendered HTML at `/`, readable without
  JavaScript; components with live status; active incidents with full
  timelines; scheduled maintenance; past-incident history. `/donate` shows
  the project addresses.
- **Incident lifecycle** — `outpage incident open|update|list` with an
  enforced state machine (investigating → identified → monitoring →
  resolved; resolved is terminal). Component status uses a fixed
  five-word vocabulary.
- **Scheduled maintenance** — `outpage maintenance add` with RFC3339
  windows.
- **Subscribers** — email via your own SMTP relay (config by env vars),
  webhooks with HMAC-SHA256 signed payloads the receiver can verify. No
  SMS gateway, ever. Subscriber addresses never appear in public HTML.
- **Statuspage migration report** — `outpage migrate --from=statuspage
  --dry-run --format=json`: machine-stable mapped/partial/unmapped
  classification of a real export, with named dropped fields and operator
  risks. Nothing is mutated; the report is the product.
- **Custom domain** — you terminate TLS at your existing reverse proxy;
  see [docs/custom-domain.md](docs/custom-domain.md).

**Outpage is not** an incident-command/on-call system: no escalations, no
rotations, no alert ingestion, no uptime probing. It publishes what the
operator says — fast.

## Quick start

```bash
go build -o outpage ./cmd/outpage

# seed one component and publish
./outpage component add --name "API" --description "public REST"
./outpage serve                    # http://127.0.0.1:8080/
```

Open an incident and your page is already updated:

```bash
./outpage incident open --name "Elevated API errors" --impact minor --components API
./outpage incident update --id 1 --status identified --body "Bad deploy rolled back."
./outpage incident update --id 1 --status resolved --body "Error rates back to baseline."
```

Email subscribers (through your SMTP relay):

```bash
./outpage subscriber add --kind email --address ops@example.net
export OUTPAGE_SMTP_HOST=smtp.example.net OUTPAGE_SMTP_FROM=status@example.net
# every incident open/update now notifies, best-effort, with failures
# recorded — an SMTP outage never rolls back the timeline.
```

Webhook subscribers get signed POSTs (`X-Outpage-Signature: sha256=…`,
HMAC-SHA256 over the raw body):

```bash
./outpage subscriber add --kind webhook --address https://hooks.example.net/x --secret "$SECRET"
```

Statuspage dry-run migration report:

```bash
./outpage migrate --from=statuspage --dry-run --format=json \
  --input statuspage-export.json
```

Docker (documented deploy path):

```bash
docker build -t outpage .
docker run -d -p 8080:8080 -v outpage-data:/data outpage
```

## Desktop GUI (Wails v2)

The desktop app is the same binary compiled with the webview backend:

```bash
CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 \
  -o outpage-desktop ./cmd/outpage
./outpage-desktop desktop
```

It covers every wedge action — component editor, incident composer with
timelines, maintenance scheduler, subscriber list (addresses masked),
Statuspage dry-run viewer, public-page preview, and the donate view.
First launch opens a step-by-step setup wizard that creates your page
name, first component, and first incident with your values; **Help**
reopens it any time. The default cgo-free build explains how to build
the GUI instead of crashing. Full setup — dependencies, tags, `wails
dev` hot reload: **[docs/gui-setup.md](docs/gui-setup.md)**.

## Terminal UI

`outpage tui` (or bare `outpage` in a terminal) opens the same workflows
in the terminal: components, incidents, subscriber counts (addresses
never render), the migrate summary, and the donate screen — same SQLite
file as the GUI and CLI. First launch runs the same setup wizard; `?`
reopens it. Keys are shown on every screen: `tab`/`shift+tab` switch
screens, `enter` submits the input line, `b`/`s` are wizard back/skip,
`q` quits. Non-TTY contexts print a hint instead of hanging.

## Status

Working today, covered by tests:

- `outpage version`, `outpage donate`
- Components: add / status (five-word vocabulary) / list
- Incidents: open / update / list with the enforced transition table
- Maintenance windows: `maintenance add`
- Subscribers: email + webhook add/list/count/remove, signed webhook
  delivery, SMTP delivery via user-supplied relay, per-incident delivery
  log
- `serve` public page + `/donate`; `page render` for static hosting
- `migrate --from=statuspage --dry-run` (+ `--format=json`) with a
  machine-stable report contract
- Desktop GUI (Wails v2, `-tags desktop,production,webkit2_41`): all
  wedge actions, first-run wizard, masked subscriber list, donate view
- Terminal UI (`outpage tui`, bare `outpage`): same wizard, same store,
  keyboard-only, no blocking network on launch

Planned (in progress, not yet shipped):

- Statuspage import beyond the dry-run report

**Deliberately not built** (scope locks, see [AGENTS.md](AGENTS.md)):
on-call/escalations, alert ingestion, an SMS gateway, billing of any
kind, and any hosted/multi-tenant tier.

## Install

Build from source (Go 1.27+):

```bash
go build -o outpage ./cmd/outpage
```

or use the Dockerfile. Prebuilt release binaries ship with v0.1.

## Donations

Outpage takes donations only — there is no paid tier, no subscription, and
no feature will ever be gated on money.

- **ETH / USDC (ERC-20):** `0x85ee7E71f762d772599cbF1EC20E651B30657521`
- **BTC:** `bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg`

Run `outpage donate` to print both addresses from the binary. Details and
the wallet-change governance rule:
[docs/donate.md](docs/donate.md) · [GOVERNANCE.md](GOVERNANCE.md).

## Docs

- [docs/migrate.md](docs/migrate.md) — the Statuspage migration report
- [docs/fidelity/statuspage.md](docs/fidelity/statuspage.md) — field-by-field mapping and honest gaps
- [docs/custom-domain.md](docs/custom-domain.md) — reverse proxy + TLS
- [docs/donate.md](docs/donate.md) — donation addresses and governance
- [docs/pricing.md](docs/pricing.md) — there is none

## License

MIT — see [LICENSE](LICENSE). Contributions are DCO-only
(`git commit -s`), no CLA, per [GOVERNANCE.md](GOVERNANCE.md).
