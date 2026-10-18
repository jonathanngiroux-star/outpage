# GOVERNANCE.md

## License

- Entire project (store, page, incidents, subscribers, migrate, serve, GUI,
  TUI): **MIT** — see `LICENSE`.
- There is **no commercial cloud SKU and no dual license** in this project.
  Outpage is free to self-host; funding is optional donations only. Do not add
  a BSL/ELv2 layer here.

## Donations (the only funding mechanism)

- Ethereum / USDC (ERC-20): `0x85ee7E71f762d772599cbF1EC20E651B30657521`
- Bitcoin: `bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg`

No feature is gated, or ever will be, on donation amount. Donations are
optional and anonymous by default; an optional signed-message/memo format for
attribution is documented in `docs/donate.md`.

## Wallet changes

Changing, adding, or removing a donation address requires all of:

1. **4/4 maintainer consensus** of the governance council.
2. **30-day public notice** before the change lands.
3. A written **migration note** in `docs/donate.md` explaining what happened
   to the old address.

Maintainer seats: (1) founder, (2)–(4) to be filled by the first two sustained
external contributors plus one community advocate. **Until all four seats are
filled, no wallet or license change may proceed at all.**

## Contributions

- **DCO only.** Every commit carries `Signed-off-by` (`git commit -s`). No
  CLA, ever. Any surprise CLA request is a governance violation and gets
  rejected in the PR.
- No copyright assignment, no dual-licensing demands on contributors.

## License changes

Same rule as wallet changes: 4/4 consensus + 30-day public notice + a written
migration path for existing self-hosters. No relicensing under acquisition
pressure without the same process.

## Security

- Private report → 90-day fix window → coordinated release. `security.md`
  lands with the first public release.
- Fixtures must never contain live secrets or real customer PII. Synthetic
  `@example.net` addresses and metadata only.
