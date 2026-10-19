//go:build desktop

package main

// system_desktop_test.go: the GUI half of the step-7 cross-surface
// parity gates (run with `go test -tags desktop ./cmd/outpage/`).

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/page"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// CLI and GUI incident creation must produce the SAME public HTML.
func TestSystemIncidentCreateIdenticalHTMLAcrossCLIAndGUI(t *testing.T) {
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

	// Surface 2: the GUI binding layer.
	guiDB := filepath.Join(dir, "gui.db")
	gst, err := store.Open(guiDB)
	if err != nil {
		t.Fatal(err)
	}
	defer gst.Close()
	app := NewDesktopApp(gst)
	if err := app.CreateComponent("API", "REST"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.OpenIncident("Elevated errors", "minor", []string{"API"}); err != nil {
		t.Fatal(err)
	}

	// Equal page names on both stores (the wizard normally sets it).
	cliSt, err := store.Open(cliDB)
	if err != nil {
		t.Fatal(err)
	}
	defer cliSt.Close()
	cliSt.SetPageName("Parity")
	gst.SetPageName("Parity")

	htmlCLI, err := page.Render(cliSt)
	if err != nil {
		t.Fatal(err)
	}
	htmlGUI, err := page.Render(gst)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeHTML(htmlCLI) != normalizeHTML(htmlGUI) {
		t.Fatalf("CLI and GUI public HTML diverge:\n--- CLI ---\n%s\n--- GUI ---\n%s",
			normalizeHTML(htmlCLI), normalizeHTML(htmlGUI))
	}
}

// The GUI's migrate dry-run must equal the CLI's JSON byte-for-byte.
// The CLI's default --input is repo-root relative, so pass the fixture
// explicitly from the test's repo root.
func TestSystemMigrateReportIdenticalAcrossCLIAndGUI(t *testing.T) {
	fixture := filepath.Join(repoRoot(t), "testdata", "fixtures", "statuspage", "sample-export.json")
	out, _, code := runArgs(t, "migrate", "--from=statuspage", "--dry-run", "--format=json",
		"--input", fixture)
	if code != 0 {
		t.Fatalf("cli migrate: %d", code)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	app := NewDesktopApp(st)
	rep, err := app.MigrateDryRun("")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	guiJSON := string(b) + "\n"
	if guiJSON != out {
		t.Fatalf("migrate JSON diverges between CLI and GUI:\n--- CLI ---\n%s\n--- GUI ---\n%s", out, guiJSON)
	}
}

// The GUI's donate view carries the exact constants.
func TestSystemGUIAddressesMatchConstants(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := NewDesktopApp(st).DonateAddresses()
	if d.Ethereum != donate.Ethereum || d.Bitcoin != donate.Bitcoin {
		t.Fatalf("GUI donate drifted: %+v", d)
	}
}
