// Package donate holds the project's donation addresses — the only
// funding mechanism, per GOVERNANCE.md. Both surfaces (public page,
// GUI/TUI footers) and the `outpage donate` command render from these
// constants so the strings can never drift between surfaces.
package donate

// Ethereum / USDC (ERC-20) and Bitcoin addresses. Binding, byte-for-byte;
// do not change without 4/4 maintainer consensus + 30-day public notice
// (GOVERNANCE.md).
const (
	Ethereum = "0x85ee7E71f762d772599cbF1EC20E651B30657521"
	Bitcoin  = "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"
)

// Text is the exact block `outpage donate` prints: both addresses, once
// each, no payment-ask language.
func Text() string {
	return "Outpage is free to self-host, forever. There is no paid tier,\n" +
		"no subscription, and no feature is gated on money.\n" +
		"If it kills your weekly status chore, donations are the only ask:\n" +
		"\n" +
		"Ethereum / USDC (ERC-20): " + Ethereum + "\n" +
		"Bitcoin: " + Bitcoin + "\n"
}

// HTML is the footer block embedded in the public page, `/donate`, and the
// GUI/TUI footers. No forms, no inputs — donations happen on-chain, not
// through this product.
func HTML() string {
	return `<footer class="site-footer">
<p>Outpage is free, MIT-licensed software. Donations are the only funding — never a paywall, never counted as traction.</p>
<p class="donate-addresses">
Ethereum / USDC (ERC-20): <code>` + Ethereum + `</code><br>
Bitcoin: <code>` + Bitcoin + `</code>
</p>
</footer>`
}
