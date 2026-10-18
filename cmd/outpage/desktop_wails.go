//go:build desktop

package main

// desktop_wails.go: the Wails v2 desktop GUI, only compiled with
// `-tags desktop,production,webkit2_41` (+ CGO on Linux). Every bound
// method is a thin wrapper over the same store methods the CLI and (in
// step 5) the TUI use — no second data model. The frontend is a single
// vanilla HTML file embedded in the binary: no npm, no bundler.

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/migrate/statuspage"
	"github.com/jonathanngiroux-star/outpage/internal/page"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

//go:embed all:frontend
var frontendEmbed embed.FS

// desktopApp is the struct bound to the frontend.
type desktopApp struct {
	ctx context.Context
	st  *store.Store
	wiz *wizard
}

// NewDesktopApp builds the binding layer over an open store. Exported
// for tests and future embedders; the Wails runtime binds it by
// instance.
func NewDesktopApp(st *store.Store) *desktopApp {
	return &desktopApp{st: st, wiz: newWizard(st)}
}

// ---- types returned to the frontend -------------------------------

// ComponentView is a component row for the editor list.
type ComponentView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// IncidentView is one incident row (both kinds).
type IncidentView struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Impact string `json:"impact"`
}

// UpdateView is one timeline entry.
type UpdateView struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Body   string `json:"body"`
}

// SubscriberView is a masked subscriber row — the list never carries raw
// addresses or secrets (they exist in the store; the list view hides
// them).
type SubscriberView struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	Display string `json:"display"`
}

// DonateInfo carries both addresses for the donate view.
type DonateInfo struct {
	Ethereum string `json:"ethereum"`
	Bitcoin  string `json:"bitcoin"`
	Note     string `json:"note"`
}

// WizardStepView is the JSON shape of one wizard page for the frontend.
type WizardStepView struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Fields    []wizardFieldJSON `json:"fields"`
	NextLabel string            `json:"next_label"`
	Optional  bool              `json:"optional"`
	Migrate   string            `json:"migrate,omitempty"`
}

type wizardFieldJSON struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
}

// WizardStateView is the wizard's full state for the frontend.
type WizardStateView struct {
	Step     WizardStepView    `json:"step"`
	Values   map[string]string `json:"values"`
	Finished bool              `json:"finished"`
}

// ---- components ---------------------------------------------------

func (a *desktopApp) CreateComponent(name, description string) error {
	_, err := a.st.AddComponent(name, description)
	return err
}

func (a *desktopApp) Components() []ComponentView {
	var out []ComponentView
	for _, c := range a.st.ListComponents() {
		out = append(out, ComponentView{ID: c.ID, Name: c.Name, Description: c.Description, Status: c.Status})
	}
	return out
}

func (a *desktopApp) SetComponentStatus(name, status string) error {
	return a.st.SetComponentStatus(name, status)
}

// ---- incidents ------------------------------------------------------

func (a *desktopApp) OpenIncident(name, impact string, components []string) (IncidentView, error) {
	inc, err := a.st.OpenIncident(name, impact, components)
	if err != nil {
		return IncidentView{}, err
	}
	return IncidentView{ID: inc.ID, Kind: inc.Kind, Name: inc.Name, Status: inc.Status, Impact: inc.Impact}, nil
}

func (a *desktopApp) UpdateIncident(id int64, status, body string) error {
	if _, err := a.st.AddUpdate(id, status, body); err != nil {
		// Name the current state so the UI can show why it refused
		// (same contract as the CLI).
		if inc, gerr := a.st.Incident(id); gerr == nil {
			return fmt.Errorf("%w (incident %d is currently %q)", err, id, inc.Status)
		}
		return err
	}
	return nil
}

func (a *desktopApp) Incidents() []IncidentView {
	var out []IncidentView
	incs, _ := a.st.ListIncidents(0, false)
	for _, inc := range incs {
		out = append(out, IncidentView{ID: inc.ID, Kind: inc.Kind, Name: inc.Name, Status: inc.Status, Impact: inc.Impact})
	}
	return out
}

func (a *desktopApp) IncidentUpdates(id int64) []UpdateView {
	var out []UpdateView
	ups, _ := a.st.IncidentUpdates(id)
	for _, u := range ups {
		out = append(out, UpdateView{ID: u.ID, Status: u.Status, Body: u.Body})
	}
	return out
}

func (a *desktopApp) IncidentComponentNames(id int64) []string {
	names, _ := a.st.IncidentComponents(id)
	return names
}

// ---- maintenance ----------------------------------------------------

func (a *desktopApp) AddMaintenance(name string, components []string, start, end string) error {
	startT, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return err
	}
	endT, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return err
	}
	_, err = a.st.OpenMaintenance(name, components, startT, endT)
	return err
}

// ---- subscribers ---------------------------------------------------

