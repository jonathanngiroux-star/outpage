package donate

import (
	"strings"
	"testing"
)

// The two addresses, byte-for-byte, exactly once each in Text() — the
// donation rule's canonical gate. No payment-ask vocabulary anywhere.
func TestDonateTextExact(t *testing.T) {
	got := Text()
	wantETH := "Ethereum / USDC (ERC-20): 0x85ee7E71f762d772599cbF1EC20E651B30657521"
	wantBTC := "Bitcoin: bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"
	if strings.Count(got, wantETH) != 1 {
		t.Fatalf("ETH address appears %d times, want exactly 1:\n%s", strings.Count(got, wantETH), got)
	}
	if strings.Count(got, wantBTC) != 1 {
		t.Fatalf("BTC address appears %d times, want exactly 1:\n%s", strings.Count(got, wantBTC), got)
	}
	if Ethereum != "0x85ee7E71f762d772599cbF1EC20E651B30657521" {
		t.Fatalf("Ethereum constant drifted: %s", Ethereum)
	}
	if Bitcoin != "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg" {
		t.Fatalf("Bitcoin constant drifted: %s", Bitcoin)
	}
	// Negation-gated words: "no subscription" is legal product copy; a
	// bare "subscription" is payment-ask language. Same rule as the
	// repo-wide no-billing walk.
	for _, w := range []string{"subscription", "invoice"} {
		if strings.Contains(strings.ToLower(got), w) && !hasNegationBefore(got, w) {
			t.Fatalf("donate text contains non-negated %q:\n%s", w, got)
		}
	}
	for _, banned := range []string{"stripe", "pricing", "upgrade", "billing", "contact sales"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Fatalf("donate text contains payment vocabulary %q:\n%s", banned, got)
		}
	}
}

// hasNegationBefore reports whether a negation word appears within ~40
// chars before every occurrence of w in s (case-insensitive).
func hasNegationBefore(s, w string) bool {
	lower := strings.ToLower(s)
	for {
		i := strings.Index(lower, strings.ToLower(w))
		if i < 0 {
			return true
		}
		window := lower[max(0, i-40):i]
		if !strings.Contains(window, "no ") && !strings.Contains(window, "not ") &&
			!strings.Contains(window, "never ") && !strings.Contains(window, "without ") {
			return false
		}
		lower = lower[i+len(w):]
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// HTML() is the footer block the public page and both UIs embed; it must
// carry both addresses and no form.
func TestDonateHTMLCarriesBothAddresses(t *testing.T) {
	got := HTML()
	if !strings.Contains(got, Ethereum) || !strings.Contains(got, Bitcoin) {
		t.Fatalf("HTML footer missing an address:\n%s", got)
	}
	if strings.Contains(strings.ToLower(got), "<form") || strings.Contains(strings.ToLower(got), "<input") {
		t.Fatalf("donate HTML must not contain a form:\n%s", got)
	}
}
