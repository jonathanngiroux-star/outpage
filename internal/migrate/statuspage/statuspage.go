// Package statuspage parses Atlassian Statuspage account exports and
// produces a dry-run migration report. Pure analysis — the report is the
// product; nothing is imported, nothing is mutated, nothing calls out.
// Every entity lands in exactly one bucket: mapped, partial (moves with
// named dropped fields), or unmapped (no Outpage equivalent).
package statuspage

import (
	"encoding/json"
	"fmt"
	"os"
)

// Export mirrors the Statuspage export shape (the documented JSON export
// from the admin UI). Unknown top-level fields are ignored on purpose —
// exports evolve; the report flags what it does not map.
type Export struct {
	Page         Page          `json:"page"`
	Components   []Component   `json:"components"`
	Incidents    []Incident    `json:"incidents"`
	Maintenances []Maintenance `json:"scheduled_maintenances"`
	Subscribers  []Subscriber  `json:"subscribers"`
	Metrics      []Metric      `json:"metrics_data"`
}

// Page is the account's single status page metadata.
type Page struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

// Component is one service on the page.
type Component struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Status             string            `json:"status"`
	OnlyShowIfDegraded bool              `json:"only_show_if_degraded"`
	CustomFields       map[string]string `json:"custom_fields"`
}

// Incident is one incident with its update history.
type Incident struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Impact     string   `json:"impact"`
	Components []string `json:"components"`
	CreatedAt  string   `json:"created_at"`
	ResolvedAt string   `json:"resolved_at"`
	Updates    []Update `json:"updates"`
}

// Maintenance is one scheduled maintenance window.
type Maintenance struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Impact     string   `json:"impact"`
	Components []string `json:"components"`
	StartsAt   string   `json:"starts_at"`
	EndsAt     string   `json:"ends_at"`
	Updates    []Update `json:"updates"`
}

// Update is one timeline entry.
type Update struct {
	Status    string `json:"status"`
	Body      string `json:"body"`
	HTMLBody  string `json:"html_body"`
	CreatedAt string `json:"created_at"`
}

// Subscriber is one notification target in the export.
type Subscriber struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Address string `json:"address"`
}

// Metric is uptime/latency history — never migrated (Outpage is a status
// page, not a monitor).
type Metric struct {
	MetricID string `json:"metric_id"`
	Name     string `json:"name"`
	Points   int    `json:"points"`
}

// Report is the dry-run output; the JSON field names are the machine
// contract CI pins.
type Report struct {
	Source   string       `json:"source"`
	DryRun   bool         `json:"dry_run"`
	Counts   ReportCounts `json:"counts"`
	Mappings []Mapping    `json:"mappings"`
	Risks    []Risk       `json:"risks"`
}

// ReportCounts tallies per result.
type ReportCounts struct {
	Mapped   int `json:"mapped"`
	Partial  int `json:"partial"`
	Unmapped int `json:"unmapped"`
}