// maskEmail renders an address as "o***@e***.net" style for list views.
func maskEmail(addr string) string {
	at := indexByte(addr, '@')
	if at <= 0 || at+1 >= len(addr) {
		return "***"
	}
	user, domain := addr[:at], addr[at+1:]
	return string(user[0]) + "***@" + string(domain[0]) + "***"
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// maskURL renders a webhook URL's host only.
func maskURL(u string) string {
	// https://hooks.example.net/x -> hooks.example.net/***
	for i := 0; i+1 < len(u); i++ {
		if u[i] == '/' && u[i+1] == '/' {
			rest := u[i+2:]
			for j := 0; j < len(rest); j++ {
				if rest[j] == '/' {
					return rest[:j] + "/***"
				}
			}
			return rest
		}
	}
	return "***"
}

func (a *desktopApp) AddSubscriber(kind, address, secret string) error {
	_, err := a.st.AddSubscriber(kind, address, secret)
	return err
}

func (a *desktopApp) Subscribers() []SubscriberView {
	var out []SubscriberView
	subs, _ := a.st.ListSubscribers()
	for _, s := range subs {
		disp := "***"
		if s.Kind == "email" {
			disp = maskEmail(s.Address)
		} else if s.Kind == "webhook" {
			disp = maskURL(s.Address)
		}
		out = append(out, SubscriberView{ID: s.ID, Kind: s.Kind, Display: disp})
	}
	return out
}

func (a *desktopApp) SubscriberCounts() (int, int) {
	e, w, _ := a.st.SubscriberCounts()
	return e, w
}

func (a *desktopApp) RemoveSubscriber(id int64) error {
	return a.st.DeleteSubscriber(id)
}

// ---- migrate ----------------------------------------------------------

func (a *desktopApp) MigrateDryRun(input string) (*statuspage.Report, error) {
	path := input
	if path == "" {
		path = repoRootString() + "/testdata/fixtures/statuspage/sample-export.json"
	}
	exp, err := statuspage.Parse(path)
	if err != nil {
		return nil, err
	}
	return statuspage.Analyze(exp), nil
}

// ---- donate ------------------------------------------------------------

func (a *desktopApp) DonateAddresses() DonateInfo {
	return DonateInfo{
		Ethereum: donate.Ethereum,
		Bitcoin:  donate.Bitcoin,
		Note:     "Donations are the only funding — no feature is gated, ever.",
	}
}

// ---- public page --------------------------------------------------------

func (a *desktopApp) PublicPagePreview() (string, error) {
	return page.Render(a.st)
}

// ---- wizard ------------------------------------------------------------

func toStepView(v wizardView) WizardStateView {
	sv := WizardStateView{
		Step: WizardStepView{
			ID: v.Step.ID, Title: v.Step.Title, Body: v.Step.Body,
			NextLabel: v.Step.NextLabel, Optional: v.Step.Optional,
			Migrate: v.Migrate,
		},
		Values:   v.Values,
		Finished: false,
	}
	for _, f := range v.Step.Fields {
		sv.Step.Fields = append(sv.Step.Fields, wizardFieldJSON{Key: f.Key, Label: f.Label, Placeholder: f.Placeholder})
	}
	return sv
}

// WizardView returns the current step (first launch opens at welcome).
func (a *desktopApp) WizardView() WizardStateView {
	return toStepView(a.wiz.view())
}

// WizardNext applies the step's fields and advances. On error the state
// stays put; the error string reaches the frontend.
func (a *desktopApp) WizardNext(vals map[string]string) (WizardStateView, error) {
	if err := a.wiz.next(vals); err != nil {
		return toStepView(a.wiz.view()), err
	}
	return a.wizardState(), nil
}

// WizardSkip skips an optional step.
func (a *desktopApp) WizardSkip() (WizardStateView, error) {
	if err := a.wiz.skip(); err != nil {
		return toStepView(a.wiz.view()), err
	}
	return a.wizardState(), nil
}

// WizardBack returns one step.
func (a *desktopApp) WizardBack() (WizardStateView, error) {
	a.wiz.back()
	return a.wizardState(), nil
}

// WizardReopen resets to welcome (Help → Open wizard).
func (a *desktopApp) WizardReopen() WizardStateView {
	a.wiz.reset()
	return toStepView(a.wiz.view())
}

// WizardDone reports whether the first-run wizard has been completed —
// the frontend hides the overlay on done stores and shows it on fresh ones.
func (a *desktopApp) WizardDone() bool {
	return a.st.WizardDone()
}

// wizardState snapshots the wizard including finished.
func (a *desktopApp) wizardState() WizardStateView {
	v := toStepView(a.wiz.view())
	v.Finished = a.wiz.finished
	return v
}

// ---- app lifecycle ----------------------------------------------------

// startup captures the runtime context.
func (a *desktopApp) startup(ctx context.Context) {
	a.ctx = ctx
}

// runDesktopWails boots the Wails window over the open store.
func runDesktopWails(st *store.Store, stdout, stderr io.Writer) int {
	app := NewDesktopApp(st)
	// First launch: the wizard runs before the main view (frontend shows
	// the wizard modal until st.WizardDone()).
	err := wails.Run(&options.App{
		Title:     "Outpage",
		Width:     1100,
		Height:    720,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets(),
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
	})
	if err != nil {
		fmt.Fprintf(stderr, "outpage desktop: %v\n", err)
		return 1
	}
	return 0
}

// frontendAssets serves the embedded frontend at /.
func frontendAssets() fs.FS {
	sub, err := fs.Sub(frontendEmbed, "frontend")
	if err != nil {
		panic("desktop: embedded frontend missing: " + err.Error())
	}
	return sub
}
