package main

// wizard.go: the guided setup wizard shared by the desktop GUI and (in
// step 5) the TUI. Every step runs the real operation on the user's
// values through the same store methods the CLI uses — no slideshow. The
// logic is a plain struct so tests drive it in-process; renderers only
// draw fields and results.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/migrate/statuspage"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// wizardStep is one guided page. Optional steps may be skipped; required
// steps refuse the skip so the core workflow always happens once.
type wizardStep struct {
	ID        string
	Title     string
	Body      string
	Fields    []wizardField
	NextLabel string
	Optional  bool
}

// wizardField is one input the renderer draws.
type wizardField struct {
	Key         string
	Label       string
	Placeholder string
}

// wizard is the state machine. One instance per app; reopen resets it.
type wizard struct {
	st       *store.Store
	step     int
	values   map[string]string
	finished bool
}

// newWizard starts at the welcome step.
func newWizard(st *store.Store) *wizard {
	return &wizard{st: st, values: map[string]string{}}
}

// wizardSteps returns the fixed sequence. Order is the wedge: name the
// page → create a component → open the first incident (the core loop) →
// optional subscriber → optional migrate preview → donate + finish.
func wizardSteps() []wizardStep {
	return []wizardStep{
		{
			ID:    "welcome",
			Title: "Welcome to Outpage",
			Body: "Outpage is a free, self-hosted public status page: one binary, one SQLite file. " +
				"This wizard walks the core workflow once with real data — everything it creates is the real thing, " +
				"and every action is also a CLI command.\n\n" +
				"You can reopen this wizard any time from Help.",
			NextLabel: "Next",
		},
		{
			ID:    "page",
			Title: "Step 1 — Name your status page",
			Body:  "The page title shown at the top of the public page and in incident emails.",
			Fields: []wizardField{
				{Key: "page_name", Label: "Page name", Placeholder: "Example Cloud"},
			},
			NextLabel: "Next",
		},
		{
			ID:    "component",
			Title: "Step 2 — Create your first component",
			Body: "Components are the services whose status the page tracks (API, Web App, Database…). " +
				"You can add more any time from the Components view or the CLI.",
			Fields: []wizardField{
				{Key: "component_name", Label: "Name", Placeholder: "API"},
				{Key: "component_description", Label: "Description (optional)", Placeholder: "Public REST API"},
			},
			NextLabel: "Next",
		},
		{
			ID:    "incident",
			Title: "Step 3 — Open a sample incident",
			Body: "The core loop: open an incident on the component, and the public page updates immediately. " +
				"Resolve it when you're comfortable — the timeline stays in history.",
			Fields: []wizardField{
				{Key: "incident_name", Label: "Title", Placeholder: "Elevated API error rates"},
				{Key: "incident_impact", Label: "Impact (none/minor/major/critical)", Placeholder: "minor"},
			},
			NextLabel: "Next",
		},
		{
			ID:    "subscriber",
			Title: "Step 4 — Add an email subscriber (optional)",
			Body: "Notifications go through your own SMTP relay (set OUTPAGE_SMTP_HOST/PORT/From to enable sending). " +
				"Without a relay configured, sends are skipped and recorded — nothing is lost.",
			Fields: []wizardField{
				{Key: "subscriber_email", Label: "Email address", Placeholder: "ops@example.net"},
			},
			NextLabel: "Next",
			Optional:  true,
		},
		{
			ID:    "migrate",
			Title: "Step 5 — Coming from Statuspage? (optional)",
			Body: "Outpage reads an Atlassian Statuspage export and reports exactly what moves, what moves partially, " +
				"and what has no equivalent — nothing is mutated. This preview runs on the sample fixture; " +
				"point the Migrate view at your real export any time.",
			NextLabel: "Next",
			Optional:  true,
		},
		{
			ID:    "donate",
			Title: "Done — your page is live",
			Body: "Outpage is free, forever: no paid tier, no subscription, no gated features. " +
				"If it kills your weekly status chore, donations are the only ask:\n\n" +
				"Ethereum / USDC (ERC-20): " + donate.Ethereum + "\n" +
				"Bitcoin: " + donate.Bitcoin + "\n\n" +
				"Finish to open the main view with what you just created.",
			NextLabel: "Finish",
		},
	}
}

// current returns the active step definition.
func (w *wizard) current() wizardStep {
	steps := wizardSteps()
	if w.step >= len(steps) {
		return steps[len(steps)-1]
	}
	return steps[w.step]
}

