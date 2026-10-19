package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func tuiStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "tui.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// First launch on an empty data dir enters the wizard before the main
// screen (the brief's binding requirement).
func TestTUIFirstLaunchEntersWizard(t *testing.T) {
	m := NewModel(tuiStore(t))
	if !m.inWizard {
		t.Fatal("fresh store must open in wizard mode")
	}
	v := m.view()
	if !strings.Contains(v, "Welcome to Outpage") {
		t.Fatalf("wizard not at welcome:\n%s", v)
	}
}

// The TUI wizard drives the SAME shared wizard logic: one line per step
// (typed value fills the step's primary field; optional fields default).
// Finishing creates the page name, component, incident; the store flag
// persists.
func TestTUIWizardCreatesObjectsAndFinishes(t *testing.T) {
	s := tuiStore(t)
	m := NewModel(s)
	m = m.update(keyMsg("enter"))             // welcome → page
	m = m.update(typeText("Example Cloud"))   // page name
	m = m.update(keyMsg("enter"))             // → component
	m = m.update(typeText("API"))             // component name
	m = m.update(keyMsg("enter"))             // → incident
	m = m.update(typeText("Elevated errors")) // incident title (impact defaults to minor)
	m = m.update(keyMsg("enter"))             // → subscriber (optional)
	m = m.update(keyMsg("s"))                 // skip subscriber
	m = m.update(keyMsg("s"))                 // skip migrate
	if !strings.Contains(m.view(), donate.Ethereum) {
		t.Fatalf("donate step must carry the address:\n%s", m.view())
	}
	m = m.update(keyMsg("enter")) // finish
	if m.inWizard {
		t.Fatalf("wizard still open after finish:\n%s", m.view())
	}
	if !s.WizardDone() {
		t.Fatal("wizard_done not set")
	}
	if got := s.PageName(); got != "Example Cloud" {
		t.Fatalf("page name = %q", got)
	}
	comps := s.ListComponents()
	if len(comps) != 1 || comps[0].Name != "API" {
		t.Fatalf("components: %+v", comps)
	}
	incs, _ := s.ListIncidents(0, false)
	if len(incs) != 1 || incs[0].Name != "Elevated errors" || incs[0].Status != "investigating" {
		t.Fatalf("incidents: %+v", incs)
	}
}

// Back ('b') and Skip ('s') keys: back returns to the previous step;
// skip only on optional steps (subscriber + migrate); required steps
// refuse skip with a visible error.
func TestTUIWizardBackAndSkipKeys(t *testing.T) {
	s := tuiStore(t)
	m := NewModel(s)
	m = m.update(keyMsg("enter")) // welcome → page
	m = m.update(typeText("P"))
	m = m.update(keyMsg("enter")) // → component
	m = m.update(typeText("API"))
	m = m.update(keyMsg("enter")) // → incident (required)
	// skip on a required step: refused with a visible error
	m = m.update(keyMsg("s"))
	if !strings.Contains(m.view(), "required") {
		t.Fatalf("skip on required step must surface an error:\n%s", m.view())
	}
	if !strings.Contains(m.view(), "Open a sample incident") {
		t.Fatalf("refused skip must not advance:\n%s", m.view())
	}
	// back lands on the component step
	m = m.update(keyMsg("b"))
	if !strings.Contains(m.view(), "Create your first component") {
		t.Fatalf("back did not land on component step:\n%s", m.view())
	}
	// forward again: component → incident → (skip) subscriber
	m = m.update(typeText("API"))
	m = m.update(keyMsg("enter"))
	m = m.update(typeText("X"))
	m = m.update(keyMsg("enter"))
	// subscriber (optional): skip works
	m = m.update(keyMsg("s"))
	if !strings.Contains(m.view(), "Statuspage") {
		t.Fatalf("skip subscriber did not reach migrate step:\n%s", m.view())
	}
	if n, _, _ := s.SubscriberCounts(); n != 0 {
		t.Fatal("skip created a subscriber")
	}
	// migrate (optional): skip
	m = m.update(keyMsg("s"))
	if !strings.Contains(m.view(), donate.Ethereum) {
		t.Fatalf("donate step must carry the ETH address:\n%s", m.view())
	}
	if !strings.Contains(m.view(), donate.Bitcoin) {
		t.Fatalf("donate step must carry the BTC address:\n%s", m.view())
	}
	// finish
	m = m.update(keyMsg("enter"))
	if m.inWizard {
		t.Fatal("wizard did not finish")
	}
}

