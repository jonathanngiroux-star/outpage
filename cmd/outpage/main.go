// Command outpage is the single binary: status-page publisher, incident
// CLI, subscriber notifier, and Statuspage migration report. One process,
// one SQLite file, no external dependencies.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/migrate/statuspage"
	"github.com/jonathanngiroux-star/outpage/internal/page"
	"github.com/jonathanngiroux-star/outpage/internal/serve"
	"github.com/jonathanngiroux-star/outpage/internal/store"
	"github.com/jonathanngiroux-star/outpage/internal/subscribers"
)

// version is the single source of truth for the binary version. Overridden
// at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// envDefault reads an env var with a fallback.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `outpage — self-hosted public status page

Usage:
  outpage serve [--db PATH] [--http ADDR]
  outpage component add|status|list
  outpage incident open|update|list
  outpage maintenance add
  outpage subscriber add|list|count|remove
  outpage migrate --from=statuspage --dry-run [--format=json] [--input PATH]
  outpage page render
  outpage version
  outpage donate

Run 'outpage <command> -h' for flags. Docs: docs/ (README for the tour).`)
}

// run dispatches. Returns process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version":
		fmt.Fprintln(stdout, "outpage "+version)
		return 0
	case "donate":
		fmt.Fprint(stdout, donate.Text())
		return 0
	case "serve":
		return cmdServe(rest, stdout, stderr)
	case "component":
		return cmdComponent(rest, stdout, stderr)
	case "incident":
		return cmdIncident(rest, stdout, stderr)
	case "maintenance":
		return cmdMaintenance(rest, stdout, stderr)
	case "subscriber":
		return cmdSubscriber(rest, stdout, stderr)
	case "migrate":
		return cmdMigrate(rest, stdout, stderr)
	case "page":
		return cmdPage(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		usage(stderr)
		return 2
	}
}

// newFlagSet builds a flag set that reports errors to stderr.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parseDBFlag opens the store for commands whose only shared flag is --db.
// A nonzero code means the caller returns it immediately.
func parseDBFlag(name string, args []string, stderr io.Writer) (*store.Store, int) {
	fs := newFlagSet(name, stderr)
	db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
		return nil, 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "outpage: %v\n", err)
		return nil, 1
	}
	return st, 0
}

// requireFlags returns a usage error (exit 2) naming every missing flag.
func requireFlags(stderr io.Writer, missing []string) int {
	fmt.Fprintf(stderr, "outpage: required flag missing: %s\n", strings.Join(missing, ", "))
	return 2
}

// cmdServe publishes the public page until killed.
func cmdServe(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("serve", stderr)
	db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
	addr := fs.String("http", envDefault("OUTPAGE_HTTP", "127.0.0.1:8080"), "listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
		return 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "outpage: %v\n", err)
		return 1
	}
	defer st.Close()
	srv, err := serve.New(st, *addr)
	if err != nil {
		fmt.Fprintf(stderr, "outpage serve: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "outpage serving on %s — public page at http://%s/ (donate: /donate)\n",
		srv.Addr().String(), srv.Addr().String())
	if err := srv.Start(); err != nil {
		fmt.Fprintf(stderr, "outpage serve: %v\n", err)
		return 1
	}
	return 0
}

// cmdComponent handles component add/status/list.
func cmdComponent(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "outpage component: need add|status|list")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		fs := newFlagSet("component add", stderr)
		db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
		name := fs.String("name", "", "component name")
		desc := fs.String("description", "", "optional description")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(stderr, "outpage: unexpected argument %q — use --name/--description, not both a flag and a positional\n", fs.Arg(0))
			return 2
		}
		if strings.TrimSpace(*name) == "" {
			return requireFlags(stderr, []string{"--name"})
		}
		st, err := store.Open(*db)
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		defer st.Close()
		c, err := st.AddComponent(*name, *desc)
		if err != nil {
			fmt.Fprintf(stderr, "outpage component add: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "component %d created: %s (%s)\n", c.ID, c.Name, c.Status)
		return 0
	case "status":
		fs := newFlagSet("component status", stderr)
		db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
		name := fs.String("name", "", "component name")
		status := fs.String("status", "", "one of: operational, degraded, partial, major, maintenance")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
			return 2
		}
		var missing []string
		if *name == "" {
			missing = append(missing, "--name")
		}
		if *status == "" {
			missing = append(missing, "--status")
		}
		if len(missing) > 0 {
			return requireFlags(stderr, missing)
		}
		st, err := store.Open(*db)
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		defer st.Close()
		if err := st.SetComponentStatus(*name, *status); err != nil {
			fmt.Fprintf(stderr, "outpage component status: %v (rejected value %q)\n", err, *status)
			return 1
		}
		fmt.Fprintf(stdout, "component %s set to %s\n", *name, *status)
		return 0
	case "list":
		st, code := parseDBFlag("component list", rest, stderr)
		if code != 0 {
			return code
		}
		defer st.Close()
		for _, c := range st.ListComponents() {
			fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\n", c.ID, c.Name, c.Status, c.Description)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "outpage component: unknown subcommand %q (add|status|list)\n", sub)
		return 2
	}
}

// cmdIncident handles incident open/update/list.
func cmdIncident(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "outpage incident: need open|update|list")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "open":
		fs := newFlagSet("incident open", stderr)
		db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
		name := fs.String("name", "", "incident title")
		impact := fs.String("impact", "minor", "none|minor|major|critical")
		comps := fs.String("components", "", "comma-separated component names")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
			return 2
		}
		if *name == "" {
			return requireFlags(stderr, []string{"--name"})
		}
		st, err := store.Open(*db)
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		defer st.Close()
		var compList []string
		if strings.TrimSpace(*comps) != "" {
			compList = strings.Split(*comps, ",")
		}
		inc, err := st.OpenIncident(*name, *impact, compList)
		if err != nil {
			fmt.Fprintf(stderr, "outpage incident open: %v\n", err)
			return 1
		}
		res := notifyLast(st, inc)
		fmt.Fprintf(stdout, "incident %d opened (%s) — notifications: sent=%d failed=%d skipped=%d\n",
			inc.ID, inc.Status, res.Sent, res.Failed, res.Skipped)
		return 0
	case "update":
		fs := newFlagSet("incident update", stderr)
		db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
		id := fs.String("id", "", "incident id")
		status := fs.String("status", "", "investigating|identified|monitoring|resolved")
		body := fs.String("body", "", "update text")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
			return 2
		}
		var missing []string
		if *id == "" {
			missing = append(missing, "--id")
		}
		if *status == "" {
			missing = append(missing, "--status")
		}
		if *body == "" {
			missing = append(missing, "--body")
		}
		if len(missing) > 0 {
			return requireFlags(stderr, missing)
		}
		st, err := store.Open(*db)
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		defer st.Close()
		incID, err := strconv.ParseInt(*id, 10, 64)
		if err != nil {
			fmt.Fprintf(stderr, "outpage incident update: --id must be a number (got %q)\n", *id)
			return 2
		}
		inc, err := st.Incident(incID)
		if err != nil {
			fmt.Fprintf(stderr, "outpage incident update: %v\n", err)
			return 1
		}
		upd, err := st.AddUpdate(incID, *status, *body)
		if err != nil {
			// Name the current state so the operator knows why it refused.
			fmt.Fprintf(stderr, "outpage incident update: %v (incident %d is currently %q)\n", err, incID, inc.Status)
			return 1
		}
		res := subscribers.Notify(st, inc, upd, smtpConfigFromEnv())
		fmt.Fprintf(stdout, "incident %d → %s — notifications: sent=%d failed=%d skipped=%d\n",
			incID, upd.Status, res.Sent, res.Failed, res.Skipped)
		return 0
	case "list":
		st, code := parseDBFlag("incident list", rest, stderr)
		if code != 0 {
			return code
		}
		defer st.Close()
		incs, err := st.ListIncidents(0, false)
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		for _, inc := range incs {
			fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\t%s\n", inc.ID, inc.Kind, inc.Name, inc.Status, inc.Impact)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "outpage incident: unknown subcommand %q (open|update|list)\n", sub)
		return 2
	}
}

// notifyLast notifies subscribers of an incident's latest update (used by
// `incident open`, whose first update is written by the store).
func notifyLast(st *store.Store, inc store.Incident) subscribers.Result {
	ups, err := st.IncidentUpdates(inc.ID)
	if err != nil || len(ups) == 0 {
		return subscribers.Result{}
	}
	return subscribers.Notify(st, inc, ups[len(ups)-1], smtpConfigFromEnv())
}

// cmdMaintenance adds a scheduled maintenance window.
func cmdMaintenance(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "add" {
		fmt.Fprintln(stderr, "outpage maintenance: need 'add'")
		return 2
	}
	rest := args[1:]
	fs := newFlagSet("maintenance add", stderr)
	db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
	name := fs.String("name", "", "maintenance title")
	comps := fs.String("components", "", "comma-separated component names")
	start := fs.String("start", "", "window start (RFC3339)")
	end := fs.String("end", "", "window end (RFC3339)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
		return 2
	}
	var missing []string
	if *name == "" {
		missing = append(missing, "--name")
	}
	if *start == "" {
		missing = append(missing, "--start")
	}
	if *end == "" {
		missing = append(missing, "--end")
	}
	if len(missing) > 0 {
		return requireFlags(stderr, missing)
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "outpage: %v\n", err)
		return 1
	}
	defer st.Close()
	startT, err := time.Parse(time.RFC3339, *start)
	if err != nil {
		fmt.Fprintf(stderr, "outpage maintenance add: --start must be RFC3339 (got %q)\n", *start)
		return 2
	}
	endT, err := time.Parse(time.RFC3339, *end)
	if err != nil {
		fmt.Fprintf(stderr, "outpage maintenance add: --end must be RFC3339 (got %q)\n", *end)
		return 2
	}
	var compList []string
	if strings.TrimSpace(*comps) != "" {
		compList = strings.Split(*comps, ",")
	}
	m, err := st.OpenMaintenance(*name, compList, startT, endT)
	if err != nil {
		fmt.Fprintf(stderr, "outpage maintenance add: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "maintenance %d scheduled (%s → %s)\n", m.ID,
		startT.Format("Jan 2, 2006 15:04 MST"), endT.Format("Jan 2, 2006 15:04 MST"))
	return 0
}

// smtpConfigFromEnv reads the user-supplied SMTP relay from the
// environment. Empty Host = not configured (email subscribers are
// skipped and recorded, never guessed).
func smtpConfigFromEnv() subscribers.SMTPConfig {
	port := 587
	if p := os.Getenv("OUTPAGE_SMTP_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65536 {
			port = n
		}
	}
	return subscribers.SMTPConfig{
		Host: os.Getenv("OUTPAGE_SMTP_HOST"),
		Port: port,
		From: os.Getenv("OUTPAGE_SMTP_FROM"),
		User: os.Getenv("OUTPAGE_SMTP_USER"),
		Pass: os.Getenv("OUTPAGE_SMTP_PASS"),
	}
}

// cmdSubscriber handles subscriber add/list/count/remove.
func cmdSubscriber(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "outpage subscriber: need add|list|count|remove")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		fs := newFlagSet("subscriber add", stderr)
		db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
		kind := fs.String("kind", "", "email|webhook")
		address := fs.String("address", "", "email address or https:// webhook URL")
		secret := fs.String("secret", "", "webhook signing secret")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
			return 2
		}
		var missing []string
		if *kind == "" {
			missing = append(missing, "--kind")
		}
		if *address == "" {
			missing = append(missing, "--address")
		}
		if len(missing) > 0 {
			return requireFlags(stderr, missing)
		}
		st, err := store.Open(*db)
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		defer st.Close()
		s, err := st.AddSubscriber(*kind, *address, *secret)
		if err != nil {
			fmt.Fprintf(stderr, "outpage subscriber add: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "subscriber %d added (%s)\n", s.ID, s.Kind)
		return 0
	case "list":
		st, code := parseDBFlag("subscriber list", rest, stderr)
		if code != 0 {
			return code
		}
		defer st.Close()
		subs, err := st.ListSubscribers()
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		for _, s := range subs {
			fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\n", s.ID, s.Kind, s.Address, s.Secret)
		}
		return 0
	case "count":
		st, code := parseDBFlag("subscriber count", rest, stderr)
		if code != 0 {
			return code
		}
		defer st.Close()
		emails, hooks, err := st.SubscriberCounts()
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "email=%d webhook=%d\n", emails, hooks)
		return 0
	case "remove":
		fs := newFlagSet("subscriber remove", stderr)
		db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
		id := fs.String("id", "", "subscriber id")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if *id == "" {
			return requireFlags(stderr, []string{"--id"})
		}
		st, err := store.Open(*db)
		if err != nil {
			fmt.Fprintf(stderr, "outpage: %v\n", err)
			return 1
		}
		defer st.Close()
		idn, err := strconv.ParseInt(*id, 10, 64)
		if err != nil {
			fmt.Fprintf(stderr, "outpage subscriber remove: --id must be a number (got %q)\n", *id)
			return 2
		}
		if err := st.DeleteSubscriber(idn); err != nil {
			fmt.Fprintf(stderr, "outpage subscriber remove: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "subscriber %d removed\n", idn)
		return 0
	default:
		fmt.Fprintf(stderr, "outpage subscriber: unknown subcommand %q (add|list|count|remove)\n", sub)
		return 2
	}
}

// cmdMigrate runs the Statuspage dry-run report. Dry-run is the only mode.
func cmdMigrate(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("migrate", stderr)
	from := fs.String("from", "", "source system (statuspage)")
	dryRun := fs.Bool("dry-run", false, "report only — migrations are always dry-run")
	format := fs.String("format", "text", "text|json")
	input := fs.String("input", "", "path to the export file (default: the committed fixture)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
		return 2
	}
	if *from != "statuspage" {
		fmt.Fprintf(stderr, "outpage migrate: unknown --from %q — only statuspage is supported\n", *from)
		return 2
	}
	if !*dryRun {
		fmt.Fprintln(stderr, "outpage migrate: import is not implemented yet — dry-run only in v0.1 (nothing is mutated)")
		return 2
	}
	path := *input
	if path == "" {
		path = "testdata/fixtures/statuspage/sample-export.json"
	}
	exp, err := statuspage.Parse(path)
	if err != nil {
		fmt.Fprintf(stderr, "outpage migrate: %v\n", err)
		return 1
	}
	rep := statuspage.Analyze(exp)
	if *format == "json" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "outpage migrate: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	fmt.Fprintf(stdout, "Statuspage dry-run migration report\n")
	fmt.Fprintf(stdout, "  mapped=%d partial=%d unmapped=%d\n", rep.Counts.Mapped, rep.Counts.Partial, rep.Counts.Unmapped)
	for _, m := range rep.Mappings {
		fmt.Fprintf(stdout, "  [%s] %s %s %q\n", m.Result, m.Key, m.SourceID, m.Name)
		if len(m.Dropped) > 0 {
			fmt.Fprintf(stdout, "    dropped: %s\n", strings.Join(m.Dropped, ", "))
		}
		if m.Note != "" {
			fmt.Fprintf(stdout, "    note: %s\n", m.Note)
		}
	}
	for _, r := range rep.Risks {
		fmt.Fprintf(stdout, "  risk %s — %s\n", r.Key, r.Note)
	}
	return 0
}

// cmdPage renders the public page HTML to stdout (for previewing or
// piping to a static host).
func cmdPage(rest []string, stdout, stderr io.Writer) int {
	st, code := parseDBFlag("page render", rest, stderr)
	if code != 0 {
		return code
	}
	defer st.Close()
	html, err := page.Render(st)
	if err != nil {
		fmt.Fprintf(stderr, "outpage page: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, html)
	return 0
}
