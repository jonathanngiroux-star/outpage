package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBillingSurface is the release gate for the donation-only rule: no
// payment processor, no pricing tiers, no "contact sales" anywhere in
// source or shipped docs. "subscription"/"invoice" may appear only inside
// an explicit negation ("no subscriptions", "without invoices", ...).
// Skipped files name banned concepts only to ban them, or are the
// gitignored internal briefs.
func TestNoBillingSurface(t *testing.T) {
	banned := []string{
		"stripe", "contact sales", "seat math", "per-seat",
		"cloud pro", "cloud starter", "enterprise tier",
	}
	skipDirs := map[string]bool{".git": true, "node_modules": true, ".hermes": true}
	skipFiles := map[string]bool{
		"docs/pricing.md": true, // the "no pricing" stub
		"AGENTS.md":       true, // agent-facing enforcement notes
		"GOVERNANCE.md":   true, // policy names the banned concepts to ban them
		// the audit note documents the billing walk itself (quotes banned
		// words to describe the gate) — skip it, same as the skill ref
		"docs/audit/backend.md": true,
		// the donate-exact test owns the banned vocabulary (it asserts its
		// absence) — skip only the file that tests the negation rule
		"internal/donate/donate_test.go": true,
	}

	root := "."
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		root = filepath.Join(root, "..")
	}

	var violations []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		// Internal planning briefs never ship (gitignored); excluded.
		base := filepath.Base(rel)
		if rel == "IDEA.md" || (len(base) >= 3 && base[0] >= '0' && base[0] <= '9' &&
			base[1] >= '0' && base[1] <= '9' && base[2] == '-' && strings.HasSuffix(base, ".md")) {
			return nil
		}
		if skipFiles[rel] || base == "billing_test.go" {
			return nil // this file owns the banned vocabulary
		}
		switch filepath.Ext(path) {
		case ".go", ".js", ".md", ".json", ".yml", ".yaml", ".html", ".css", ".ts":
		default:
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lower := strings.ToLower(string(b))
		for _, ban := range banned {
			if strings.Contains(lower, ban) {
				violations = append(violations, rel+": contains \""+ban+"\"")
			}
		}
		for _, w := range []string{"subscription", "invoice"} {
			if strings.Contains(lower, w) && !containsNegation(lower, w) {
				violations = append(violations, rel+": non-negated \""+w+"\"")
			}
		}
		return nil
	})
	if len(violations) > 0 {
		t.Fatalf("billing-surface violations (rewrite the text, never the gate):\n%s",
			strings.Join(violations, "\n"))
	}
}

// containsNegation reports whether every occurrence of w in s has a
// negation word within ~40 chars before it (case-insensitive).
func containsNegation(s, w string) bool {
	lower := strings.ToLower(s)
	target := strings.ToLower(w)
	for {
		i := strings.Index(lower, target)
		if i < 0 {
			return true
		}
		start := i - 40
		if start < 0 {
			start = 0
		}
		window := lower[start:i]
		if !strings.Contains(window, "no ") && !strings.Contains(window, "not ") &&
			!strings.Contains(window, "never ") && !strings.Contains(window, "without ") &&
			!strings.Contains(window, "isn't ") && !strings.Contains(window, "none ") {
			return false
		}
		lower = lower[i+len(target):]
	}
}