// A done store opens straight to the main screen; '?' reopens the wizard.
func TestTUIDoneStoreSkipsWizardAndReopenKey(t *testing.T) {
	s := tuiStore(t)
	if err := s.SetWizardDone(); err != nil {
		t.Fatal(err)
	}
	m := NewModel(s)
	if m.inWizard {
		t.Fatal("done store must open on the main screen")
	}
	m = m.update(keyMsg("?"))
	if !m.inWizard || !strings.Contains(m.view(), "Welcome to Outpage") {
		t.Fatalf("'?' must reopen the wizard:\n%s", m.view())
	}
}

// Screens cycle with tab / shift+tab; every screen renders its key help.
func TestTUIScreenCycleAndHelp(t *testing.T) {
	s := tuiStore(t)
	s.SetWizardDone()
	m := NewModel(s)
	want := []string{"Components", "Incidents", "Subscribers", "Migrate", "Donate"}
	for _, w := range want {
		if !strings.Contains(m.view(), w) {
			t.Fatalf("screen %s missing from view:\n%s", w, m.view())
		}
		if !strings.Contains(m.view(), "tab") {
			t.Fatalf("key help missing on screen %s", w)
		}
		m = m.update(keyMsg("tab"))
	}
	// full cycle back to components (5 screens, 5 tabs)
	if !strings.Contains(m.view(), "Add: name") {
		t.Fatalf("tab did not cycle back:\n%s", m.view())
	}
	m = m.update(keyMsg("shift+tab"))
	if !strings.Contains(m.view(), "Donate") || strings.Contains(m.view(), "Add: name") {
		t.Fatalf("shift+tab must go backward:\n%s", m.view())
	}
}

// The donate screen shows both addresses byte-for-byte.
func TestTUIDonateScreen(t *testing.T) {
	s := tuiStore(t)
	s.SetWizardDone()
	m := NewModel(s)
	for i := 0; i < 4; i++ {
		m = m.update(keyMsg("tab"))
	}
	v := m.view()
	if !strings.Contains(v, donate.Ethereum) || !strings.Contains(v, donate.Bitcoin) {
		t.Fatalf("donate screen missing addresses:\n%s", v)
	}
}

// Components screen: input "name | description" + enter creates; the
// list shows it; duplicates surface in the status line.
func TestTUIComponentForm(t *testing.T) {
	s := tuiStore(t)
	s.SetWizardDone()
	m := NewModel(s)
	m = m.update(typeText("API | Public REST"))
	m = m.update(keyMsg("enter"))
	comps := s.ListComponents()
	if len(comps) != 1 || comps[0].Name != "API" || comps[0].Description != "Public REST" {
		t.Fatalf("store after form: %+v", comps)
	}
	if !strings.Contains(m.view(), "API") {
		t.Fatalf("list does not show the component:\n%s", m.view())
	}
	m = m.update(typeText("API"))
	m = m.update(keyMsg("enter"))
	if !strings.Contains(m.view(), "duplicate") {
		t.Fatalf("duplicate must surface in the status line:\n%s", m.view())
	}
}

