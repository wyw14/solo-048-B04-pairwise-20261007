package recovery

import (
	"testing"

	"heatpulse/internal/exchanger"
	"heatpulse/internal/network"
	"heatpulse/internal/pump"
	"heatpulse/internal/station"
)

func newZoneRecoveryFixture() (*ZoneRecovery, *exchanger.WarmupService, *pump.PressureMonitor) {
	state := network.NewState([]network.Zone{
		{ID: "z1", Name: "Zone 1", Phase: network.PhaseActive},
		{ID: "z2", Name: "Zone 2", Phase: network.PhaseActive},
	})
	bindings := network.NewRegistry([]network.StationBinding{
		{StationID: "s1", ZoneID: "z1", Required: true},
		{StationID: "s2", ZoneID: "z2", Required: true},
	})
	stations := station.NewRegistry([]station.Station{
		{ID: "s1", Name: "S1", ZoneID: "z1", Online: true, Exchangers: map[string]station.ExchangerPhase{"A": station.ExchangerCarrying}},
		{ID: "s2", Name: "S2", ZoneID: "z2", Online: true, Exchangers: map[string]station.ExchangerPhase{"A": station.ExchangerCarrying}},
	})
	warmup := exchanger.NewWarmupService(stations)
	pressure := pump.NewPressureMonitor()
	return NewZoneRecovery(state, bindings, warmup, pressure), warmup, pressure
}

func TestOnConnectedRejectsUnknownStation(t *testing.T) {
	zr, _, _ := newZoneRecoveryFixture()
	if _, err := zr.Begin("z1"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := zr.OnConnected("z1", "ghost"); err == nil {
		t.Fatal("expected error for unknown station")
	}
	if readiness := zr.Readiness("z1"); readiness["s1"]["connected"] {
		t.Fatal("unknown station must not mark anything connected")
	}
	zone, err := zr.Evaluate("z1")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if zone.Phase != network.PhaseWarming {
		t.Fatalf("unknown station advanced zone phase to %s", zone.Phase)
	}
}

func TestOnConnectedRejectsCrossZoneStation(t *testing.T) {
	zr, warmup, pressure := newZoneRecoveryFixture()
	if _, err := zr.Begin("z1"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// Fully prove s2 (bound to z2) at z1's recovery revision: even with
	// satisfied external records, a cross-zone station must not connect.
	if _, err := warmup.Proof("s2", "A", 1, 40); err != nil {
		t.Fatalf("warmup proof: %v", err)
	}
	if _, err := pressure.Record("s2", 1, 2, 0.2); err != nil {
		t.Fatalf("pressure record: %v", err)
	}
	if _, err := zr.OnConnected("z1", "s2"); err == nil {
		t.Fatal("expected error for cross-zone station")
	}
	if readiness := zr.Readiness("z1"); readiness["s1"]["connected"] {
		t.Fatal("cross-zone station must not mark z1 stations connected")
	}
	zone, err := zr.Evaluate("z1")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if zone.Phase != network.PhaseWarming {
		t.Fatalf("cross-zone station advanced zone phase to %s", zone.Phase)
	}
}

func TestOnConnectedRejectsUnknownZone(t *testing.T) {
	zr, _, _ := newZoneRecoveryFixture()
	if _, err := zr.OnConnected("nowhere", "s1"); err == nil {
		t.Fatal("expected error for unknown zone")
	}
}

func TestOnConnectedAcceptsBoundStationAndActivates(t *testing.T) {
	zr, warmup, pressure := newZoneRecoveryFixture()
	if _, err := zr.Begin("z1"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := warmup.Proof("s1", "A", 1, 40); err != nil {
		t.Fatalf("warmup proof: %v", err)
	}
	if _, err := pressure.Record("s1", 1, 2, 0.2); err != nil {
		t.Fatalf("pressure record: %v", err)
	}
	zone, err := zr.OnConnected("z1", "s1")
	if err != nil {
		t.Fatalf("on connected: %v", err)
	}
	if zone.Phase != network.PhaseActive {
		t.Fatalf("expected zone active after bound station connected, got %s", zone.Phase)
	}
	if readiness := zr.Readiness("z1"); !readiness["s1"]["connected"] || !readiness["s1"]["warm"] || !readiness["s1"]["pressurized"] {
		t.Fatalf("readiness does not reflect bound station: %v", readiness["s1"])
	}
}
