// Package store is the SQLite persistence layer: components, incidents
// (with updates), maintenance windows, subscribers, notification log,
// and page metadata. One file, no server, no cgo. Timestamps are INTEGER
// unix nanos — ordering reads integers, never TEXT (RFC3339 strings with
// mixed fraction lengths sort wrong lexicographically).
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/incident"

	_ "modernc.org/sqlite"
)

// Errors surfaced to callers (CLI, serve, GUI, TUI).
var (
	ErrDuplicate         = errors.New("duplicate")
	ErrNotFound          = errors.New("not found")
	ErrInvalidSubscriber = errors.New("invalid subscriber")
)

// Store wraps the SQLite database.
type Store struct {
	db   *sql.DB
	path string
	// mu serializes multi-statement write transactions. SQLite is
	// single-writer; a deferred BEGIN that upgrades to a write under
	// concurrency fails immediately with SQLITE_BUSY_SNAPSHOT, which
	// busy_timeout does NOT retry. One process = one Store = this mutex
	// closes the race; cross-process writers fall back to busy_timeout
	// on plain writes.
	mu sync.Mutex
}

// Open opens (creating if needed) the database at path and applies the
// schema. SetMaxOpenConns(1) makes BEGIN-level serialization possible.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db, path: path}, nil
}

// Path returns the on-disk database path.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS components (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  description TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'operational',
  created_ns  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS incidents (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL DEFAULT 'incident', -- incident | maintenance
  name        TEXT NOT NULL,
  impact      TEXT NOT NULL DEFAULT 'none',
  status      TEXT NOT NULL,
  created_ns  INTEGER NOT NULL,
  resolved_ns INTEGER,
  starts_ns   INTEGER,
  ends_ns     INTEGER
);
CREATE INDEX IF NOT EXISTS idx_incidents_kind_created ON incidents(kind, created_ns);
CREATE TABLE IF NOT EXISTS incident_updates (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  incident_id INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  status      TEXT NOT NULL,
  body        TEXT NOT NULL,
  created_ns  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_updates_incident ON incident_updates(incident_id, created_ns);
CREATE TABLE IF NOT EXISTS incident_components (
  incident_id  INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  component_id INTEGER NOT NULL REFERENCES components(id) ON DELETE CASCADE,
  PRIMARY KEY (incident_id, component_id)
);
CREATE TABLE IF NOT EXISTS subscribers (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  kind       TEXT NOT NULL, -- email | webhook
  address    TEXT NOT NULL UNIQUE COLLATE NOCASE,
  secret     TEXT NOT NULL DEFAULT '',
  created_ns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS notifications (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  incident_id    INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  subscriber_id INTEGER NOT NULL,
  channel       TEXT NOT NULL,
  status        TEXT NOT NULL, -- sent | failed
  detail        TEXT NOT NULL DEFAULT '',
  created_ns    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifications_incident ON notifications(incident_id, created_ns);
`

// Component is one service row on the public page.
type Component struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedNS   int64  `json:"created_ns"`
}

// Incident is one row of either track (incident or maintenance window).
type Incident struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"` // incident | maintenance
	Name       string `json:"name"`
	Impact     string `json:"impact"`
	Status     string `json:"status"`
	CreatedNS  int64  `json:"created_ns"`
	ResolvedNS int64  `json:"resolved_ns"` // 0 when open
	StartsNS   int64  `json:"starts_ns"`   // maintenance only
	EndsNS     int64  `json:"ends_ns"`     // maintenance only
}

// Update is one entry in an incident's (or maintenance's) timeline.
type Update struct {
	ID         int64  `json:"id"`
	IncidentID int64  `json:"incident_id"`
	Status     string `json:"status"`
	Body       string `json:"body"`
	CreatedNS  int64  `json:"created_ns"`
}

// Subscriber is one notification target. Emails are stored locally;
// webhooks carry a per-subscriber signing secret (never in exports).
type Subscriber struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Address   string `json:"address"`
	Secret    string `json:"secret,omitempty"`
	CreatedNS int64  `json:"created_ns"`
}

// Notification is one delivery attempt record.
type Notification struct {
	ID           int64  `json:"id"`
	IncidentID   int64  `json:"incident_id"`
	SubscriberID int64  `json:"subscriber_id"`
	Channel      string `json:"channel"`
	Status       string `json:"status"`
	Detail       string `json:"detail"`
	CreatedNS    int64  `json:"created_ns"`
}

func nowNS() int64 { return time.Now().UnixNano() }

// ---- meta --------------------------------------------------------------

func (s *Store) setMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) getMeta(key, fallback string) string {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err != nil {
		return fallback
	}
	return v
}

// SetPageName sets the public page title.
func (s *Store) SetPageName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return incident.ErrInvalidStatus
	}
	return s.setMeta("page_name", name)
}