// Incidents screen: "title | impact | components" opens; "id | status |
// body" posts an update; resolve works; illegal transition names the
// state.
func TestTUIIncidentLifecycle(t *testing.T) {
	s := tuiStore(t)
	s.SetWizardDone()
	m := NewModel(s)
	m = m.update(keyMsg("tab")) // incidents
	m = m.update(typeText("Elevated errors | minor | API"))
	m = m.update(keyMsg("enter"))
	// component API does not exist → store error surfaces; create it via
	// the components screen instead.
	if !strings.Contains(m.view(), "not found") {
		t.Fatalf("unknown component must surface:\n%s", m.view())
	}
	m = m.update(keyMsg("shift+tab")) // back to components
	m = m.update(typeText("API"))
	m = m.update(keyMsg("enter"))
	m = m.update(keyMsg("tab")) // incidents
	m = m.update(typeText("Elevated errors | minor | API"))
	m = m.update(keyMsg("enter"))
	incs, _ := s.ListIncidents(0, false)
	if len(incs) != 1 || incs[0].Status != "investigating" {
		t.Fatalf("incident open failed: %+v", incs)
	}
	// update: "1 | identified | Found it."
	m = m.update(typeText("1 | identified | Found the bad deploy."))
	m = m.update(keyMsg("enter"))
	ups, _ := s.IncidentUpdates(incs[0].ID)
	if len(ups) != 2 || ups[1].Status != "identified" {
		t.Fatalf("update failed: %+v", ups)
	}
	// resolve
	m = m.update(typeText("1 | resolved | Rollback complete."))
	m = m.update(keyMsg("enter"))
	got, _ := s.Incident(incs[0].ID)
	if got.Status != "resolved" {
		t.Fatalf("resolve failed: %+v", got)
	}
	// illegal transition from resolved: state named in status line
	m = m.update(typeText("1 | monitoring | zombie"))
	m = m.update(keyMsg("enter"))
	if !strings.Contains(m.view(), "resolved") {
		t.Fatalf("illegal transition must name the state:\n%s", m.view())
	}
}

// Subscribers screen: counts only (addresses never render); add via
// "email | address"; remove via "remove | id".
func TestTUISubscriberScreenMasksAddresses(t *testing.T) {
	s := tuiStore(t)
	s.SetWizardDone()
	m := NewModel(s)
	m = m.update(keyMsg("tab"))
	m = m.update(keyMsg("tab")) // subscribers
	m = m.update(typeText("email | ops@example.net"))
	m = m.update(keyMsg("enter"))
	e, w, _ := s.SubscriberCounts()
	if e != 1 || w != 0 {
		t.Fatalf("counts after add: email=%d webhook=%d", e, w)
	}
	v := m.view()
	if strings.Contains(v, "ops@example.net") {
		t.Fatalf("subscriber address must never render in the TUI:\n%s", v)
	}
	if !strings.Contains(v, "email=1") {
		t.Fatalf("count must render:\n%s", v)
	}
	// SMS refusal surfaces
	m = m.update(typeText("sms | +1555"))
	m = m.update(keyMsg("enter"))
	if !strings.Contains(strings.ToLower(m.view()), "sms") {
		t.Fatalf("SMS refusal must surface:\n%s", m.view())
	}
	// remove
	m = m.update(typeText("remove | 1"))
	m = m.update(keyMsg("enter"))
	if e, _, _ = s.SubscriberCounts(); e != 0 {
		t.Fatalf("remove failed: %d", e)
	}
}

// Migrate screen: the real fixture numbers, same as the CLI.
func TestTUIMigrateScreenRealNumbers(t *testing.T) {
	s := tuiStore(t)
	s.SetWizardDone()
	m := NewModel(s)
	for i := 0; i < 3; i++ {
		m = m.update(keyMsg("tab"))
	}
	v := m.view()
	for _, want := range []string{"mapped=7", "partial=3", "unmapped=2", "webhook.secrets"} {
		if !strings.Contains(v, want) {
			t.Fatalf("migrate screen missing %q:\n%s", want, v)
		}
	}
}

// Resize to 80x24 (and mid-wizard) does not crash; the view is a pure
// string function.
func TestTUIResize80x24NoCrash(t *testing.T) {
	s := tuiStore(t)
	m := NewModel(s)
	m.width, m.height = 80, 24
	_ = m.view()
	m = m.update(keyMsg("enter"))
	m.width, m.height = 80, 24
	if m.width != 80 || m.height != 24 {
		t.Fatal("resize not applied")
	}
	v := m.view()
	if len(v) == 0 {
		t.Fatal("empty view at 80x24")
	}
}

// Non-TTY launch prints a hint and exits — never hangs (tests, pipes,
// CI).
func TestTUILaunchNonTTY(t *testing.T) {
	orig := isTerminal
	isTerminal = func(f *osFile) bool { return false }
	defer func() { isTerminal = orig }()
	var out, errb strings.Builder
	code := launchTUI([]string{"--db", filepath.Join(t.TempDir(), "x.db")}, &out, &errb)
	if code != 0 {
		t.Fatalf("non-TTY tui exit = %d (err: %s)", code, errb.String())
	}
	if !strings.Contains(out.String(), "terminal") {
		t.Fatalf("hint missing: %q", out.String())
	}
}
