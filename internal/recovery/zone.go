package recovery

import (
	"fmt"
	"sync"

	"heatpulse/internal/exchanger"
	"heatpulse/internal/network"
	"heatpulse/internal/pump"
)

type ZoneRecovery struct {
	state     *network.State
	registry  *network.Registry
	warmup    *exchanger.WarmupService
	pressure  *pump.PressureMonitor
	mu        sync.Mutex
	connected map[string]map[string]bool
}

func NewZoneRecovery(state *network.State, registry *network.Registry, warmup *exchanger.WarmupService, pressure *pump.PressureMonitor) *ZoneRecovery {
	return &ZoneRecovery{state: state, registry: registry, warmup: warmup, pressure: pressure, connected: make(map[string]map[string]bool)}
}

func (r *ZoneRecovery) Begin(zoneID string) (network.Zone, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connected[zoneID] = make(map[string]bool)
	return r.state.Update(zoneID, func(zone *network.Zone) error {
		zone.RecoveryRevision++
		zone.Phase = network.PhaseWarming
		return nil
	})
}

var (
	ErrUnknownZone    = fmt.Errorf("zone not found in recovery")
	ErrForeignStation = fmt.Errorf("station is not bound to this zone")
)

// belongsToZone reports whether stationID is a registered member of zoneID.
// Stations bound to another zone, or unknown to the network registry, never
// count toward a zone's recovery.
func (r *ZoneRecovery) belongsToZone(zoneID, stationID string) bool {
	boundZone, ok := r.registry.ZoneForStation(stationID)
	return ok && boundZone == zoneID
}

// ValidateStation ensures the zone exists and the station is a registered
// member of that zone. Stations belonging to another zone, or unknown to
// the network registry, are rejected so they cannot advance recovery state.
func (r *ZoneRecovery) ValidateStation(zoneID, stationID string) (network.Zone, error) {
	zone, ok := r.state.Get(zoneID)
	if !ok {
		return network.Zone{}, fmt.Errorf("%w: %s", ErrUnknownZone, zoneID)
	}
	if !r.belongsToZone(zoneID, stationID) {
		return zone, fmt.Errorf("%w: station %s", ErrForeignStation, stationID)
	}
	return zone, nil
}

func (r *ZoneRecovery) OnConnected(zoneID, stationID string) (network.Zone, error) {
	if _, err := r.ValidateStation(zoneID, stationID); err != nil {
		return network.Zone{}, err
	}
	// Membership is verified before touching the connected set, so a
	// foreign or unknown station can never be recorded as a connection.
	r.mu.Lock()
	if r.connected[zoneID] == nil {
		r.connected[zoneID] = make(map[string]bool)
	}
	r.connected[zoneID][stationID] = true
	r.mu.Unlock()
	return r.evaluate(zoneID)
}

func (r *ZoneRecovery) Evaluate(zoneID string) (network.Zone, error) {
	return r.evaluate(zoneID)
}

func (r *ZoneRecovery) evaluate(zoneID string) (network.Zone, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	zone, ok := r.state.Get(zoneID)
	if !ok {
		return network.Zone{}, fmt.Errorf("%w: %s", ErrUnknownZone, zoneID)
	}
	connected := r.connected[zoneID]
	required := r.registry.RequiredForZone(zoneID)
	if len(required) == 0 {
		// A zone with no required stations cannot be recovered through
		// station readiness; never auto-advance it.
		return zone, nil
	}
	for _, stationID := range required {
		// Defense in depth: only stations still bound to this zone may
		// advance readiness. A stale connected entry never satisfies a
		// required station even if bindings change mid-recovery.
		if !connected[stationID] || !r.belongsToZone(zoneID, stationID) || !r.warmup.Ready(stationID, zone.RecoveryRevision) || !r.pressure.Ready(stationID, zone.RecoveryRevision) {
			return zone, nil
		}
	}
	return r.state.Activate(zoneID, zone.RecoveryRevision)
}

func (r *ZoneRecovery) Readiness(zoneID string) map[string]map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	zone, _ := r.state.Get(zoneID)
	result := make(map[string]map[string]bool)
	for _, stationID := range r.registry.RequiredForZone(zoneID) {
		result[stationID] = map[string]bool{
			"connected":   r.connected[zoneID][stationID] && r.belongsToZone(zoneID, stationID),
			"warm":        r.warmup.Ready(stationID, zone.RecoveryRevision),
			"pressurized": r.pressure.Ready(stationID, zone.RecoveryRevision),
		}
	}
	return result
}
