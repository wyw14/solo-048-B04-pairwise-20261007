package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"heatpulse/internal/recovery"
)

// recoveryErrorStatus classifies recovery validation errors: an unknown zone
// is 404, while a cross-zone or unknown station is a 400 from the caller.
func recoveryErrorStatus(err error) int {
	switch {
	case errors.Is(err, recovery.ErrUnknownZone):
		return http.StatusNotFound
	case errors.Is(err, recovery.ErrForeignStation):
		return http.StatusBadRequest
	default:
		return http.StatusConflict
	}
}

func (a *App) reconnectStation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Epoch uint64 `json:"epoch"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := a.Reconnector.Reconnect(chi.URLParam(r, "stationID"), input.Epoch)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) beginZoneRecovery(w http.ResponseWriter, r *http.Request) {
	zone, err := a.ZoneRecovery.Begin(chi.URLParam(r, "zoneID"))
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, zone)
}

func (a *App) connectRecoveryStation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		StationID string `json:"station_id"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	zone, err := a.ZoneRecovery.OnConnected(chi.URLParam(r, "zoneID"), input.StationID)
	if err != nil {
		writeError(w, recoveryErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"zone": zone, "readiness": a.ZoneRecovery.Readiness(zone.ID)})
}

func (a *App) recordRecoveryProofs(w http.ResponseWriter, r *http.Request) {
	var input struct {
		StationID    string  `json:"station_id"`
		ExchangerID  string  `json:"exchanger_id"`
		Revision     uint64  `json:"revision"`
		Outlet       float64 `json:"outlet_celsius"`
		PumpStage    int     `json:"pump_stage"`
		Differential float64 `json:"differential"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	zoneID := chi.URLParam(r, "zoneID")
	if _, err := a.ZoneRecovery.ValidateStation(zoneID, input.StationID); err != nil {
		writeError(w, recoveryErrorStatus(err), err)
		return
	}
	warmup, err := a.Warmup.Proof(input.StationID, input.ExchangerID, input.Revision, input.Outlet)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	pressure, err := a.Pressure.Record(input.StationID, input.Revision, input.PumpStage, input.Differential)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	zone, err := a.ZoneRecovery.Evaluate(zoneID)
	if err != nil {
		writeError(w, recoveryErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"warmup": warmup, "pressure": pressure, "zone": zone})
}