// PageName returns the public page title ("Status" before first set).
func (s *Store) PageName() string { return s.getMeta("page_name", "Status") }

// SetWizardDone marks the first-run wizard complete.
func (s *Store) SetWizardDone() error { return s.setMeta("wizard_done", "1") }

// WizardDone reports whether the first-run wizard has been completed.
func (s *Store) WizardDone() bool { return s.getMeta("wizard_done", "") == "1" }

// ---- components ----------------------------------------------------------

// AddComponent inserts a new component with status operational.
func (s *Store) AddComponent(name, description string) (Component, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Component{}, incident.ErrInvalidStatus
	}
	c := Component{Name: name, Description: strings.TrimSpace(description),
		Status: incident.StatusOperational, CreatedNS: nowNS()}
	res, err := s.db.Exec(`INSERT INTO components(name, description, status, created_ns)
		VALUES(?,?,?,?)`, c.Name, c.Description, c.Status, c.CreatedNS)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Component{}, fmt.Errorf("%w: component %q", ErrDuplicate, name)
		}
		return Component{}, err
	}
	c.ID, err = res.LastInsertId()
	return c, err
}

// SetComponentStatus updates one component's status after validating the
// vocabulary.
func (s *Store) SetComponentStatus(name, status string) error {
	if err := incident.ValidComponentStatus(status); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE components SET status=? WHERE name=? COLLATE NOCASE`, status, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: component %q", ErrNotFound, name)
	}
	return nil
}

// ListComponents returns every component, ordered by creation.
func (s *Store) ListComponents() []Component {
	rows, err := s.db.Query(`SELECT id, name, description, status, created_ns
		FROM components ORDER BY created_ns, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Component
	for rows.Next() {
		var c Component
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Status, &c.CreatedNS); err != nil {
			return nil
		}
		out = append(out, c)
	}
	return out
}

// ---- incidents + maintenance --------------------------------------------

// OpenIncident creates a new incident in status investigating and writes
// the first timeline update.
func (s *Store) OpenIncident(name string, impact string, componentNames []string) (Incident, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Incident{}, incident.ErrInvalidStatus
	}
	if err := incident.ValidImpact(impact); err != nil {
		return Incident{}, err
	}
	inc := Incident{Kind: "incident", Name: name, Impact: impact,
		Status: incident.StatusInvestigating, CreatedNS: nowNS()}

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Incident{}, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO incidents(kind, name, impact, status, created_ns)
		VALUES(?,?,?,?,?)`, inc.Kind, inc.Name, inc.Impact, inc.Status, inc.CreatedNS)
	if err != nil {
		return Incident{}, err
	}
	inc.ID, err = res.LastInsertId()
	if err != nil {
		return Incident{}, err
	}
	for _, cn := range componentNames {
		var cid int64
		if err := tx.QueryRow(`SELECT id FROM components WHERE name=? COLLATE NOCASE`, cn).Scan(&cid); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Incident{}, fmt.Errorf("%w: component %q", ErrNotFound, cn)
			}
			return Incident{}, err
		}
		if _, err := tx.Exec(`INSERT INTO incident_components(incident_id, component_id) VALUES(?,?)`,
			inc.ID, cid); err != nil {
			return Incident{}, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO incident_updates(incident_id, status, body, created_ns)
		VALUES(?,?,?,?)`, inc.ID, inc.Status, "Incident opened.", inc.CreatedNS); err != nil {
		return Incident{}, err
	}
	if err := tx.Commit(); err != nil {
		return Incident{}, err
	}
	return inc, nil
}

// OpenMaintenance creates a scheduled maintenance window.
func (s *Store) OpenMaintenance(name string, componentNames []string, starts, ends time.Time) (Incident, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Incident{}, incident.ErrInvalidStatus
	}
	if !ends.After(starts) {
		return Incident{}, fmt.Errorf("%w: window ends before it starts", incident.ErrInvalidStatus)
	}
	inc := Incident{Kind: "maintenance", Name: name, Impact: "none",
		Status:    incident.StatusScheduled,
		CreatedNS: nowNS(),
		StartsNS:  starts.UnixNano(),
		EndsNS:    ends.UnixNano()}

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Incident{}, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO incidents(kind, name, impact, status, created_ns, starts_ns, ends_ns)
		VALUES(?,?,?,?,?,?,?)`,
		inc.Kind, inc.Name, inc.Impact, inc.Status, inc.CreatedNS, inc.StartsNS, inc.EndsNS)
	if err != nil {
		return Incident{}, err
	}
	inc.ID, err = res.LastInsertId()
	if err != nil {
		return Incident{}, err
	}
	for _, cn := range componentNames {
		var cid int64
		if err := tx.QueryRow(`SELECT id FROM components WHERE name=? COLLATE NOCASE`, cn).Scan(&cid); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Incident{}, fmt.Errorf("%w: component %q", ErrNotFound, cn)
			}
			return Incident{}, err
		}
		if _, err := tx.Exec(`INSERT INTO incident_components(incident_id, component_id) VALUES(?,?)`,
			inc.ID, cid); err != nil {
			return Incident{}, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO incident_updates(incident_id, status, body, created_ns)
		VALUES(?,?,?,?)`, inc.ID, inc.Status, "Maintenance scheduled.", inc.CreatedNS); err != nil {
		return Incident{}, err
	}
	if err := tx.Commit(); err != nil {
		return Incident{}, err
	}
	return inc, nil
}

