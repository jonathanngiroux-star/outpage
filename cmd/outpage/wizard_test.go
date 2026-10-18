package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func wizardStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "wiz.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// drive advances the wizard one step with the given field values, as the
// GUI does (each Next click submits the step's fields).
func drive(t *testing.T, w *wizard, vals map[string]string) {
	t.Helper()
	if err := w.next(vals); err != nil {
		t.Fatalf("wizard next at step %d: %v", w.step, err)
	}
}

// The happy path creates real objects and marks the wizard done.
func TestWizardHappyPathCreatesObjects(t *testing.T) {
	s := wizardStore(t)
	w := newWizard(s)
	if got := w.current().ID; got != "welcome" {
		t.Fatalf("first step = %q, want welcome", got)
	}
	drive(t, w, nil) // welcome
	drive(t, w, map[string]string{"page_name": "Example Cloud"})
	drive(t, w, map[string]string{"component_name": "API", "component_description": "REST"})
	drive(t, w, map[string]string{"incident_name": "First incident", "incident_impact": "minor"})
	drive(t, w, map[string]string{"subscriber_email": "ops@example.net"})
	drive(t, w, nil) // migrate preview (optional, Next = continue)
	drive(t, w, nil) // donate + finish

	if !w.finished {
		t.Fatal("wizard did not finish")
	}
	if !s.WizardDone() {
		t.Fatal("wizard_done not persisted")
	}
	if got := s.PageName(); got != "Example Cloud" {
		t.Fatalf("page name = %q", got)
	}
	comps := s.ListComponents()
	if len(comps) != 1 || comps[0].Name != "API" || comps[0].Description != "REST" {
		t.Fatalf("wizard component: %+v", comps)
	}
	incs, _ := s.ListIncidents(0, false)
	if len(incs) != 1 || incs[0].Name != "First incident" || incs[0].Status != "investigating" {
		t.Fatalf("wizard incident: %+v", incs)
	}
	names, _ := s.IncidentComponents(incs[0].ID)
	if len(names) != 1 || names[0] != "API" {
		t.Fatalf("wizard incident components: %v", names)
	}
	subs, _ := s.ListSubscribers()
	if len(subs) != 1 || subs[0].Kind != "email" || subs[0].Address != "ops@example.net" {
		t.Fatalf("wizard subscriber: %+v", subs)
	}
}

// Validation: required steps refuse empty values naming the field; the
// wizard stays put on error.
func TestWizardValidationErrors(t *testing.T) {
	s := wizardStore(t)
	w := newWizard(s)
	drive(t, w, nil)                                 // welcome
	drive(t, w, map[string]string{"page_name": "P"}) // page
	// Component step: empty name is refused.
	if err := w.next(map[string]string{"component_name": "  "}); err == nil ||
		!strings.Contains(err.Error(), "component_name") {
		t.Fatalf("empty component name: %v, want error naming the field", err)
	}
	if w.current().ID != "component" {
		t.Fatalf("wizard advanced on error: %q", w.current().ID)
	}
	drive(t, w, map[string]string{"component_name": "API"})
	// Incident step: empty name refused; bad impact surfaces store error.
	if err := w.next(map[string]string{"incident_name": ""}); err == nil ||
		!strings.Contains(err.Error(), "incident_name") {
		t.Fatalf("empty incident name: %v", err)
	}
	if err := w.next(map[string]string{"incident_name": "X", "incident_impact": "huge"}); err == nil {
		t.Fatal("invalid impact must surface the store error")
	}
	drive(t, w, map[string]string{"incident_name": "X", "incident_impact": "minor"})
	_ = s
}

// Skip exists only on truly optional steps; required steps refuse it.
func TestWizardSkipOnlyOptional(t *testing.T) {
	s := wizardStore(t)
	w := newWizard(s)
	drive(t, w, nil)
	drive(t, w, map[string]string{"page_name": "P"})
	// component step is required: skip refused
	if err := w.skip(); err == nil {
		t.Fatal("skip on required step accepted")
	}
	drive(t, w, map[string]string{"component_name": "API"})
	drive(t, w, map[string]string{"incident_name": "I", "incident_impact": "minor"})
	// subscriber step is optional: skip works (no subscriber created)
	if err := w.skip(); err != nil {
		t.Fatalf("skip subscriber: %v", err)
	}
	if got := w.current().ID; got != "migrate" {
		t.Fatalf("after skip: %q", got)
	}
	if n, _, _ := s.SubscriberCounts(); n != 0 {
		t.Fatal("skip created a subscriber")
	}
	// migrate step is optional too
	if err := w.skip(); err != nil {
		t.Fatalf("skip migrate: %v", err)
	}
	if got := w.current().ID; got != "donate" {
		t.Fatalf("after skip migrate: %q", got)
	}
	// donate is the last step: skip refused, next finishes
	if err := w.skip(); err == nil {
		t.Fatal("skip on last step accepted")
	}
	drive(t, w, nil)
	if !w.finished || !s.WizardDone() {
		t.Fatal("finish did not complete")
	}
}

