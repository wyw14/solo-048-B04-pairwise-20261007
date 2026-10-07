package network

import (
	"sort"
	"sync"
)

type StationBinding struct {
	StationID string `json:"station_id"`
	ZoneID    string `json:"zone_id"`
	Required  bool   `json:"required"`
}

type Registry struct {
	mu       sync.RWMutex
	bindings map[string]StationBinding
}

func NewRegistry(bindings []StationBinding) *Registry {
	r := &Registry{bindings: make(map[string]StationBinding, len(bindings))}
	for _, binding := range bindings {
		r.bindings[binding.StationID] = binding
	}
	return r
}

func (r *Registry) Binding(stationID string) (StationBinding, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	binding, ok := r.bindings[stationID]
	return binding, ok
}

func (r *Registry) ForZone(zoneID string) []StationBinding {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]StationBinding, 0)
	for _, binding := range r.bindings {
		if binding.ZoneID == zoneID {
			result = append(result, binding)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StationID < result[j].StationID })
	return result
}

func (r *Registry) RequiredForZone(zoneID string) []string {
	bindings := r.ForZone(zoneID)
	ids := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Required {
			ids = append(ids, binding.StationID)
		}
	}
	return ids
}
