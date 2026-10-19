package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/page"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// The step-7 system gates: cross-surface parity, address identity, scope
// locks. These run in the default (cgo-free) build; the GUI parity gates
// live in system_desktop_test.go under the desktop tag.

var timeRe = regexp.MustCompile(`[A-Z][a-z]{2} \d+, \d{4} \d{2}:\d{2} UTC`)

// normalizeHTML strips rendered timestamps so two pages created at
// different instants compare byte-for-byte.
func normalizeHTML(html string) string {
	return timeRe.ReplaceAllString(html, "TIME")
}

// CLI and TUI incident creation must produce the SAME public HTML —
// one renderer, one data model, surfaces are thin.
func TestSystemIncidentCreateIdenticalHTMLAcrossCLIAndTUI(t *testing.T) {
	dir := t.TempDir()

	// Surface 1: the CLI.
	cliDB := filepath.Join(dir, "cli.db")
	if _, _, code := runArgs(t, "component", "add", "--name", "API", "--description", "REST", "--db", cliDB); code != 0 {
		t.Fatalf("cli component add: %d", code)
	}
	if _, _, code := runArgs(t, "incident", "open", "--name", "Elevated errors",
		"--impact", "minor", "--components", "API", "--db", cliDB); code != 0 {
		t.Fatalf("cli incident open: %d", code)
	}

	// Surface 2: the TUI model (store pre-marked wizard-done so the main
	// screens are under test, matching a real post-setup operator).
	tuiDB := filepath.Join(dir, "tui.db")
	st, err := store.Open(tuiDB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetWizardDone(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPageName("Parity"); err != nil {
		t.Fatal(err)
	}
	cliSt, err := store.Open(cliDB)
	if err != nil {
		t.Fatal(err)
	}
	defer cliSt.Close()
	if err := cliSt.SetPageName("Parity"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(st)
	m = m.update(typeText("API | REST"))
	m = m.update(keyMsg("enter"))
	m = m.update(keyMsg("tab")) // incidents
	m = m.update(typeText("Elevated errors | minor | API"))
	m = m.update(keyMsg("enter"))

	htmlCLI, err := page.Render(cliSt)
	if err != nil {
		t.Fatal(err)
	}
	htmlTUI, err := page.Render(st)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeHTML(htmlCLI) != normalizeHTML(htmlTUI) {
		t.Fatalf("CLI and TUI public HTML diverge:\n--- CLI ---\n%s\n--- TUI ---\n%s",
			normalizeHTML(htmlCLI), normalizeHTML(htmlTUI))
	}
}

// The migrate report must be byte-identical across surfaces: CLI JSON
// vs the TUI's sample report. The CLI's default --input is repo-root
// relative, so pass the fixture explicitly from the test's repo root.
func TestSystemMigrateReportIdenticalAcrossCLIAndTUI(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "testdata", "fixtures", "statuspage", "sample-export.json")
	out, _, code := runArgs(t, "migrate", "--from=statuspage", "--dry-run", "--format=json",
		"--input", fixture)
	if code != 0 {
		t.Fatalf("cli migrate: %d", code)
	}
	rep := sampleMigrateReport()
	if rep == nil {
		t.Fatal("TUI sample report nil")
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tuiJSON := string(b) + "\n"
	if tuiJSON != out {
		t.Fatalf("migrate JSON diverges between CLI and TUI:\n--- CLI ---\n%s\n--- TUI ---\n%s", out, tuiJSON)
	}
}

// Both donation addresses, byte-for-byte, exactly once each, in every
// surface and shipped doc: the donate command, the TUI donate screen,
// README, and GOVERNANCE.md (the GUI is gated in the desktop-tag file).
func TestSystemDonateAddressesIdenticalEverywhere(t *testing.T) {
	root := repoRoot(t)

	// CLI donate output.
	out, _, code := runArgs(t, "donate")
	if code != 0 {
		t.Fatal("donate command failed")
	}
	if strings.Count(out, donate.Ethereum) != 1 || strings.Count(out, donate.Bitcoin) != 1 {
		t.Fatalf("donate command: ETH=%d BTC=%d occurrences, want 1/1",
			strings.Count(out, donate.Ethereum), strings.Count(out, donate.Bitcoin))
	}

	// TUI donate screen.
	st, err := store.Open(filepath.Join(t.TempDir(), "sys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetWizardDone()
	m := NewModel(st)
	for i := 0; i < 4; i++ {
		m = m.update(keyMsg("tab"))
	}
	v := m.view()
	if !strings.Contains(v, donate.Ethereum) || !strings.Contains(v, donate.Bitcoin) {
		t.Fatalf("TUI donate screen missing addresses:\n%s", v)
	}

	// README + GOVERNANCE.
	for _, doc := range []string{"README.md", "GOVERNANCE.md", "docs/donate.md"} {
		b, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		s := string(b)
		if strings.Count(s, donate.Ethereum) != 1 || strings.Count(s, donate.Bitcoin) != 1 {
			t.Fatalf("%s: ETH=%d BTC=%d occurrences, want 1/1", doc,
				strings.Count(s, donate.Ethereum), strings.Count(s, donate.Bitcoin))
		}
	}
}

// Scope lock: no on-call / escalation / rotation code. Mentions are
// legal only in negation ("no escalations") — the same window rule as
// the billing walk. Internal briefs are skipped (gitignored, refusal
// voice).
func TestSystemNoOnCallProduct(t *testing.T) {
	root := repoRoot(t)
	banned := []string{"pagerduty", "escalation", "rotation", "on-call", "oncall", "alertmanager"}
	skipDirs := map[string]bool{".git": true, "node_modules": true, "frontend": false}
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
		base := filepath.Base(rel)
		// Skip: internal briefs (gitignored, refusal voice), enforcement
		// docs, this gate's own vocabulary, and the audit notes that
		// document the gates (they quote banned words to describe them).
		if rel == "IDEA.md" || rel == "AGENTS.md" ||
			base == "system_test.go" ||
			strings.HasPrefix(rel, "docs/audit/") ||
			(len(base) >= 3 && base[0] >= '0' && base[0] <= '9' && base[1] >= '0' &&
				base[1] <= '9' && base[2] == '-' && strings.HasSuffix(base, ".md")) {
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".js", ".md", ".json", ".yml", ".yaml", ".html", ".css", ".ts", ".sh":
		default:
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lower := strings.ToLower(string(b))
		for _, w := range banned {
			if strings.Contains(lower, w) && !containsNegation(lower, w) {
				violations = append(violations, rel+": non-negated \""+w+"\"")
			}
		}
		return nil
	})
	if len(violations) > 0 {
		t.Fatalf("on-call scope violations (status page only — refuse the incident-command surface):\n%s",
			strings.Join(violations, "\n"))
	}
}

// The GUI is still a Wails app, not a browser-only admin: wails.json
// present, desktop source runs wails.Run over the embedded frontend.
func TestSystemStillAWailsApp(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "cmd", "outpage", "wails.json")); err != nil {
		t.Fatal("wails.json missing — the GUI must stay Wails v2")
	}
	src, err := os.ReadFile(filepath.Join(root, "cmd", "outpage", "desktop_wails.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{"wails.Run", "assetserver", "go:embed all:frontend"} {
		if !strings.Contains(s, want) {
			t.Fatalf("desktop_wails.go missing %q", want)
		}
	}
	for _, f := range []string{"index.html", "app.js"} {
		if _, err := os.Stat(filepath.Join(root, "cmd", "outpage", "frontend", f)); err != nil {
			t.Fatalf("frontend/%s missing: %v", f, err)
		}
	}
	// The frontend calls Go bindings, not a remote admin API.
	fe, err := os.ReadFile(filepath.Join(root, "cmd", "outpage", "frontend", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fe), "window.go.main.desktopApp") {
		t.Fatal("frontend does not bind to the Go app — a browser-only admin is a failed step 3")
	}
}
