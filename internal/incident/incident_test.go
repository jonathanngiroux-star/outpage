package incident

import (
	"errors"
	"testing"
)

// The state machine is the product's spine: every legal transition is
// enumerated, everything else is rejected with ErrIllegalTransition.
func TestCanTransitionTable(t *testing.T) {
	legal := [][2]string{
		{StatusInvestigating, StatusIdentified},
		{StatusInvestigating, StatusMonitoring},
		{StatusInvestigating, StatusResolved},
		{StatusIdentified, StatusMonitoring},
		{StatusIdentified, StatusResolved},
		{StatusMonitoring, StatusResolved},
	}
	for _, c := range legal {
		if err := CanTransition(c[0], c[1]); err != nil {
			t.Errorf("CanTransition(%s -> %s) = %v, want nil", c[0], c[1], err)
		}
	}
	illegal := [][2]string{
		{StatusResolved, StatusInvestigating}, // resolved is terminal
		{StatusResolved, StatusMonitoring},
		{StatusMonitoring, StatusInvestigating}, // no backwards
		{StatusMonitoring, StatusIdentified},
		{StatusIdentified, StatusInvestigating},
		{StatusInvestigating, StatusInvestigating}, // no self
	}
	for _, c := range illegal {
		if err := CanTransition(c[0], c[1]); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("CanTransition(%s -> %s) = %v, want ErrIllegalTransition", c[0], c[1], err)
		}
	}
	// Unknown statuses are invalid input, not transition denials.
	if err := CanTransition("bogus", StatusResolved); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("CanTransition(bogus -> resolved) = %v, want ErrInvalidStatus", err)
	}
}

// Maintenance windows run their own track: scheduled -> in_progress ->
// completed. Incident statuses are illegal on a maintenance and vice versa.
func TestMaintenanceTransitionTable(t *testing.T) {
	legal := [][2]string{
		{StatusScheduled, StatusInProgress},
		{StatusScheduled, StatusCompleted},
		{StatusInProgress, StatusCompleted},
	}
	for _, c := range legal {
		if err := CanTransition(c[0], c[1]); err != nil {
			t.Errorf("CanTransition(%s -> %s) = %v, want nil", c[0], c[1], err)
		}
	}
	illegal := [][2]string{
		{StatusCompleted, StatusScheduled},
		{StatusInProgress, StatusScheduled},
		{StatusScheduled, StatusInvestigating}, // incident status on maintenance track
		{StatusInvestigating, StatusScheduled}, // maintenance status on incident track
	}
	for _, c := range illegal {
		if err := CanTransition(c[0], c[1]); err == nil {
			t.Errorf("CanTransition(%s -> %s) accepted, want rejection", c[0], c[1])
		}
	}
}

// Component statuses accept exactly the five-word vocabulary.
func TestComponentStatusValidation(t *testing.T) {
	for _, ok := range []string{StatusOperational, StatusDegraded, StatusPartial, StatusMajor, StatusMaintenance} {
		if err := ValidComponentStatus(ok); err != nil {
			t.Errorf("ValidComponentStatus(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "Operational", "partial_outage", "down", "ok"} {
		if err := ValidComponentStatus(bad); !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("ValidComponentStatus(%q) = %v, want ErrInvalidStatus", bad, err)
		}
	}
}

// Incident impact vocabulary: none, minor, major, critical.
func TestImpactValidation(t *testing.T) {
	for _, ok := range []string{ImpactNone, ImpactMinor, ImpactMajor, ImpactCritical} {
		if err := ValidImpact(ok); err != nil {
			t.Errorf("ValidImpact(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "huge", "maintenance"} {
		if err := ValidImpact(bad); !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("ValidImpact(%q) = %v, want ErrInvalidStatus", bad, err)
		}
	}
}
