package store

import (
	"errors"
	"sync"
	"testing"

	"github.com/jonathanngiroux-star/outpage/internal/incident"
)

// Bug proof (whitespace): component names passed with stray spaces — e.g.
// a CLI `--components "API, Web"` split on commas — must resolve to the
// trimmed component, not a false not-found.
func TestIncidentComponentNamesTrimmed(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddComponent("Web", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenIncident("Spaces", "minor", []string{" Web", "Web "}); err != nil {
		t.Fatalf("OpenIncident with padded names: %v (want trimmed lookup)", err)
	}
	names, err := s.IncidentComponents(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "Web" {
		t.Fatalf("components = %v, want [Web]", names)
	}
}

// TOCTOU proof: N workers race one legal transition
// (investigating→identified). Exactly one may win; the rest must get
// ErrIllegalTransition (they see the post-commit status); the final state
// must be identified with exactly one new update.
func TestConcurrentAddUpdateSingleWinner(t *testing.T) {
	s := openTestStore(t)
	inc, err := s.OpenIncident("Race", "minor", nil)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	var wins, illegal int
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AddUpdate(inc.ID, incident.StatusIdentified, "racing update")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, incident.ErrIllegalTransition):
				illegal++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || illegal != workers-1 {
		t.Fatalf("wins=%d illegal=%d, want 1/%d", wins, illegal, workers-1)
	}
	got, err := s.Incident(inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != incident.StatusIdentified {
		t.Fatalf("final status = %q, want identified", got.Status)
	}
	ups, err := s.IncidentUpdates(inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 2 { // "Incident opened." + the single racing winner
		t.Fatalf("updates = %d, want 2", len(ups))
	}
}

// Concurrency proof: 50 goroutines adding distinct components must all
// land (50 rows, no corruption); 50 goroutines adding the SAME name must
// produce exactly one row — the UNIQUE constraint is the gate, so there
// is no check-then-insert window.
func TestConcurrentAddComponent(t *testing.T) {
	s := openTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.AddComponent(uniqueName(i), ""); err != nil {
				t.Errorf("distinct add %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if got := len(s.ListComponents()); got != 50 {
		t.Fatalf("components = %d, want 50", got)
	}

	s2 := openTestStore(t)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s2.AddComponent("same", "") // error expected for 49
		}()
	}
	wg.Wait()
	if got := len(s2.ListComponents()); got != 1 {
		t.Fatalf("duplicate race: components = %d, want 1", got)
	}
}

func uniqueName(i int) string {
	return "svc-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
}
