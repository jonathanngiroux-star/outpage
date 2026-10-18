// Package incident is the status vocabulary and transition rules shared
// by every surface (CLI, public page, GUI, TUI, migrate). Two tracks run
// through one table: incident (investigating → identified → monitoring →
// resolved) and maintenance (scheduled → in_progress → completed).
// Everything else is an illegal transition — the state machine is the
// product's spine, so it lives in pure code with table-driven tests.
package incident

import "errors"

// Status vocabulary.
const (
	StatusInvestigating = "investigating"
	StatusIdentified    = "identified"
	StatusMonitoring    = "monitoring"
	StatusResolved      = "resolved"

	StatusScheduled  = "scheduled"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
)

// Impact vocabulary (mirrors Statuspage).
const (
	ImpactNone     = "none"
	ImpactMinor    = "minor"
	ImpactMajor    = "major"
	ImpactCritical = "critical"
)

// Component status vocabulary — exactly five words, no synonyms.
const (
	StatusOperational = "operational"
	StatusDegraded    = "degraded"
	StatusPartial     = "partial"
	StatusMajor       = "major"
	StatusMaintenance = "maintenance"
)

// Errors.
var (
	ErrIllegalTransition = errors.New("illegal status transition")
	ErrInvalidStatus     = errors.New("invalid status")
)

// transitions is the exhaustive legal-transition table. Self-loops,
// backwards moves, and cross-track moves are absent by construction.
var transitions = map[string][]string{
	// Incident track.
	StatusInvestigating: {StatusIdentified, StatusMonitoring, StatusResolved},
	StatusIdentified:    {StatusMonitoring, StatusResolved},
	StatusMonitoring:    {StatusResolved},
	// Maintenance track.
	StatusScheduled:  {StatusInProgress, StatusCompleted},
	StatusInProgress: {StatusCompleted},
}

// validStatus reports membership in either track (terminal states
// resolved/completed are valid current statuses, just not sources).
func validStatus(s string) bool {
	switch s {
	case StatusInvestigating, StatusIdentified, StatusMonitoring, StatusResolved,
		StatusScheduled, StatusInProgress, StatusCompleted:
		return true
	}
	return false
}

// CanTransition validates a status move. Resolved and completed are
// terminal; cross-track moves are illegal; unknown statuses are invalid
// input.
func CanTransition(from, to string) error {
	if !validStatus(from) {
		return ErrInvalidStatus
	}
	if _, ok := transitions[from]; !ok {
		return ErrIllegalTransition // terminal source (resolved/completed)
	}
	for _, next := range transitions[from] {
		if next == to {
			return nil
		}
	}
	return ErrIllegalTransition
}

// ValidComponentStatus accepts exactly the five-word vocabulary.
func ValidComponentStatus(s string) error {
	switch s {
	case StatusOperational, StatusDegraded, StatusPartial, StatusMajor, StatusMaintenance:
		return nil
	}
	return ErrInvalidStatus
}

// ValidImpact accepts the four-word impact vocabulary.
func ValidImpact(s string) error {
	switch s {
	case ImpactNone, ImpactMinor, ImpactMajor, ImpactCritical:
		return nil
	}
	return ErrInvalidStatus
}

// IsIncidentStatus / IsMaintenanceStatus let callers (e.g. the store)
// enforce the track at creation time.
func IsIncidentStatus(s string) bool {
	switch s {
	case StatusInvestigating, StatusIdentified, StatusMonitoring, StatusResolved:
		return true
	}
	return false
}

func IsMaintenanceStatus(s string) bool {
	switch s {
	case StatusScheduled, StatusInProgress, StatusCompleted:
		return true
	}
	return false
}