// AddUpdate appends a timeline update, enforcing the transition table and
// stamping resolved_ns when an incident resolves. The status read and the
// writes happen inside one transaction — the single connection makes
// check-then-write atomic against other in-process callers.
func (s *Store) AddUpdate(incidentID int64, status, body string) (Update, error) {
	if strings.TrimSpace(body) == "" {
		return Update{}, incident.ErrInvalidStatus
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Update{}, err
	}
	defer tx.Rollback()

	var current string
	err = tx.QueryRow(`SELECT status FROM incidents WHERE id=?`, incidentID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return Update{}, fmt.Errorf("%w: incident %d", ErrNotFound, incidentID)
	}
	if err != nil {
		return Update{}, err
	}
	if err := incident.CanTransition(current, status); err != nil {
		return Update{}, err
	}
	u := Update{IncidentID: incidentID, Status: status, Body: strings.TrimSpace(body), CreatedNS: nowNS()}
	res, err := tx.Exec(`INSERT INTO incident_updates(incident_id, status, body, created_ns)
		VALUES(?,?,?,?)`, u.IncidentID, u.Status, u.Body, u.CreatedNS)
	if err != nil {
		return Update{}, err
	}
	if u.ID, err = res.LastInsertId(); err != nil {
		return Update{}, err
	}
	if status == incident.StatusResolved {
		if _, err := tx.Exec(`UPDATE incidents SET status=?, resolved_ns=? WHERE id=?`,
			status, u.CreatedNS, incidentID); err != nil {
			return Update{}, err
		}
	} else {
		if _, err := tx.Exec(`UPDATE incidents SET status=? WHERE id=?`, status, incidentID); err != nil {
			return Update{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Update{}, err
	}
	return u, nil
}

// Incident returns one incident or maintenance row.
func (s *Store) Incident(id int64) (Incident, error) {
	var inc Incident
	err := s.db.QueryRow(`SELECT id, kind, name, impact, status, created_ns,
		COALESCE(resolved_ns,0), COALESCE(starts_ns,0), COALESCE(ends_ns,0)
		FROM incidents WHERE id=?`, id).
		Scan(&inc.ID, &inc.Kind, &inc.Name, &inc.Impact, &inc.Status, &inc.CreatedNS,
			&inc.ResolvedNS, &inc.StartsNS, &inc.EndsNS)
	if errors.Is(err, sql.ErrNoRows) {
		return Incident{}, fmt.Errorf("%w: incident %d", ErrNotFound, id)
	}
	return inc, err
}

// ListIncidents returns incidents (both kinds) newest first. limit<=0
// means all; activeOnly drops resolved/completed.
func (s *Store) ListIncidents(limit int, activeOnly bool) ([]Incident, error) {
	q := `SELECT id, kind, name, impact, status, created_ns,
		COALESCE(resolved_ns,0), COALESCE(starts_ns,0), COALESCE(ends_ns,0)
		FROM incidents`
	if activeOnly {
		q += ` WHERE status NOT IN ('resolved','completed')`
	}
	q += ` ORDER BY created_ns DESC, id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
	}
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.Query(q, limit)
	} else {
		rows, err = s.db.Query(q)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.Kind, &inc.Name, &inc.Impact, &inc.Status, &inc.CreatedNS,
			&inc.ResolvedNS, &inc.StartsNS, &inc.EndsNS); err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// IncidentUpdates returns one incident's timeline oldest first.
func (s *Store) IncidentUpdates(incidentID int64) ([]Update, error) {
	rows, err := s.db.Query(`SELECT id, incident_id, status, body, created_ns
		FROM incident_updates WHERE incident_id=? ORDER BY created_ns, id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Update
	for rows.Next() {
		var u Update
		if err := rows.Scan(&u.ID, &u.IncidentID, &u.Status, &u.Body, &u.CreatedNS); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// IncidentComponents returns the names of the components linked to one
// incident, alphabetical for stable display.
func (s *Store) IncidentComponents(incidentID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT c.name FROM incident_components ic
		JOIN components c ON c.id = ic.component_id
		WHERE ic.incident_id=? ORDER BY c.name`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---- subscribers ---------------------------------------------------------

// validateSubscriber enforces the address shape per kind. Webhooks must be
// https — they carry incident data — except explicit loopback hosts, which
// are allowed over http for local receivers and tests. SMS is rejected:
// Outpage never operates an SMS gateway (scope lock).
func validateSubscriber(kind, address string) error {
	switch kind {
	case "email":
		a, err := mail.ParseAddress(address)
		if err != nil || a.Address != address {
			return fmt.Errorf("%w: not a valid email address", ErrInvalidSubscriber)
		}
	case "webhook":
		u, err := url.Parse(address)
		if err != nil || u.Host == "" {
			return fmt.Errorf("%w: webhooks must be https:// URLs", ErrInvalidSubscriber)
		}
		switch u.Scheme {
		case "https":
		case "http":
			host := u.Hostname()
			if host != "127.0.0.1" && host != "::1" && host != "localhost" {
				return fmt.Errorf("%w: webhooks must be https:// URLs", ErrInvalidSubscriber)
			}
		default:
			return fmt.Errorf("%w: webhooks must be https:// URLs", ErrInvalidSubscriber)
		}
	default:
		return fmt.Errorf("%w: kind must be email or webhook (no SMS, ever)", ErrInvalidSubscriber)
	}
	return nil
}

// AddSubscriber validates and stores one subscriber. email addresses must
// parse; webhooks must be https URLs (loopback http allowed for local
// receivers). SMS is rejected — Outpage never operates an SMS gateway
// (scope lock).
func (s *Store) AddSubscriber(kind, address, secret string) (Subscriber, error) {
	if err := validateSubscriber(kind, address); err != nil {
		return Subscriber{}, err
	}
	sub := Subscriber{Kind: kind, Address: address, Secret: secret, CreatedNS: nowNS()}
	res, err := s.db.Exec(`INSERT INTO subscribers(kind, address, secret, created_ns)
		VALUES(?,?,?,?)`, sub.Kind, sub.Address, sub.Secret, sub.CreatedNS)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Subscriber{}, fmt.Errorf("%w: subscriber %q", ErrDuplicate, address)
		}
		return Subscriber{}, err
	}
	sub.ID, err = res.LastInsertId()
	return sub, err
}

// ListSubscribers returns every subscriber ordered by creation. Emails are
// stored locally in this SQLite file; they never appear in public HTML.
func (s *Store) ListSubscribers() ([]Subscriber, error) {
	rows, err := s.db.Query(`SELECT id, kind, address, secret, created_ns
		FROM subscribers ORDER BY created_ns, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		var sub Subscriber
		if err := rows.Scan(&sub.ID, &sub.Kind, &sub.Address, &sub.Secret, &sub.CreatedNS); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// SubscriberCounts returns (email, webhook) counts.
func (s *Store) SubscriberCounts() (int, int, error) {
	var emailCount, webhookCount int
	err := s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN kind='email' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='webhook' THEN 1 ELSE 0 END),0)
		FROM subscribers`).Scan(&emailCount, &webhookCount)
	return emailCount, webhookCount, err
}

// SetSubscriberAddress updates one subscriber's address (re-validated).
func (s *Store) SetSubscriberAddress(id int64, address string) error {
	var kind string
	if err := s.db.QueryRow(`SELECT kind FROM subscribers WHERE id=?`, id).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: subscriber %d", ErrNotFound, id)
		}
		return err
	}
	if err := validateSubscriber(kind, address); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE subscribers SET address=? WHERE id=?`, address, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("%w: subscriber %q", ErrDuplicate, address)
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: subscriber %d", ErrNotFound, id)
	}
	return nil
}

// DeleteSubscriber removes one subscriber by id.
func (s *Store) DeleteSubscriber(id int64) error {
	res, err := s.db.Exec(`DELETE FROM subscribers WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: subscriber %d", ErrNotFound, id)
	}
	return nil
}

// ---- notification log ----------------------------------------------------

// RecordNotification logs one delivery attempt. SMTP failures are logged
// here and never roll back the incident — the timeline is the source of
// truth, delivery is best-effort.
func (s *Store) RecordNotification(incidentID, subscriberID int64, channel, status, detail string) error {
	_, err := s.db.Exec(`INSERT INTO notifications(incident_id, subscriber_id, channel, status, detail, created_ns)
		VALUES(?,?,?,?,?,?)`, incidentID, subscriberID, channel, status, detail, nowNS())
	return err
}

// Notifications returns one incident's delivery log, oldest first.
func (s *Store) Notifications(incidentID int64) ([]Notification, error) {
	rows, err := s.db.Query(`SELECT id, incident_id, subscriber_id, channel, status, detail, created_ns
		FROM notifications WHERE incident_id=? ORDER BY created_ns, id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.IncidentID, &n.SubscriberID, &n.Channel, &n.Status, &n.Detail, &n.CreatedNS); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
