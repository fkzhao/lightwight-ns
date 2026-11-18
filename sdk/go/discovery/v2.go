package discovery

import (
	"context"
	"errors"
	"math/rand/v2"
)

type Query struct {
	Service    string            `json:"service"`
	Namespace  string            `json:"namespace,omitempty"`
	Group      string            `json:"group,omitempty"`
	Version    string            `json:"version,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	AllowStale bool              `json:"allowStale,omitempty"`
}

type Instance struct {
	InstanceID string            `json:"instanceId"`
	Address    string            `json:"address"`
	Port       int               `json:"port"`
	Version    string            `json:"version,omitempty"`
	Weight     int               `json:"weight"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Stale      bool              `json:"stale"`
}

type Result struct {
	RegistryVersion uint64     `json:"registryVersion"`
	Endpoints       []Instance `json:"endpoints"`
	Stale           bool       `json:"stale"`
}

func (c *Client) ResolveV2(ctx context.Context, query Query) (Result, error) {
	var result Result
	err := c.call(ctx, "discovery.resolveV2", query, &result)
	return result, err
}

// PickWeighted chooses one eligible endpoint from the caller's current result.
func PickWeighted(endpoints []Instance) (Instance, error) {
	total := 0
	for _, endpoint := range endpoints {
		if endpoint.Weight > 0 {
			total += endpoint.Weight
		}
	}
	if total == 0 {
		return Instance{}, errors.New("no eligible endpoints")
	}
	choice := rand.IntN(total)
	for _, endpoint := range endpoints {
		if endpoint.Weight > 0 {
			choice -= endpoint.Weight
			if choice < 0 {
				return endpoint, nil
			}
		}
	}
	return Instance{}, errors.New("no eligible endpoints")
}
