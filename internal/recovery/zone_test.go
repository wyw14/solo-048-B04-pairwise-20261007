package recovery

import (
	"errors"
	"testing"

	"heatpulse/internal/exchanger"
	"heatpulse/internal/network"
	"heatpulse/internal/pump"
	"heatpulse/internal/station"
)

func newTestRecovery(t *testing.T) (*ZoneRecovery, *exchanger.WarmupService, *pump.PressureMonitor) {
	t.Helper()
	state := network.NewState([]network.Zone{
		{ID: "north", Phase: network.PhaseOffline},
		{ID: "west", Phase: network.PhaseOffline},
		{ID: "empty", Phase: network.PhaseOffline},
	})
	registry := network.NewRegistry([]network.StationBinding{
		{StationID: "xincheng", ZoneID: "north", Required: true},
		{StationID: "nanhu", ZoneID: "west", Required: true},
		{StationID: "xijiao", ZoneID: "west", Required: true},
	})
	stations := station.NewRegistry([]station.Station{
		{ID: "xincheng", Exchangers: map[string]station.ExchangerPhase{"A": station.ExchangerCarrying}},
		{ID: "nanhu", Exchangers: map[string]station.ExchangerPhase{"A": station.ExchangerCarrying}},
		{ID: "xijiao", Exchangers: map[string]station.ExchangerPhase{"A": station.ExchangerCarrying}},
	})
	warmup := exchanger.NewWarmupService(stations)
	pressure := pump.NewPressureMonitor()
	return NewZoneRecovery(state, registry, warmup, pressure), warmup, pressure
}

func TestOnConnectedRejectsCrossZoneStation(t *testing.T) {
	r, _, _ := newTestRecovery(t)
	if _, err := r.Begin("north"); err != nil {
		t.Fatalf("begin: %v", err)
	}

	// "nanhu" belongs to west, not north: the connection must be refused
	// before any state is recorded.
	if _, err := r.OnConnected("north", "nanhu"); !errors.Is(err, ErrForeignStation) {
		t.Fatalf("expected ErrForeignStation, got %v", err)
	}

	readiness := r.Readiness("north")
	if readiness["xincheng"]["connected"] {
		t.Fatal("xincheng must not be marked connected")
	}
	if _, leaked := readiness["nanhu"]; leaked {
		t.Fatal("cross-zone station must not appear in north readiness")
	}
}

func TestOnConnectedRejectsUnknownStation(t *testing.T) {
	r, _, _ := newTestRecovery(t)
	if _, err := r.Begin("north"); err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := r.OnConnected("north", "ghost-station"); !errors.Is(err, ErrForeignStation) {
		t.Fatalf("expected ErrForeignStation, got %v", err)
	}
	if r.Readiness("north")["ghost-station"] != nil {
		t.Fatal("unknown station must not appear in readiness")
	}
}

func TestOnConnectedRejectsUnknownZone(t *testing.T) {
	r, _, _ := newTestRecovery(t)
	if _, err := r.OnConnected("nowhere", "xincheng"); !errors.Is(err, ErrUnknownZone) {
		t.Fatalf("expected ErrUnknownZone, got %v", err)
	}
}

func TestEvaluateAdvancesOnlyForGenuineMembers(t *testing.T) {
	r, warmup, pressure := newTestRecovery(t)
	zone, err := r.Begin("west")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	revision := zone.RecoveryRevision

	// A foreign station posting ready proofs must never substitute for
	// west's own required stations (nanhu, xijiao), which never connected.
	r.mu.Lock()
	r.connected["west"]["xincheng"] = true // belongs to north
	r.mu.Unlock()

	if _, err := warmup.Proof("xincheng", "A", revision, 55); err != nil {
		t.Fatalf("warmup proof: %v", err)
	}
	if _, err := pressure.Record("xincheng", revision, 2, 0.3); err != nil {
		t.Fatalf("pressure record: %v", err)
	}

	zone, err = r.Evaluate("west")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if zone.Phase == network.PhaseActive {
		t.Fatal("zone must not activate on cross-zone station proofs")
	}
}

func TestEvaluateActivatesWhenAllMembersReady(t *testing.T) {
	r, warmup, pressure := newTestRecovery(t)
	zone, err := r.Begin("west")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	revision := zone.RecoveryRevision

	for _, id := range []string{"nanhu", "xijiao"} {
		if _, err := r.OnConnected("west", id); err != nil {
			t.Fatalf("connect %s: %v", id, err)
		}
		if _, err := warmup.Proof(id, "A", revision, 55); err != nil {
			t.Fatalf("warmup proof %s: %v", id, err)
		}
		if _, err := pressure.Record(id, revision, 2, 0.3); err != nil {
			t.Fatalf("pressure record %s: %v", id, err)
		}
	}

	zone, err = r.Evaluate("west")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if zone.Phase != network.PhaseActive {
		t.Fatalf("expected active phase, got %s", zone.Phase)
	}
}

func TestEvaluateNeverAdvancesZoneWithoutRequiredStations(t *testing.T) {
	r, _, _ := newTestRecovery(t)
	zone, err := r.Begin("empty")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	zone, err = r.Evaluate("empty")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if zone.Phase != network.PhaseWarming {
		t.Fatalf("zone with no required stations must stay warming, got %s", zone.Phase)
	}
}

func TestValidateStation(t *testing.T) {
	r, _, _ := newTestRecovery(t)
	if _, err := r.ValidateStation("north", "xincheng"); err != nil {
		t.Fatalf("genuine member rejected: %v", err)
	}
	if _, err := r.ValidateStation("north", "nanhu"); !errors.Is(err, ErrForeignStation) {
		t.Fatalf("cross-zone: expected ErrForeignStation, got %v", err)
	}
	if _, err := r.ValidateStation("north", "ghost"); !errors.Is(err, ErrForeignStation) {
		t.Fatalf("unknown station: expected ErrForeignStation, got %v", err)
	}
	if _, err := r.ValidateStation("nowhere", "xincheng"); !errors.Is(err, ErrUnknownZone) {
		t.Fatalf("unknown zone: expected ErrUnknownZone, got %v", err)
	}
}