// view renders the step plus current values (for prefilling fields).
type wizardView struct {
	Step    wizardStep
	Values  map[string]string
	Migrate string // populated on the migrate step: the real dry-run summary
}

func (w *wizard) view() wizardView {
	v := wizardView{Step: w.current(), Values: w.values}
	if v.Step.ID == "migrate" {
		v.Migrate = migratePreview()
	}
	return v
}

// migratePreviewCached memoizes the fixture report per process: the TUI
// renders on every keypress and the wizard re-views the migrate step;
// a file read + JSON parse per frame is a stutter bug.
var (
	migrateParseCalls int32
	migrateOnce       sync.Once
	migrateCache      string
)

// migratePreview returns the one-line dry-run summary (cached; parsed
// once per process — the TUI renders on every keypress).
func migratePreview() string {
	migrateOnce.Do(func() {
		atomic.AddInt32(&migrateParseCalls, 1)
		root := repoRootString()
		exp, err := statuspage.Parse(root + "/testdata/fixtures/statuspage/sample-export.json")
		if err != nil {
			migrateCache = "migrate preview unavailable: " + err.Error()
			return
		}
		rep := statuspage.Analyze(exp)
		migrateCache = fmt.Sprintf("Sample Statuspage export: mapped=%d partial=%d unmapped=%d — see the Migrate view for the full report.",
			rep.Counts.Mapped, rep.Counts.Partial, rep.Counts.Unmapped)
	})
	return migrateCache
}

// next validates and applies the step, then advances. Errors name the
// field; the wizard stays put.
func (w *wizard) next(vals map[string]string) error {
	step := w.current()
	for k, v := range vals {
		w.values[k] = v
	}
	switch step.ID {
	case "page":
		name := strings.TrimSpace(vals["page_name"])
		if name == "" {
			return fmt.Errorf("page_name: a page name is required")
		}
		if err := w.st.SetPageName(name); err != nil {
			return fmt.Errorf("page_name: %w", err)
		}
	case "component":
		name := strings.TrimSpace(vals["component_name"])
		if name == "" {
			return fmt.Errorf("component_name: a component name is required")
		}
		if _, err := w.st.AddComponent(name, strings.TrimSpace(vals["component_description"])); err != nil {
			// Going Back then re-submitting the same value is the edit
			// path, not an error: if the component already exists with
			// this name, treat the step as satisfied. A genuinely new
			// duplicate (different description) still surfaces.
			if errors.Is(err, store.ErrDuplicate) {
				existing := false
				for _, c := range w.st.ListComponents() {
					if strings.EqualFold(c.Name, name) {
						existing = true
						break
					}
				}
				if existing {
					break // step satisfied; advance
				}
			}
			return fmt.Errorf("component_name: %w", err)
		}
	case "incident":
		name := strings.TrimSpace(vals["incident_name"])
		if name == "" {
			return fmt.Errorf("incident_name: an incident title is required")
		}
		impact := strings.TrimSpace(vals["incident_impact"])
		if impact == "" {
			impact = "minor"
		}
		// Link to every component that exists — the wizard's sample
		// incident touches what the user just created.
		var comps []string
		for _, c := range w.st.ListComponents() {
			comps = append(comps, c.Name)
		}
		if _, err := w.st.OpenIncident(name, impact, comps); err != nil {
			return fmt.Errorf("incident_name: %w", err)
		}
	case "subscriber":
		email := strings.TrimSpace(vals["subscriber_email"])
		if email == "" {
			return nil // optional: empty field on an optional step = skip
		}
		if _, err := w.st.AddSubscriber("email", email, ""); err != nil {
			return fmt.Errorf("subscriber_email: %w", err)
		}
	}
	w.step++
	if w.step >= len(wizardSteps()) {
		w.finished = true
		if err := w.st.SetWizardDone(); err != nil {
			return err
		}
	}
	return nil
}

// skip advances an optional step without applying it.
func (w *wizard) skip() error {
	if !w.current().Optional {
		return fmt.Errorf("step %q is required", w.current().ID)
	}
	w.step++
	return nil
}

// back returns to the previous step (values kept for editing).
func (w *wizard) back() {
	if w.step > 0 {
		w.step--
	}
}

// reset reopens the wizard from the welcome step (Help → Open wizard).
func (w *wizard) reset() {
	w.step = 0
	w.finished = false
}
