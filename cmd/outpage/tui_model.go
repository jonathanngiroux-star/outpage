package main

// tui_model.go: the Outpage terminal UI model — a plain struct over the
// SAME store the CLI and desktop GUI use, with the SAME shared wizard
// logic (wizard.go). No second data model, no second wizard. The model
// is renderer-agnostic: update() takes key messages, view() returns a
// string; the bubbletea adapter (tui_tea.go) is thin plumbing, and
// tests drive the model in-process.
//
// Keys (shown on every screen):
//
//	tab / shift+tab   next / previous screen
//	enter             submit the input line (or advance the wizard)
//	b                 wizard: back one step
//	s                 wizard: skip (optional steps only)
//	?                 reopen the setup wizard
//	q / ctrl+c        quit

import (
	"fmt"
	"strings"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// screen is one TUI view.
type screen int

const (
	screenComponents screen = iota
	screenIncidents
	screenSubscribers
	screenMigrate
	screenDonate
	screenCount
)

var screenTitles = map[screen]string{
	screenComponents:  "Components",
	screenIncidents:   "Incidents",
	screenSubscribers: "Subscribers",
	screenMigrate:     "Migrate",
	screenDonate:      "Donate",
}

// Model is the full TUI state.
type Model struct {
	st            *store.Store
	sc            screen
	input         string
	status        string // last action's result or error (the status line)
	wiz           *wizard
	inWizard      bool
	width, height int
}

// NewModel builds the model; a fresh store opens in the wizard.
func NewModel(st *store.Store) Model {
	m := Model{st: st, wiz: newWizard(st), width: 100, height: 40}
	if !st.WizardDone() {
		m.inWizard = true
	}
	return m
}

// keyMsg is the normalized key a renderer delivers (tests use it too).
func keyMsg(k string) string { return k }

// typeText appends typed runes to the input line.
func typeText(s string) string { return s }

// update advances the model with one key event (or typed text).
func (m Model) update(input string) Model {
	switch input {
	case "tab":
		if !m.inWizard {
			m.sc = (m.sc + 1) % screenCount
		}
		return m
	case "shift+tab":
		if !m.inWizard {
			m.sc = (m.sc - 1 + screenCount) % screenCount
		}
		return m
	case "?":
		if !m.inWizard {
			m.wiz.reset()
			m.inWizard = true
			m.status = ""
		}
		return m
	case "q", "ctrl+c":
		return m // the adapter handles quitting
	}

	if m.inWizard {
		m.updateWizard(input)
		return m
	}

	switch input {
	case "enter":
		m.submit()
	case "backspace":
		if len(m.input) > 0 {
			// cut one rune (ASCII fine for our commands)
			m.input = m.input[:len(m.input)-1]
		}
	default:
		m.input += input
	}
	return m
}

// updateWizard routes a key while the wizard is open. Field values are
// typed into the input line; enter applies the current step's fields,
// 'b' goes back, 's' skips optional steps.
func (m *Model) updateWizard(input string) {
	switch input {
	case "b":
		m.wiz.back()
		m.status = ""
		return
	case "s":
		if err := m.wiz.skip(); err != nil {
			m.status = err.Error()
		} else {
			m.status = ""
		}
		return
	case "enter":
		vals := map[string]string{}
		// Single-input convenience: the typed line fills the first empty
		// field of the step; subsequent enters submit remaining (empty)
		// fields using defaults.
		fields := m.wiz.current().Fields
		if len(fields) > 0 && m.input != "" {
			// Find the first field not yet filled for this step.
			for _, f := range fields {
				if strings.TrimSpace(m.wiz.values[f.Key]) == "" {
					vals[f.Key] = m.input
					break
				}
			}
		}
		// Prefill previously entered values so next() keeps them.
		for _, f := range fields {
			if v, ok := m.wiz.values[f.Key]; ok && vals[f.Key] == "" {
				vals[f.Key] = v
			}
		}
		if err := m.wiz.next(vals); err != nil {
			m.status = err.Error()
			return
		}
		m.input = ""
		m.status = ""
		if m.wiz.finished {
			m.inWizard = false
		}
		return
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return
	default:
		m.input += input
	}
}

// submit handles the main-screen input line per screen.
func (m *Model) submit() {
	line := strings.TrimSpace(m.input)
	m.input = ""
	if line == "" {
		return
	}
	switch m.sc {
	case screenComponents:
		m.submitComponents(line)
	case screenIncidents:
		m.submitIncidents(line)
	case screenSubscribers:
		m.submitSubscribers(line)
	case screenMigrate:
		m.status = "migrate is dry-run only — run 'outpage migrate --from=statuspage --dry-run' for the full report"
	case screenDonate:
		m.status = "donations are on-chain — copy the address that fits your wallet"
	}
}

// splitPipe splits "a | b | c" into parts (trimmed).
func splitPipe(line string) []string {
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func (m *Model) submitComponents(line string) {
	parts := splitPipe(line)
	name := parts[0]
	desc := ""
	if len(parts) > 1 {
		desc = parts[1]
	}
	if _, err := m.st.AddComponent(name, desc); err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf("component %q added", name)
}

func (m *Model) submitIncidents(line string) {
	parts := splitPipe(line)
	if len(parts) < 2 {
		m.status = "use: 'title | impact | components' to open, 'id | status | body' to update"
		return
	}
	// Update form: first part is a number.
	if id, err := parseID(parts[0]); err == nil {
		if len(parts) < 3 {
			m.status = "use: 'id | status | body'"
			return
		}
		if _, err := m.st.AddUpdate(id, parts[1], parts[2]); err != nil {
			if inc, gerr := m.st.Incident(id); gerr == nil {
				m.status = fmt.Sprintf("%v (incident %d is %q)", err, id, inc.Status)
			} else {
				m.status = err.Error()
			}
			return
		}
		m.status = fmt.Sprintf("incident %d → %s", id, parts[1])
		return
	}
	// Open form: title | impact | components
	title := parts[0]
	impact := "minor"
	if len(parts) > 1 && parts[1] != "" {
		impact = parts[1]
	}
	var comps []string
	if len(parts) > 2 && parts[2] != "" {
		comps = strings.Split(parts[2], ",")
	}
	inc, err := m.st.OpenIncident(title, impact, comps)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf("incident %d opened (%s)", inc.ID, inc.Status)
}

func (m *Model) submitSubscribers(line string) {
	parts := splitPipe(line)
	if parts[0] == "remove" {
		if len(parts) < 2 {
			m.status = "use: 'remove | id'"
			return
		}
		id, err := parseID(parts[1])
		if err != nil {
			m.status = "id must be a number"
			return
		}
		if err := m.st.DeleteSubscriber(id); err != nil {
			m.status = err.Error()
			return
		}
		m.status = fmt.Sprintf("subscriber %d removed", id)
		return
	}
	if len(parts) < 2 {
		m.status = "use: 'email | address', 'webhook | url | secret', or 'remove | id'"
		return
	}
	kind := parts[0]
	address := parts[1]
	secret := ""
	if len(parts) > 2 {
		secret = parts[2]
	}
	if _, err := m.st.AddSubscriber(kind, address, secret); err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf("%s subscriber added", kind)
}

// view renders the current screen (pure string).
func (m Model) view() string {
	if m.inWizard {
		return m.viewWizard()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Outpage — %s%s\n\n", screenTitles[m.sc], m.repeatTail())
	switch m.sc {
	case screenComponents:
		m.renderComponents(&b)
	case screenIncidents:
		m.renderIncidents(&b)
	case screenSubscribers:
		m.renderSubscribers(&b)
	case screenMigrate:
		m.renderMigrate(&b)
	case screenDonate:
		m.renderDonate(&b)
	}
	if m.status != "" {
		fmt.Fprintf(&b, "\n  %s\n", m.status)
	}
	fmt.Fprintf(&b, "\n  [tab/shift+tab] screens  [enter] submit  [?] wizard  [q] quit\n")
	return b.String()
}

// repeatTail pads the title line for stable rendering in tests.
func (m Model) repeatTail() string { return "" }

func (m Model) renderComponents(b *strings.Builder) {
	comps := m.st.ListComponents()
	fmt.Fprintf(b, "  Add: name | description\n")
	fmt.Fprintf(b, "  %s\n", strings.Repeat("─", 60))
	if len(comps) == 0 {
		fmt.Fprintf(b, "  (no components yet)\n")
		return
	}
	for _, c := range comps {
		desc := ""
		if c.Description != "" {
			desc = " — " + c.Description
		}
		fmt.Fprintf(b, "  %-24s %-14s%s\n", c.Name, c.Status, desc)
	}
}

func (m Model) renderIncidents(b *strings.Builder) {
	incs, _ := m.st.ListIncidents(0, false)
	fmt.Fprintf(b, "  Open: title | impact | components     Update: id | status | body\n")
	fmt.Fprintf(b, "  %s\n", strings.Repeat("─", 60))
	if len(incs) == 0 {
		fmt.Fprintf(b, "  (no incidents yet)\n")
		return
	}
	for _, inc := range incs {
		fmt.Fprintf(b, "  #%d %-10s %-12s %-12s %s\n", inc.ID, inc.Kind, inc.Status, inc.Impact, inc.Name)
	}
}

func (m Model) renderSubscribers(b *strings.Builder) {
	e, w, _ := m.st.SubscriberCounts()
	fmt.Fprintf(b, "  Add: email | address     webhook | url | secret     remove | id\n")
	fmt.Fprintf(b, "  %s\n", strings.Repeat("─", 60))
	fmt.Fprintf(b, "  email=%d webhook=%d (addresses are never rendered here)\n", e, w)
}

func (m Model) renderMigrate(b *strings.Builder) {
	rep := sampleMigrateReport()
	fmt.Fprintf(b, "  Statuspage dry-run (sample fixture):\n")
	fmt.Fprintf(b, "  %s\n", strings.Repeat("─", 60))
	if rep == nil {
		fmt.Fprintf(b, "  (fixture unavailable)\n")
	} else {
		fmt.Fprintf(b, "  mapped=%d partial=%d unmapped=%d\n",
			rep.Counts.Mapped, rep.Counts.Partial, rep.Counts.Unmapped)
		for _, r := range rep.Risks {
			fmt.Fprintf(b, "  risk %s — %s\n", r.Key, r.Note)
		}
	}
	fmt.Fprintf(b, "  Full report: outpage migrate --from=statuspage --dry-run --format=json\n")
}

func (m Model) renderDonate(b *strings.Builder) {
	fmt.Fprintf(b, "  Outpage is free, MIT-licensed software.\n")
	fmt.Fprintf(b, "  Donations are the only funding — never a paywall, never counted as traction.\n\n")
	fmt.Fprintf(b, "  Ethereum / USDC (ERC-20):\n  %s\n\n", donate.Ethereum)
	fmt.Fprintf(b, "  Bitcoin:\n  %s\n", donate.Bitcoin)
}

// viewWizard renders the open wizard.
func (m Model) viewWizard() string {
	v := m.wiz.view()
	var b strings.Builder
	fmt.Fprintf(&b, "Outpage setup wizard — %s\n\n", v.Step.Title)
	fmt.Fprintf(&b, "  %s\n\n", strings.ReplaceAll(v.Step.Body, "\n", "\n  "))
	for _, f := range v.Step.Fields {
		prefix := "  "
		if val, ok := v.Values[f.Key]; ok && strings.TrimSpace(val) != "" {
			fmt.Fprintf(&b, "%s%s: %s\n", prefix, f.Label, val)
		} else {
			fmt.Fprintf(&b, "%s%s (e.g. %s)\n", prefix, f.Label, f.Placeholder)
		}
	}
	if v.Step.ID == "migrate" && v.Migrate != "" {
		fmt.Fprintf(&b, "\n  %s\n", v.Migrate)
	}
	if m.status != "" {
		fmt.Fprintf(&b, "\n  ! %s\n", m.status)
	}
	fmt.Fprintf(&b, "\n  [enter] %s   [b] back   ", v.Step.NextLabel)
	if v.Step.Optional {
		fmt.Fprintf(&b, "[s] skip   ")
	}
	fmt.Fprintf(&b, "[q] quit\n")
	return b.String()
}

// parseID parses a small positive integer (command ids).
func parseID(s string) (int64, error) {
	var n int64
	if s == "" {
		return 0, fmt.Errorf("empty id")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int64(r-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("id must be positive")
	}
	return n, nil
}
