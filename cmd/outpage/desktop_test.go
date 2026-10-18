//go:build desktop

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func appStore(t *testing.T) (*store.Store, *desktopApp) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "gui.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, NewDesktopApp(s)
}

// Component editor path: create, duplicate error surfaces, status set,
// list returns it.
func TestAppComponents(t *testing.T) {
	_, app := appStore(t)
	if err := app.CreateComponent("API", "REST"); err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}
	if err := app.CreateComponent("API", "again"); err == nil {
		t.Fatal("duplicate must surface an error the UI shows")
	}
	comps := app.Components()
	if len(comps) != 1 || comps[0].Name != "API" || comps[0].Status != "operational" {
		t.Fatalf("Components: %+v", comps)
	}
	if err := app.SetComponentStatus("API", "degraded"); err != nil {
		t.Fatalf("SetComponentStatus: %v", err)
	}
	if err := app.SetComponentStatus("API", "exploded"); err == nil {
		t.Fatal("invalid status must error")
	}
	if got := app.Components()[0].Status; got != "degraded" {
		t.Fatalf("status after set: %q", got)
	}
}

// Incident composer: open on a component, illegal transitions error with
// the current state named, resolve works, updates list.
func TestAppIncidentLifecycle(t *testing.T) {
	_, app := appStore(t)
	if err := app.CreateComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	inc, err := app.OpenIncident("Elevated errors", "minor", []string{"API"})
	if err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	if inc.Status != "investigating" {
		t.Fatalf("status: %q", inc.Status)
	}
	// Illegal: resolved is a jump we allow via monitoring first? No —
	// investigating→resolved IS legal; monitoring→investigating is not.
	if err := app.UpdateIncident(inc.ID, "resolved", "done fast"); err != nil {
		t.Fatalf("UpdateIncident resolved: %v", err)
	}
	err = app.UpdateIncident(inc.ID, "monitoring", "zombie")
	if err == nil || !strings.Contains(err.Error(), "resolved") {
		t.Fatalf("update after resolve: %v, want error naming state", err)
	}
	incs := app.Incidents()
	if len(incs) != 1 || incs[0].Status != "resolved" {
		t.Fatalf("Incidents: %+v", incs)
	}
	ups := app.IncidentUpdates(inc.ID)
	if len(ups) != 2 {
		t.Fatalf("updates: %d", len(ups))
	}
	names := app.IncidentComponentNames(inc.ID)
	if len(names) != 1 || names[0] != "API" {
		t.Fatalf("incident components: %v", names)
	}
}

