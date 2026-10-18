package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/incident"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Schema creation is idempotent and data persists across reopen — the
// single-file deploy story.
func TestOpenReopensWithPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.AddComponent("API", "rest"); err != nil {
		t.Fatalf("AddComponent: %v", err)
	}
	if err := s.SetPageName("Example Cloud"); err != nil {
		t.Fatalf("SetPageName: %v", err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	comps := s2.ListComponents()
	if len(comps) != 1 || comps[0].Name != "API" || comps[0].Description != "rest" {
		t.Fatalf("component did not persist: %+v", comps)
	}
	if got := s2.PageName(); got != "Example Cloud" {
		t.Fatalf("page name did not persist: %q", got)
	}
}

func TestAddComponentDuplicateRejected(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddComponent("API", ""); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := s.AddComponent("API", "again"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v, want ErrDuplicate", err)
	}
}

func TestAddComponentEmptyNameRejected(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddComponent("   ", ""); !errors.Is(err, incident.ErrInvalidStatus) {
		t.Fatalf("empty name: %v, want ErrInvalidStatus", err)
	}
}

func TestSetComponentStatusVocabulary(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{"operational", "degraded", "partial", "major", "maintenance"} {
		if err := s.SetComponentStatus("API", ok); err != nil {
			t.Fatalf("SetComponentStatus(%q): %v", ok, err)
		}
	}
	if err := s.SetComponentStatus("API", "exploded"); !errors.Is(err, incident.ErrInvalidStatus) {
		t.Fatalf("invalid status: %v, want ErrInvalidStatus", err)
	}
	if err := s.SetComponentStatus("Missing", "operational"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing component: %v, want ErrNotFound", err)
	}
}

// The full incident lifecycle through the store, including the terminal
// state: once resolved, nothing may move.
func TestIncidentLifecycleAndTerminalState(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	inc, err := s.OpenIncident("Errors", "minor", []string{"API"})
	if err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	if inc.Status != incident.StatusInvestigating {
		t.Fatalf("new incident status = %q, want investigating", inc.Status)
	}
	for _, next := range []string{"identified", "monitoring", "resolved"} {
		if _, err := s.AddUpdate(inc.ID, next, "update "+next); err != nil {
			t.Fatalf("AddUpdate(%s): %v", next, err)
		}
	}
	got, err := s.Incident(inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "resolved" || got.ResolvedNS == 0 {
		t.Fatalf("resolved incident: status=%q resolved_ns=%d", got.Status, got.ResolvedNS)
	}
	// Terminal: any further update is an illegal transition.
	if _, err := s.AddUpdate(inc.ID, "monitoring", "zombie"); !errors.Is(err, incident.ErrIllegalTransition) {
		t.Fatalf("update after resolve: %v, want ErrIllegalTransition", err)
	}
}

func TestOpenIncidentValidatesInputs(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenIncident("Bad impact", "huge", nil); !errors.Is(err, incident.ErrInvalidStatus) {
		t.Fatalf("bad impact: %v, want ErrInvalidStatus", err)
	}
	if _, err := s.OpenIncident("Unknown component", "minor", []string{"Nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown component: %v, want ErrNotFound", err)
	}
	if _, err := s.OpenIncident("", "minor", nil); !errors.Is(err, incident.ErrInvalidStatus) {
		t.Fatalf("empty name: %v, want ErrInvalidStatus", err)
	}
}

func TestMaintenanceWindowTimesAndTrack(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddComponent("DB", ""); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(2 * time.Hour)
	end := start.Add(2 * time.Hour)
	m, err := s.OpenMaintenance("Upgrade", []string{"DB"}, start, end)
	if err != nil {
		t.Fatalf("OpenMaintenance: %v", err)
	}
	if m.Status != incident.StatusScheduled || m.StartsNS != start.UnixNano() || m.EndsNS != end.UnixNano() {
		t.Fatalf("maintenance: %+v", m)
	}
	if _, err := s.AddUpdate(m.ID, "in_progress", "starting"); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	if _, err := s.AddUpdate(m.ID, "completed", "done"); err != nil {
		t.Fatalf("completed: %v", err)
	}
	// Incident-track statuses are illegal on a maintenance.
	if _, err := s.AddUpdate(m.ID, "investigating", "wrong track"); !errors.Is(err, incident.ErrIllegalTransition) {
		t.Fatalf("incident status on maintenance: %v, want ErrIllegalTransition", err)
	}
}

func TestMaintenanceValidatesWindow(t *testing.T) {
	s := openTestStore(t)
	start := time.Now()
	if _, err := s.OpenMaintenance("Bad", nil, start, start.Add(-time.Hour)); !errors.Is(err, incident.ErrInvalidStatus) {
		t.Fatalf("end before start: %v, want ErrInvalidStatus", err)
	}
}

func TestIncidentComponentsLinked(t *testing.T) {
	s := openTestStore(t)
	for _, n := range []string{"API", "Web"} {
		if _, err := s.AddComponent(n, ""); err != nil {
			t.Fatal(err)
		}
	}
	inc, err := s.OpenIncident("Multi", "major", []string{"API", "Web"})
	if err != nil {
		t.Fatal(err)
	}
	names, err := s.IncidentComponents(inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "API" || names[1] != "Web" {
		t.Fatalf("IncidentComponents = %v", names)
	}
}

func TestListIncidentsNewestFirstWithUpdates(t *testing.T) {
	s := openTestStore(t)
	first, _ := s.OpenIncident("First", "minor", nil)
	second, _ := s.OpenIncident("Second", "minor", nil)
	if _, err := s.AddUpdate(first.ID, "identified", "u1"); err != nil {
		t.Fatal(err)
	}
	incs, err := s.ListIncidents(0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(incs) != 2 || incs[0].ID != second.ID || incs[1].ID != first.ID {
		t.Fatalf("order: %+v", incs)
	}
	ups, err := s.IncidentUpdates(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	// One implicit "investigating" update is written at open, then u1.
	if len(ups) != 2 || ups[1].Body != "u1" {
		t.Fatalf("updates: %+v", ups)
	}
	resolved, _ := s.AddUpdate(second.ID, "resolved", "done")
	_ = resolved
	active, err := s.ListIncidents(0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("activeOnly: %+v", active)
	}
}

func TestSubscribersCRUDAndValidation(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddSubscriber("email", "ops@example.net", ""); err != nil {
		t.Fatalf("email: %v", err)
	}
	if _, err := s.AddSubscriber("webhook", "https://hooks.example.net/x", "sekrit"); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	if _, err := s.AddSubscriber("email", "ops@example.net", ""); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate email: %v", err)
	}
	if _, err := s.AddSubscriber("email", "not-an-email", ""); !errors.Is(err, ErrInvalidSubscriber) {
		t.Fatalf("bad email: %v, want ErrInvalidSubscriber", err)
	}
	if _, err := s.AddSubscriber("webhook", "ftp://nope", ""); !errors.Is(err, ErrInvalidSubscriber) {
		t.Fatalf("bad webhook: %v", err)
	}
	if _, err := s.AddSubscriber("sms", "+1555", ""); !errors.Is(err, ErrInvalidSubscriber) {
		t.Fatalf("sms: %v, want ErrInvalidSubscriber (no SMS gateway, ever)", err)
	}
	emails, hooks, err := s.SubscriberCounts()
	if err != nil {
		t.Fatal(err)
	}
	if emails != 1 || hooks != 1 {
		t.Fatalf("counts: email=%d webhook=%d", emails, hooks)
	}
	subs, err := s.ListSubscribers()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 || subs[1].Secret != "sekrit" {
		t.Fatalf("subscribers: %+v", subs)
	}
	if err := s.DeleteSubscriber(subs[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	emails, hooks, err = s.SubscriberCounts()
	if err != nil {
		t.Fatal(err)
	}
	if emails != 0 || hooks != 1 {
		t.Fatalf("counts after delete: email=%d webhook=%d", emails, hooks)
	}
}

func TestNotificationsLog(t *testing.T) {
	s := openTestStore(t)
	inc, _ := s.OpenIncident("N", "minor", nil)
	if err := s.RecordNotification(inc.ID, 7, "email", "failed", "connection refused"); err != nil {
		t.Fatalf("RecordNotification: %v", err)
	}
	logs, err := s.Notifications(inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Status != "failed" || logs[0].Detail != "connection refused" {
		t.Fatalf("notifications: %+v", logs)
	}
}

func TestWizardFlag(t *testing.T) {
	s := openTestStore(t)
	if s.WizardDone() {
		t.Fatal("fresh store reports wizard done")
	}
	if err := s.SetWizardDone(); err != nil {
		t.Fatal(err)
	}
	if !s.WizardDone() {
		t.Fatal("wizard flag did not persist")
	}
}
