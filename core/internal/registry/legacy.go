package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// loadLegacySnapshot accepts both nested instances and the older machine table.
func loadLegacySnapshot(path string) (Snapshot, error) {
	snapshot := Snapshot{Services: []Service{}}
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	var disk struct {
		Version  uint64    `json:"version"`
		Services []Service `json:"services"`
		Machines []struct {
			ID      string `json:"id"`
			Address string `json:"address"`
		} `json:"machines"`
		Instances []struct {
			ID        string `json:"id"`
			ServiceID string `json:"serviceId"`
			MachineID string `json:"machineId"`
			Port      int    `json:"port"`
			Enabled   bool   `json:"enabled"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(encoded, &disk); err != nil {
		return Snapshot{}, fmt.Errorf("decode legacy registry %s: %w", path, err)
	}
	snapshot.Version = disk.Version
	if disk.Services != nil {
		snapshot.Services = disk.Services
	}
	machines := make(map[string]string, len(disk.Machines))
	for _, machine := range disk.Machines {
		machines[machine.ID] = machine.Address
	}
	for _, old := range disk.Instances {
		address, ok := machines[old.MachineID]
		if !ok {
			return Snapshot{}, fmt.Errorf("legacy instance %s references a missing machine", old.ID)
		}
		found := false
		for i := range snapshot.Services {
			if snapshot.Services[i].ID == old.ServiceID {
				snapshot.Services[i].Instances = append(snapshot.Services[i].Instances, Instance{
					ID: old.ID, Address: address, Port: old.Port, Enabled: old.Enabled,
				})
				found = true
				break
			}
		}
		if !found {
			return Snapshot{}, fmt.Errorf("legacy instance %s references a missing service", old.ID)
		}
	}
	for i := range snapshot.Services {
		service := &snapshot.Services[i]
		if service.HealthCheck.Type == "" {
			service.HealthCheck.Type = "tcp"
		}
		if service.Instances == nil {
			service.Instances = []Instance{}
		}
		for j := range service.Instances {
			service.Instances[j].Health = HealthUnknown
		}
	}
	return snapshot, nil
}