// Mapping is one entity's classification.
type Mapping struct {
	Key      string   `json:"key"`
	SourceID string   `json:"source_id"`
	Name     string   `json:"name"`
	Result   string   `json:"result"` // mapped | partial | unmapped
	Dropped  []string `json:"dropped,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// Risk is an operator attention item that is not a per-entity gap.
type Risk struct {
	Key  string `json:"key"`
	Note string `json:"note"`
}

// Parse reads and unmarshals a Statuspage export. The error names the
// file so the CLI can surface a useful message.
func Parse(path string) (*Export, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var exp Export
	if err := json.Unmarshal(b, &exp); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &exp, nil
}

// MapComponentStatus converts a Statuspage component status to the
// Outpage five-word vocabulary. Unknown values return "" — the caller
// flags the component partial with `status:<original>` named.
func MapComponentStatus(s string) string {
	switch s {
	case "operational":
		return "operational"
	case "degraded_performance":
		return "degraded"
	case "partial_outage":
		return "partial"
	case "major_outage":
		return "major"
	case "under_maintenance":
		return "maintenance"
	}
	return ""
}

// Analyze classifies every entity in the export. Deterministic order:
// page, components, incidents, maintenances, subscribers, metrics.
func Analyze(exp *Export) *Report {
	rep := &Report{Source: "statuspage", DryRun: true}

	// Page metadata: name maps; identity fields have no equivalent.
	if exp.Page.Name != "" {
		m := Mapping{Key: "page", SourceID: exp.Page.ID, Name: exp.Page.Name, Result: "mapped"}
		var dropped []string
		if exp.Page.ID != "" {
			dropped = append(dropped, "page.id")
		}
		if exp.Page.URL != "" {
			dropped = append(dropped, "page.url")
		}
		if exp.Page.CreatedAt != "" {
			dropped = append(dropped, "page.created_at")
		}
		if len(dropped) > 0 {
			m.Result = "partial"
			m.Dropped = dropped
			m.Note = "page title imports; identity fields drop (custom domain is your reverse proxy)"
		}
		rep.Mappings = append(rep.Mappings, m)
	}

	// Components: status maps via the five-word bridge; extras drop.
	for _, c := range exp.Components {
		m := Mapping{Key: "component", SourceID: c.ID, Name: c.Name, Result: "mapped"}
		var dropped []string
		if MapComponentStatus(c.Status) == "" {
			dropped = append(dropped, "status:"+c.Status)
		}
		if c.OnlyShowIfDegraded {
			dropped = append(dropped, "only_show_if_degraded")
		}
		if len(c.CustomFields) > 0 {
			dropped = append(dropped, "custom_fields")
		}
		if len(dropped) > 0 {
			m.Result = "partial"
			m.Dropped = dropped
		}
		rep.Mappings = append(rep.Mappings, m)
	}

	// Incidents: status/impact/components/updates map; html_body drops.
	for _, i := range exp.Incidents {
		m := Mapping{Key: "incident", SourceID: i.ID, Name: i.Name, Result: "mapped"}
		for _, u := range i.Updates {
			if u.HTMLBody != "" {
				m.Result = "partial"
				m.Dropped = append(m.Dropped, "updates.html_body")
				break
			}
		}
		rep.Mappings = append(rep.Mappings, m)
	}

	// Maintenances: name/window/components map fully.
	for _, ma := range exp.Maintenances {
		m := Mapping{Key: "maintenance", SourceID: ma.ID, Name: ma.Name, Result: "mapped"}
		rep.Mappings = append(rep.Mappings, m)
	}

	// Subscribers: email/webhook import; SMS never.
	emailCount, webhookCount := 0, 0
	for _, sub := range exp.Subscribers {
		switch sub.Type {
		case "email":
			emailCount++
			rep.Mappings = append(rep.Mappings, Mapping{Key: "subscriber.email", SourceID: sub.ID, Name: sub.Address, Result: "mapped"})
		case "webhook":
			webhookCount++
			rep.Mappings = append(rep.Mappings, Mapping{Key: "subscriber.webhook", SourceID: sub.ID, Name: sub.Address, Result: "mapped"})
		default:
			rep.Mappings = append(rep.Mappings, Mapping{Key: "subscriber.sms", SourceID: sub.ID, Name: sub.Address, Result: "unmapped", Note: "no SMS gateway, by scope lock"})
		}
	}

	// Metrics: never migrated.
	for _, met := range exp.Metrics {
		rep.Mappings = append(rep.Mappings, Mapping{Key: "metrics.uptime", SourceID: met.MetricID, Name: met.Name, Result: "unmapped", Note: "Outpage is a status page, not a monitor"})
	}

	// Risks: reported when the input contains the trigger.
	if webhookCount > 0 {
		rep.Risks = append(rep.Risks, Risk{Key: "webhook.secrets", Note: "signing secrets do not exist in the export; re-enter per webhook subscriber after import"})
	}
	if emailCount > 0 {
		rep.Risks = append(rep.Risks, Risk{Key: "subscriber.reconfirm", Note: "re-confirm imported email addresses before first send (anti-spam hygiene)"})
	}

	rep.addCounts()
	return rep
}

// addCounts walks the mappings and fills Counts.
func (r *Report) addCounts() {
	r.Counts = ReportCounts{}
	for _, m := range r.Mappings {
		switch m.Result {
		case "mapped":
			r.Counts.Mapped++
		case "partial":
			r.Counts.Partial++
		case "unmapped":
			r.Counts.Unmapped++
		}
	}
}