// Back walks to the previous step without re-applying anything.
func TestWizardBack(t *testing.T) {
	s := wizardStore(t)
	w := newWizard(s)
	drive(t, w, nil)
	drive(t, w, map[string]string{"page_name": "P"})
	drive(t, w, map[string]string{"component_name": "API"})
	w.back()
	if got := w.current().ID; got != "component" {
		t.Fatalf("back landed on %q", got)
	}
	// Values survive going back so the user can edit.
	if w.values["component_name"] != "API" {
		t.Fatalf("values lost on back: %+v", w.values)
	}
	// Re-applying with a new name updates the value store; the component
	// already exists, so a duplicate surfaces as a step error (user edits).
	if err := w.next(map[string]string{"component_name": "API"}); err == nil {
		t.Fatal("duplicate component on re-apply must surface")
	}
	drive(t, w, map[string]string{"component_name": "Web"})
	if got := len(s.ListComponents()); got != 2 {
		t.Fatalf("components after back+edit = %d, want 2", got)
	}
}

// The last step carries both donate addresses, byte-for-byte.
func TestWizardDonateLastStepCarriesAddresses(t *testing.T) {
	s := wizardStore(t)
	w := newWizard(s)
	for w.current().ID != "donate" && !w.finished {
		if w.current().Optional {
			if err := w.skip(); err != nil {
				t.Fatal(err)
			}
			continue
		}
		vals := map[string]string{
			"page_name":        "P",
			"component_name":   "API",
			"incident_name":    "I",
			"incident_impact":  "minor",
			"subscriber_email": "",
		}
		if err := w.next(vals); err != nil {
			t.Fatal(err)
		}
	}
	v := w.view()
	if !strings.Contains(v.Step.Body, donate.Ethereum) || !strings.Contains(v.Step.Body, donate.Bitcoin) {
		t.Fatalf("donate step missing addresses:\n%s", v.Step.Body)
	}
	if !strings.Contains(v.Step.Body, "Finish") {
		t.Fatalf("last step must say Finish:\n%s", v.Step.Body)
	}
}

// Reopen: a finished store can run the wizard again (Help → Open wizard);
// finishing again is harmless and objects created stack normally.
func TestWizardReopenAfterFinish(t *testing.T) {
	s := wizardStore(t)
	w := newWizard(s)
	drive(t, w, nil)
	drive(t, w, map[string]string{"page_name": "P"})
	drive(t, w, map[string]string{"component_name": "API"})
	drive(t, w, map[string]string{"incident_name": "I", "incident_impact": "minor"})
	if err := w.skip(); err != nil { // subscriber
		t.Fatal(err)
	}
	if err := w.skip(); err != nil { // migrate
		t.Fatal(err)
	}
	drive(t, w, nil) // finish
	if !s.WizardDone() {
		t.Fatal("not done")
	}
	w2 := newWizard(s)
	if got := w2.current().ID; got != "welcome" {
		t.Fatalf("reopened wizard starts at %q, want welcome", got)
	}
}

// The migrate-preview step shows the real dry-run counts from the
// committed fixture — the same numbers the CLI prints.
func TestWizardMigratePreviewRealNumbers(t *testing.T) {
	s := wizardStore(t)
	w := newWizard(s)
	drive(t, w, nil)
	drive(t, w, map[string]string{"page_name": "P"})
	drive(t, w, map[string]string{"component_name": "API"})
	drive(t, w, map[string]string{"incident_name": "I", "incident_impact": "minor"})
	if err := w.skip(); err != nil {
		t.Fatal(err)
	}
	if w.current().ID != "migrate" {
		t.Fatalf("expected migrate step, got %q", w.current().ID)
	}
	v := w.view()
	if !strings.Contains(v.Migrate, "mapped=7") || !strings.Contains(v.Migrate, "partial=3") || !strings.Contains(v.Migrate, "unmapped=2") {
		t.Fatalf("migrate preview missing real counts:\n%s", v.Migrate)
	}
}
