package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runArgs(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return out.String(), errb.String(), code
}

// runDB runs a command with --db appended after the command's own flags
// (the series convention: `outpage serve --db X`, never a leading global).
func runDB(t *testing.T, db string, args ...string) (string, string, int) {
	t.Helper()
	return runArgs(t, append(args, "--db", db)...)
}

func tmpDB(t *testing.T) string {
	return filepath.Join(t.TempDir(), "cli.db")
}

func TestVersion(t *testing.T) {
	out, _, code := runArgs(t, "version")
	if code != 0 || !strings.HasPrefix(out, "outpage ") {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
}

// The donate command prints both addresses exactly once each — the CI
// grep -Fx gate mirrors this byte-for-byte.
func TestDonateCommandExact(t *testing.T) {
	out, _, code := runArgs(t, "donate")
	if code != 0 {
		t.Fatalf("donate: code=%d", code)
	}
	if strings.Count(out, "Ethereum / USDC (ERC-20): 0x85ee7E71f762d772599cbF1EC20E651B30657521") != 1 {
		t.Fatalf("ETH line not exact-once:\n%s", out)
	}
	if strings.Count(out, "Bitcoin: bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg") != 1 {
		t.Fatalf("BTC line not exact-once:\n%s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, errOut, code := runArgs(t, "bogus")
	if code != 2 || !strings.Contains(errOut, "bogus") {
		t.Fatalf("unknown: code=%d err=%q", code, errOut)
	}
}

// Component lifecycle from the CLI.
func TestComponentCommands(t *testing.T) {
	db := tmpDB(t)
	out, _, code := runDB(t, db, "component", "add", "--name", "API", "--description", "REST")
	if code != 0 || !strings.Contains(out, "API") {
		t.Fatalf("component add: code=%d out=%q", code, out)
	}
	// Duplicate is an error.
	_, _, code = runDB(t, db, "component", "add", "--name", "API")
	if code == 0 {
		t.Fatal("duplicate component add must fail")
	}
	// Empty name is a usage error naming the problem.
	_, errOut, code := runDB(t, db, "component", "add", "--name", "  ")
	if code == 0 || !strings.Contains(errOut, "name") {
		t.Fatalf("empty name: code=%d err=%q", code, errOut)
	}
	// Status set + invalid vocabulary.
	_, _, code = runDB(t, db, "component", "status", "--name", "API", "--status", "degraded")
	if code != 0 {
		t.Fatalf("component status: %d", code)
	}
	_, errOut, _ = runDB(t, db, "component", "status", "--name", "API", "--status", "exploded")
	if !strings.Contains(errOut, "exploded") {
		t.Fatalf("invalid status must name the rejected value: %q", errOut)
	}
	// List shows it.
	out, _, _ = runDB(t, db, "component", "list")
	if !strings.Contains(out, "API") || !strings.Contains(out, "degraded") {
		t.Fatalf("component list: %q", out)
	}
}

// The incident lifecycle end-to-end through run(): open, update, resolve,
// then verify list reflects the state.
func TestIncidentLifecycleCLI(t *testing.T) {
	db := tmpDB(t)
	runDB(t, db, "component", "add", "--name", "API")
	out, _, code := runDB(t, db, "incident", "open",
		"--name", "Elevated errors", "--impact", "minor", "--components", "API")
	if code != 0 || !strings.Contains(out, "1") {
		t.Fatalf("incident open: code=%d out=%q", code, out)
	}
	// Illegal CLI transition: resolve then try to update again.
	_, _, code = runDB(t, db, "incident", "update", "--id", "1", "--status", "resolved", "--body", "Done")
	if code != 0 {
		t.Fatalf("resolve: %d", code)
	}
	_, errOut, code := runDB(t, db, "incident", "update", "--id", "1", "--status", "monitoring", "--body", "zombie")
	if code == 0 || !strings.Contains(errOut, "resolved") {
		t.Fatalf("update after resolve must fail naming the state: code=%d err=%q", code, errOut)
	}
	// List shows the resolved incident.
	out, _, _ = runDB(t, db, "incident", "list")
	if !strings.Contains(out, "Elevated errors") || !strings.Contains(out, "resolved") {
		t.Fatalf("incident list: %q", out)
	}
}

// Ambiguous CLI inputs are usage errors, never silent precedence picks.
func TestAmbiguousInputsRejected(t *testing.T) {
	db := tmpDB(t)
	// component add: both --description flag and a positional extra.
	_, errOut, code := runDB(t, db, "component", "add", "--name", "API", "extra")
	if code == 0 || !strings.Contains(strings.ToLower(errOut), "not both") {
		t.Fatalf("positional+flag: code=%d err=%q", code, errOut)
	}
	// migrate: --from=unknown names the rejected source.
	_, errOut, code = runArgs(t, "migrate", "--from=sendgrid", "--dry-run")
	if code == 0 || !strings.Contains(errOut, "sendgrid") {
		t.Fatalf("unknown --from: code=%d err=%q", code, errOut)
	}
}

// Migrate on the fixture: machine-stable JSON contract. The default
// --input path is repo-root relative (same as CI), so chdir there.
func TestMigrateFixtureJSON(t *testing.T) {
	t.Chdir(repoRoot(t))
	out, _, code := runArgs(t, "migrate", "--from=statuspage", "--dry-run", "--format=json")
	if code != 0 {
		t.Fatalf("migrate: code=%d err(out)=%s", code, firstN(out, 200))
	}
	var rep struct {
		Source string `json:"source"`
		DryRun bool   `json:"dry_run"`
		Counts struct {
			Mapped   int `json:"mapped"`
			Partial  int `json:"partial"`
			Unmapped int `json:"unmapped"`
		} `json:"counts"`
		Mappings []struct {
			Key    string `json:"key"`
			Result string `json:"result"`
		} `json:"mappings"`
		Risks []struct {
			Key string `json:"key"`
		} `json:"risks"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("migrate output is not JSON: %v\n%s", err, firstN(out, 300))
	}
	if rep.Source != "statuspage" || !rep.DryRun {
		t.Fatalf("header: %+v", rep)
	}
	if rep.Counts.Mapped != 7 || rep.Counts.Partial != 3 || rep.Counts.Unmapped != 2 {
		t.Fatalf("counts: %+v", rep.Counts)
	}
	var unmapped []string
	for _, m := range rep.Mappings {
		if m.Result == "unmapped" {
			unmapped = append(unmapped, m.Key)
		}
	}
	if strings.Join(unmapped, ",") != "subscriber.sms,metrics.uptime" {
		t.Fatalf("unmapped keys: %v", unmapped)
	}
	var riskKeys []string
	for _, r := range rep.Risks {
		riskKeys = append(riskKeys, r.Key)
	}
	if !strings.Contains(strings.Join(riskKeys, ","), "webhook.secrets") {
		t.Fatalf("risks: %v", riskKeys)
	}
}

func TestMigrateExplicitInput(t *testing.T) {
	root := repoRoot(t)
	out, _, code := runArgs(t, "migrate", "--from=statuspage", "--dry-run",
		"--format=json", "--input", filepath.Join(root, "testdata", "fixtures", "statuspage", "sample-export.json"))
	if code != 0 || !strings.Contains(out, `"source": "statuspage"`) {
		t.Fatalf("migrate --input: code=%d out=%s", code, firstN(out, 120))
	}
	// Missing file names the file.
	_, errOut, code := runArgs(t, "migrate", "--from=statuspage", "--dry-run", "--input", "/no/such/file.json")
	if code == 0 || !strings.Contains(errOut, "/no/such/file.json") {
		t.Fatalf("missing input: code=%d err=%q", code, errOut)
	}
}

// Subscriber commands: add email + webhook, list, count, remove.
func TestSubscriberCommands(t *testing.T) {
	db := tmpDB(t)
	_, _, code := runDB(t, db, "subscriber", "add", "--kind", "email", "--address", "ops@example.net")
	if code != 0 {
		t.Fatalf("subscriber add email: %d", code)
	}
	_, _, code = runDB(t, db, "subscriber", "add", "--kind", "webhook",
		"--address", "https://hooks.example.net/x", "--secret", "s3cr3t")
	if code != 0 {
		t.Fatalf("subscriber add webhook: %d", code)
	}
	// SMS is refused by name.
	_, errOut, code := runDB(t, db, "subscriber", "add", "--kind", "sms", "--address", "+1555")
	if code == 0 || !strings.Contains(strings.ToLower(errOut), "sms") {
		t.Fatalf("sms refusal: code=%d err=%q", code, errOut)
	}
	out, _, _ := runDB(t, db, "subscriber", "list")
	if !strings.Contains(out, "ops@example.net") || !strings.Contains(out, "hooks.example.net") {
		t.Fatalf("subscriber list: %q", out)
	}
	out, _, _ = runDB(t, db, "subscriber", "count")
	if !strings.Contains(out, "email=1") || !strings.Contains(out, "webhook=1") {
		t.Fatalf("subscriber count: %q", out)
	}
	_, _, code = runDB(t, db, "subscriber", "remove", "--id", "1")
	if code != 0 {
		t.Fatalf("subscriber remove: %d", code)
	}
}

// Maintenance windows from the CLI with RFC3339 times.
func TestMaintenanceCommand(t *testing.T) {
	db := tmpDB(t)
	runDB(t, db, "component", "add", "--name", "DB")
	_, _, code := runDB(t, db, "maintenance", "add",
		"--name", "Upgrade", "--components", "DB",
		"--start", "2026-10-25T01:00:00Z", "--end", "2026-10-25T03:00:00Z")
	if code != 0 {
		t.Fatalf("maintenance add: %d", code)
	}
	out, _, _ := runDB(t, db, "incident", "list")
	if !strings.Contains(out, "Upgrade") || !strings.Contains(out, "maintenance") {
		t.Fatalf("incident list after maintenance: %q", out)
	}
	// Bad window: end before start.
	_, errOut, code := runDB(t, db, "maintenance", "add",
		"--name", "Bad", "--start", "2026-10-25T03:00:00Z", "--end", "2026-10-25T01:00:00Z")
	if code == 0 || !strings.Contains(strings.ToLower(errOut), "start") {
		t.Fatalf("bad window: code=%d err=%q", code, errOut)
	}
}

// Fixed-port serve smoke (the same shape CI runs): boots, serves the
// public page, /donate, 404s unknown paths, and never leaks subscriber
// emails into public HTML.
func TestServeSmokeFixedPort(t *testing.T) {
	db := tmpDB(t)
	runDB(t, db, "component", "add", "--name", "API", "--description", "REST")
	runDB(t, db, "subscriber", "add", "--kind", "email", "--address", "secret-ops@example.net")

	// serve runs until killed; do NOT wait on the goroutine (it never
	// returns — waiting deadlocks the suite). The listener dies with the
	// test process.
	go func() {
		var out bytes.Buffer
		_ = run([]string{"serve", "--db", db, "--http", "127.0.0.1:18471"}, &out, io.Discard)
	}()

	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://127.0.0.1:18471/")
		if err == nil {
			break
		}
		timeSleepBrief(t)
	}
	if err != nil {
		t.Fatalf("serve never booted: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "API") || !strings.Contains(string(body), "All systems operational") {
		t.Fatalf("public page missing component:\n%s", firstN(string(body), 400))
	}
	// Privacy: subscriber email must not be in the HTML.
	if strings.Contains(string(body), "secret-ops@example.net") {
		t.Fatal("subscriber email leaked into public HTML")
	}
	// Donate page.
	resp, err = http.Get("http://127.0.0.1:18471/donate")
	if err != nil {
		t.Fatalf("/donate: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "0x85ee7E71f762d772599cbF1EC20E651B30657521") ||
		!strings.Contains(string(body), "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg") {
		t.Fatal("/donate missing addresses")
	}
	// Unknown paths are 404; there is no admin surface.
	resp, err = http.Get("http://127.0.0.1:18471/admin")
	if err != nil {
		t.Fatalf("/admin: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("/admin = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestServeBadAddr(t *testing.T) {
	_, errOut, code := runArgs(t, "serve", "--db", tmpDB(t), "--http", "bogus")
	if code == 0 || !strings.Contains(errOut, "listen") {
		t.Fatalf("bad addr: code=%d err=%q", code, errOut)
	}
}

// repoRoot walks up to the nearest go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("no go.mod upward")
	return ""
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
