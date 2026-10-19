package main

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func auditStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// 'q' on a main screen: typed when the input line has text (you can name
// a component "Queue"), quit when the line is empty. The model must treat
// 'q' as a plain character; quitting is the adapter's call.
func TestTUIQTypesWhenInputNonEmpty(t *testing.T) {
	s := auditStore(t)
	s.SetWizardDone()
	m := NewModel(s)
	m = m.update(typeText("Que"))
	m = m.update(keyMsg("q"))
	if m.input != "Queq" {
		t.Fatalf("'q' must type when input is non-empty, got %q", m.input)
	}
	m = m.update(keyMsg("u"))
	m = m.update(keyMsg("e"))
	m = m.update(keyMsg("enter"))
	comps := s.ListComponents()
	if len(comps) != 1 || comps[0].Name != "Queque" {
		t.Fatalf("typed name with q wrong: %+v", comps)
	}
}

// The quit decision is the adapter's, on the empty line only.
func TestQuitOnKeyDecision(t *testing.T) {
	cases := []struct {
		input string
		key   string
		want  bool
	}{
		{"", "q", true},
		{"", "ctrl+c", true},
		{"Que", "q", false},
		{"anything", "ctrl+c", true}, // ctrl+c always quits
		{"", "x", false},
	}
	for _, c := range cases {
		if got := quitOnKey(c.input, c.key); got != c.want {
			t.Errorf("quitOnKey(%q, %q) = %v, want %v", c.input, c.key, got, c.want)
		}
	}
}

// Wizard typing: 's' and 'b' are back/skip only on an EMPTY input line;
// with text being typed they are characters (page name "Status" must be
// typeable).
func TestTUIWizardSandBAreTypedWhenInputNonEmpty(t *testing.T) {
	s := auditStore(t)
	m := NewModel(s)
	m = m.update(keyMsg("enter")) // welcome → page step
	// Type "Status" — the final 's' has non-empty input and must type.
	m = m.update(typeText("Statu"))
	m = m.update(keyMsg("s"))
	if m.wiz.current().ID != "page" {
		t.Fatalf("'s' with non-empty input must not skip (landed on %q)", m.wiz.current().ID)
	}
	m = m.update(keyMsg("enter"))
	if got := s.PageName(); got != "Status" {
		t.Fatalf("page name = %q, want Status", got)
	}
	// On the component step, 'b' with non-empty input types (name "Bob").
	m = m.update(typeText("Bo"))
	m = m.update(keyMsg("b"))
	if m.wiz.current().ID != "component" {
		t.Fatalf("'b' with non-empty input must not go back (landed on %q)", m.wiz.current().ID)
	}
	m = m.update(keyMsg("enter"))
	comps := s.ListComponents()
	if len(comps) != 1 || comps[0].Name != "Bob" {
		t.Fatalf("component = %+v, want Bob", comps)
	}
	// With an EMPTY line, 's' skips the optional subscriber step.
	m = m.update(typeText("Inc"))
	m = m.update(keyMsg("enter")) // incident step
	m = m.update(keyMsg("enter")) // → subscriber (optional)
	m = m.update(keyMsg("s"))     // empty line → skip
	if m.wiz.current().ID != "migrate" {
		t.Fatalf("empty-line 's' must skip, landed on %q", m.wiz.current().ID)
	}
}

// The fixture is parsed once per process, not once per render (the TUI
// renders on every keypress; a file read + JSON parse per frame is a
// stutter bug).
func TestMigratePreviewParsedOncePerProcess(t *testing.T) {
	atomic.StoreInt32(&migrateParseCalls, 0)
	s := auditStore(t)
	m := NewModel(s)
	s.SetWizardDone()
	// Render the migrate screen repeatedly.
	for i := 0; i < 5; i++ {
		m = m.update(keyMsg("tab"))
	}
	for i := 0; i < 5; i++ {
		_ = m.view()
	}
	// The wizard's migrate step view also goes through the cache.
	w := newWizard(s)
	w.step = 5 // migrate step
	for i := 0; i < 5; i++ {
		_ = w.view()
	}
	if got := atomic.LoadInt32(&migrateParseCalls); got > 1 {
		t.Fatalf("fixture parsed %d times, want ≤1 (cache)", got)
	}
}

// The wizard skip error names the step (required-step refusal is
// understandable without the README).
func TestTUIWizardSkipErrorNamesStep(t *testing.T) {
	m := NewModel(auditStore(t))
	m = m.update(keyMsg("enter")) // → page (required)
	m = m.update(keyMsg("s"))
	if !strings.Contains(m.view(), "page") || !strings.Contains(m.view(), "required") {
		t.Fatalf("skip refusal must name the step:\n%s", m.view())
	}
}
