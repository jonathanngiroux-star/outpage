// Package page renders the public status page from the store. The page is
// server-rendered HTML with zero JavaScript — the status must be readable
// from the HTML source alone (data-status attributes carry the machine
// view). Operator-supplied text is escaped; subscriber addresses never
// render.
package page

import (
	"bytes"
	"html/template"
	"strings"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// incidentView is one incident or maintenance rendered on the page.
type incidentView struct {
	Name       string
	Impact     string
	Status     string
	StatusText string
	Kind       string
	Starts     string
	Ends       string
	Updates    []store.Update
	Components []string
}

type pageData struct {
	Name         string
	Banner       string
	BannerClass  string
	Components   []store.Component
	Active       []incidentView
	Maintenances []incidentView
	History      []incidentView
	HasHistory   bool
	Footer       template.HTML
}

var funcs = template.FuncMap{
	"title":    title,
	"fmtTime":  fmtTime,
	"join":     strings.Join,
	"statText": statusText,
}

var pageTmpl = template.Must(template.New("page").Funcs(funcs).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Name}} — status</title>
<style>
:root { --ok:#2f9e44; --warn:#e8b109; --bad:#d64545; --muted:#667; }
body { font-family: system-ui, sans-serif; margin: 0; color: #1c1f24; background: #fafafa; }
main { max-width: 46rem; margin: 0 auto; padding: 2rem 1rem 4rem; }
h1 { font-size: 1.6rem; }
.banner { padding: .9rem 1.1rem; border-radius: 8px; font-weight: 600; margin: 1rem 0 2rem; }
.banner.ok { background: #e5f6e9; color: var(--ok); }
.banner.warn { background: #fdf4dd; color: #a07c00; }
.banner.bad { background: #fce4e4; color: var(--bad); }
.component { display: flex; justify-content: space-between; padding: .7rem 0; border-bottom: 1px solid #e6e6e6; }
.component .status { font-weight: 600; }
.status.operational { color: var(--ok); }
.status.degraded, .status.maintenance { color: var(--warn); }
.status.partial, .status.major { color: var(--bad); }
.incident { border: 1px solid #e2e2e2; border-radius: 8px; padding: 1rem 1.2rem; margin: 1rem 0; background: #fff; }
.incident h3 { margin: 0 0 .2rem; font-size: 1.05rem; }
.impact { display: inline-block; font-size: .72rem; padding: .1rem .5rem; border-radius: 999px; background: #eef1f4; color: #46586b; }
.timeline { list-style: none; padding: 0; margin: .6rem 0 0; }
.timeline li { padding: .5rem 0; border-top: 1px dashed #e8e8e8; }
.timeline .u-status { font-weight: 600; }
.timeline time { color: var(--muted); font-size: .8rem; margin-left: .4rem; }
section { margin-top: 2.4rem; }
h2 { font-size: 1.15rem; }
.empty { color: var(--muted); }
footer { margin-top: 3rem; border-top: 1px solid #e2e2e2; padding-top: 1rem; font-size: .85rem; color: #555; }
footer code { font-size: .78rem; word-break: break-all; }
</style>
</head>
<body>
<main>
<h1>{{.Name}}</h1>
<div class="banner {{.BannerClass}}" data-banner="{{.Banner}}">{{.Banner}}</div>

<section aria-label="components">
{{- if .Components}}
<ul style="list-style:none;padding:0">
{{- range .Components}}
<li class="component" data-status="{{.Status}}">
  <span>{{.Name}}{{if .Description}} — <span class="empty">{{.Description}}</span>{{end}}</span>
  <span class="status {{.Status}}">{{statText .Status}}</span>
</li>
{{- end}}
</ul>
{{- else}}
<p class="empty">No components yet — add one with <code>outpage component add</code>.</p>
{{- end}}
</section>

{{- if .Active}}
<section aria-label="active incidents">
<h2>Active incidents</h2>
{{- range .Active}}
<div class="incident" data-incident-kind="incident">
  <h3>{{.Name}}</h3>
  <span class="impact">{{.Impact}}</span>
  <ol class="timeline">
  {{- range .Updates}}
  <li><span class="u-status">{{title .Status}}</span><time>{{fmtTime .CreatedNS}}</time><br>{{.Body}}</li>
  {{- end}}
  </ol>
  {{- if .Components}}<p class="empty">Affected: {{join .Components ", "}}</p>{{end}}
</div>
{{- end}}
</section>
{{- end}}

{{- if .Maintenances}}
<section aria-label="scheduled maintenance">
<h2>Scheduled maintenance</h2>
{{- range .Maintenances}}
<div class="incident" data-incident-kind="maintenance">
  <h3>{{.Name}}</h3>
  <p>{{.StatusText}} — {{.Starts}} to {{.Ends}}</p>
  <ol class="timeline">
  {{- range .Updates}}
  <li><span class="u-status">{{title .Status}}</span><time>{{fmtTime .CreatedNS}}</time><br>{{.Body}}</li>
  {{- end}}
  </ol>
  {{- if .Components}}<p class="empty">Affected: {{join .Components ", "}}</p>{{end}}
</div>
{{- end}}
</section>
{{- end}}

{{- if .HasHistory}}
<section aria-label="incident history">
<h2>Past incidents</h2>
{{- range .History}}
<div class="incident" data-incident-kind="{{.Kind}}">
  <h3>{{.Name}}</h3>
  <span class="impact">{{.Impact}}</span>
  <ol class="timeline">
  {{- range .Updates}}
  <li><span class="u-status">{{title .Status}}</span><time>{{fmtTime .CreatedNS}}</time><br>{{.Body}}</li>
  {{- end}}
  </ol>
</div>
{{- end}}
</section>
{{- end}}

{{.Footer}}
</main>
</body>
</html>
`))

// worstOf orders component statuses by severity for the banner.
func worstOf(statuses []string) (string, string) {
	rank := map[string]int{
		"operational": 0, "maintenance": 1, "degraded": 2, "partial": 3, "major": 4,
	}
	banner := map[string][2]string{
		"operational": {"All systems operational", "ok"},
		"maintenance": {"Service under maintenance", "warn"},
		"degraded":    {"Degraded performance", "warn"},
		"partial":     {"Partial outage", "bad"},
		"major":       {"Major outage", "bad"},
	}
	best := 0
	for _, s := range statuses {
		if r, ok := rank[s]; ok && r > best {
			best = r
		}
	}
	for s, r := range rank {
		if r == best {
			b := banner[s]
			return b[0], b[1]
		}
	}
	return "All systems operational", "ok"
}

// statusText maps the five-word vocabulary to display text.
func statusText(s string) string {
	switch s {
	case "operational":
		return "Operational"
	case "degraded":
		return "Degraded performance"
	case "partial":
		return "Partial outage"
	case "major":
		return "Major outage"
	case "maintenance":
		return "Under maintenance"
	}
	return s
}

// title capitalizes a status word for display.
func title(s string) string {
	if s == "in_progress" {
		return "In progress"
	}
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// fmtTime renders a unix-nanos timestamp as "Oct 25, 2026 01:00 UTC".
func fmtTime(ns int64) string {
	return time.Unix(0, ns).UTC().Format("Jan 2, 2006 15:04") + " UTC"
}

// toView assembles the render view of one incident.
func toView(inc store.Incident, updates []store.Update, comps []string) incidentView {
	v := incidentView{
		Name: inc.Name, Impact: inc.Impact, Status: inc.Status, Kind: inc.Kind,
		StatusText: statusText(inc.Status), Updates: updates, Components: comps,
	}
	if inc.StartsNS != 0 {
		v.Starts = fmtTime(inc.StartsNS)
	}
	if inc.EndsNS != 0 {
		v.Ends = fmtTime(inc.EndsNS)
	}
	return v
}

// Render produces the full public status page HTML.
func Render(s *store.Store) (string, error) {
	comps := s.ListComponents()
	statuses := make([]string, len(comps))
	for i, c := range comps {
		statuses[i] = c.Status
	}
	banner, bannerClass := worstOf(statuses)

	incs, err := s.ListIncidents(0, false)
	if err != nil {
		return "", err
	}
	data := pageData{
		Name:        s.PageName(),
		Banner:      banner,
		BannerClass: bannerClass,
		Components:  comps,
		Footer:      template.HTML(donate.HTML()),
	}
	for _, inc := range incs {
		updates, err := s.IncidentUpdates(inc.ID)
		if err != nil {
			return "", err
		}
		comps, err := s.IncidentComponents(inc.ID)
		if err != nil {
			return "", err
		}
		v := toView(inc, updates, comps)
		switch {
		case inc.Status == "resolved" || inc.Status == "completed":
			if len(data.History) < 10 {
				data.History = append(data.History, v)
			}
		case inc.Kind == "maintenance":
			data.Maintenances = append(data.Maintenances, v)
		default:
			data.Active = append(data.Active, v)
		}
	}
	data.HasHistory = len(data.History) > 0

	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderDonate produces the standalone /donate page: both addresses, no
// forms, no payment elements of any kind.
func RenderDonate() string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Donate to Outpage</title>
<style>
body { font-family: system-ui, sans-serif; margin: 0; color: #1c1f24; background: #fafafa; }
main { max-width: 40rem; margin: 0 auto; padding: 2rem 1rem 4rem; }
code { background: #f0f0f0; padding: .15rem .4rem; border-radius: 4px; font-size: .9rem; word-break: break-all; }
.address { margin: .8rem 0; }
footer { margin-top: 3rem; border-top: 1px solid #e2e2e2; padding-top: 1rem; font-size: .85rem; color: #555; }
</style>
</head>
<body>
<main>
<h1>Donate to Outpage</h1>
<p>Outpage is free, MIT-licensed software. There is no paid tier, no
subscription, and no feature is gated on money — and there never will be.
Donations are the only funding mechanism.</p>
<p>If maintaining your status page stopped being a weekly chore, support
the project:</p>
<p class="address">Ethereum / USDC (ERC-20):<br><code>` + donate.Ethereum + `</code></p>
<p class="address">Bitcoin:<br><code>` + donate.Bitcoin + `</code></p>
<p>Both addresses are also printed by <code>outpage donate</code>. Wallet
changes require 4/4 maintainer consensus and 30-day public notice per
GOVERNANCE.md.</p>
<footer>` + donate.Ethereum + ` · ` + donate.Bitcoin + `</footer>
</main>
</body>
</html>
`
}
