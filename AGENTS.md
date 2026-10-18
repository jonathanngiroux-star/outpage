# AGENTS.md — Outpage working rules

Binding brief: `IDEA.md` and the numbered step briefs (gitignored, never shipped).
This file is the enforcement summary. If a request fights the wedge, refuse it
and name the trap.

## The wedge (only this)

- Single-binary public status page: Go + SQLite, one `outpage` process, no
  runtime dependencies.
- `outpage migrate --from=statuspage --dry-run` — the migration fidelity story
  is the product, not an afterthought.
- Public HTML status page readable without JavaScript.
- Incident lifecycle (open → update → resolve) and component status from the
  CLI (and later GUI/TUI, sharing one store).
- Scheduled maintenance windows.
- Email subscribers sent via user-supplied SMTP; webhook subscribers with
  HMAC-signed payloads. No SMS gateway, ever.
- Cold start in the 2-minute class, hard cap 15 minutes. Kubernetes is a bug.

## Monetization (binding override — series-wide)

- **No subscriptions. No invoices. No seats. No "contact sales." No Stripe, no
  payment processor, no usage billing. No cloud tier, no BSL/ELv2 SKU.**
- The only ask is optional donations:

  - Ethereum / USDC (ERC-20): `0x85ee7E71f762d772599cbF1EC20E651B30657521`
  - Bitcoin: `bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg`

- Both addresses go in README, `/donate`, `outpage donate`, and every UI
  footer. No other wallets without the maintainer saying so.
- Success metrics: self-host deploys, migrate dry-run usage, fixture quality,
  cold-start time, on-chain donations — **not ARR/MRR**.
- Named substitute cost ($300–$3,000/mo Atlassian Statuspage spend) may appear
  in the README as *why this exists* — never as a price we charge.
- If someone asks for a cloud SKU, point at `docs/donate.md` and stop.

## Explicitly out of scope (refuse on sight)

- Full incident command / on-call / PagerDuty clone (escalations, rotations,
  every alert source). Status page only.
- SMS vendor we operate.
- Subscription billing of any kind.
- Kubernetes.
- Multi-tenant hosted status as a product.
- Uptime probing / synthetic monitoring — Outpage displays what the operator
  says; it does not watch anything.

## Stack rules

- Go. SQLite via `modernc.org/sqlite` — no cgo in the default build (Docker,
  scratch, CI stay pure-Go).
- Timestamps stored as INTEGER unix nanos; ordering reads integers, never TEXT.
- Desktop GUI: Wails v2, compiled with `-tags desktop,production,webkit2_41`
  (cgo). Default binary stays cgo-free and explains how to get the GUI.
  Vanilla embedded frontend — no npm, no bundler.
- TUI: Bubble Tea, in-repo, over the same store — no second data model.
- MIT core; DCO-only contributions (`git commit -s`), no CLA, ever.
- GOVERNANCE.md: 4/4 maintainer consensus + 30-day public notice for license or
  donation-wallet changes.
- Solo-maintainable surface area is a hard constraint. Every added file is a
  liability.

## Definition of done (v0.1)

- [ ] One binary, SQLite, public page, `docker run` ≤ 15 min
- [ ] Incident lifecycle from CLI
- [ ] Email via user SMTP + webhook subscriber with signed payload
- [ ] Statuspage dry-run migrate with machine-stable report
- [ ] Donate command prints both addresses exactly
- [ ] No on-call product, no billing, no SMS gateway