// Maintenance window from the GUI.
func TestAppMaintenance(t *testing.T) {
	_, app := appStore(t)
	if err := app.CreateComponent("DB", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.AddMaintenance("Upgrade", []string{"DB"}, "2026-10-25T01:00:00Z", "2026-10-25T03:00:00Z"); err != nil {
		t.Fatalf("AddMaintenance: %v", err)
	}
	if err := app.AddMaintenance("Bad", nil, "2026-10-25T03:00:00Z", "2026-10-25T01:00:00Z"); err == nil {
		t.Fatal("reversed window must error")
	}
	incs := app.Incidents()
	if len(incs) != 1 || incs[0].Kind != "maintenance" {
		t.Fatalf("maintenance in incidents: %+v", incs)
	}
}

// Subscriber list masks email addresses; the raw address is never in the
// GUI list payload.
func TestAppSubscribersMasked(t *testing.T) {
	_, app := appStore(t)
	if err := app.AddSubscriber("email", "ops@example.net", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.AddSubscriber("webhook", "https://hooks.example.net/x", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	subs := app.Subscribers()
	if len(subs) != 2 {
		t.Fatalf("Subscribers: %+v", subs)
	}
	for _, s := range subs {
		if strings.Contains(s.Display, "ops@example.net") {
			t.Fatalf("email not masked: %q", s.Display)
		}
		if strings.Contains(s.Display, "s3cr3t") {
			t.Fatalf("webhook secret leaked into list: %q", s.Display)
		}
	}
	e, w := app.SubscriberCounts()
	if e != 1 || w != 1 {
		t.Fatalf("counts: email=%d webhook=%d", e, w)
	}
	if err := app.RemoveSubscriber(1); err != nil {
		t.Fatalf("RemoveSubscriber: %v", err)
	}
	if e, _ = app.SubscriberCounts(); e != 0 {
		t.Fatalf("after remove: email=%d", e)
	}
}

// Migrate dry-run viewer: same fixture numbers as the CLI.
func TestAppMigrateDryRun(t *testing.T) {
	_, app := appStore(t)
	rep, err := app.MigrateDryRun("")
	if err != nil {
		t.Fatalf("MigrateDryRun: %v", err)
	}
	if rep.Source != "statuspage" || rep.Counts.Mapped != 7 || rep.Counts.Partial != 3 || rep.Counts.Unmapped != 2 {
		t.Fatalf("report: %+v", rep.Counts)
	}
}

// Donate view: both addresses byte-for-byte.
func TestAppDonateAddresses(t *testing.T) {
	_, app := appStore(t)
	d := app.DonateAddresses()
	if d.Ethereum != donate.Ethereum || d.Bitcoin != donate.Bitcoin {
		t.Fatalf("donate addresses drifted: %+v", d)
	}
	if !strings.Contains(d.Note, "no feature is gated") {
		t.Fatalf("donate note must say no gating: %q", d.Note)
	}
}

// The GUI can preview the exact public page HTML — same renderer as serve,
// and it must not contain subscriber emails.
func TestAppPublicPagePreview(t *testing.T) {
	_, app := appStore(t)
	if err := app.CreateComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.AddSubscriber("email", "secret-ops@example.net", ""); err != nil {
		t.Fatal(err)
	}
	html, err := app.PublicPagePreview()
	if err != nil {
		t.Fatalf("PublicPagePreview: %v", err)
	}
	if !strings.Contains(html, "API") {
		t.Fatal("preview missing component")
	}
	if strings.Contains(html, "secret-ops@example.net") {
		t.Fatal("preview leaked subscriber email")
	}
	if !strings.Contains(html, donate.Ethereum) {
		t.Fatal("preview footer missing donate address")
	}
}

// The wizard is bound through the app: first launch on an empty store
// starts at welcome; the reopen path works on a done store.
func TestAppWizardBinding(t *testing.T) {
	s, app := appStore(t)
	v := app.WizardView()
	if v.Step.ID != "welcome" {
		t.Fatalf("first WizardView step: %+v", v.Step)
	}
	// Finish the whole wizard through the binding.
	fields := map[string]string{
		"page_name":       "P",
		"component_name":  "API",
		"incident_name":   "I",
		"incident_impact": "minor",
	}
	v, err := app.WizardNext(nil)
	if err != nil {
		t.Fatalf("welcome next: %v", err)
	}
	v, err = app.WizardNext(map[string]string{"page_name": "P"})
	if err != nil {
		t.Fatalf("page next: %v", err)
	}
	v, err = app.WizardNext(map[string]string{"component_name": "API"})
	if err != nil {
		t.Fatalf("component next: %v", err)
	}
	_ = fields
	v, err = app.WizardNext(map[string]string{"incident_name": "I", "incident_impact": "minor"})
	if err != nil {
		t.Fatalf("incident next: %v", err)
	}
	if v.Step.ID != "subscriber" {
		t.Fatalf("expected subscriber step: %+v", v.Step)
	}
	v, err = app.WizardSkip()
	if err != nil {
		t.Fatalf("subscriber skip: %v", err)
	}
	v, err = app.WizardSkip()
	if err != nil {
		t.Fatalf("migrate skip: %v", err)
	}
	if v.Step.ID != "donate" {
		t.Fatalf("expected donate step: %+v", v.Step)
	}
	v, err = app.WizardNext(nil)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !v.Finished {
		t.Fatal("binding did not finish")
	}
	if !s.WizardDone() {
		t.Fatal("wizard_done not set")
	}
	// Reopen via the binding.
	v = app.WizardReopen()
	if v.Step.ID != "welcome" {
		t.Fatalf("reopen: %+v", v.Step)
	}
}

// Back through the binding.
func TestAppWizardBackBinding(t *testing.T) {
	_, app := appStore(t)
	app.WizardNext(nil)
	app.WizardNext(map[string]string{"page_name": "P"})
	v, err := app.WizardBack()
	if err != nil {
		t.Fatalf("back: %v", err)
	}
	if v.Step.ID != "page" {
		t.Fatalf("back landed on %+v", v.Step)
	}
	// Wizard state survives across calls (the app keeps one wizard).
	if v := app.WizardView(); v.Step.ID != "page" {
		t.Fatalf("view after back: %+v", v.Step)
	}
}
