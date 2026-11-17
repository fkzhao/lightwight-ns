package health

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

type Checker struct {
	Store    *registry.Store
	Interval time.Duration
	Timeout  time.Duration
}

func (c *Checker) Run(ctx context.Context) {
	interval := c.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.RunOnce(ctx); err != nil {
			log.Printf("health check: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Checker) RunOnce(ctx context.Context) error {
	if err := c.Store.ExpireLeases(time.Now()); err != nil {
		return err
	}
	snapshot := c.Store.Snapshot()
	results := make(map[string]string)
	var resultsMu sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, service := range snapshot.Services {
		for _, instance := range service.Instances {
			if !instance.Enabled {
				continue
			}
			group.Add(1)
			slots <- struct{}{}
			go func(service registry.Service, instance registry.Instance) {
				defer group.Done()
				defer func() { <-slots }()
				status := registry.HealthUnhealthy
				if c.probe(ctx, service.HealthCheck, instance) {
					status = registry.HealthHealthy
				}
				resultsMu.Lock()
				results[instance.ID] = status
				resultsMu.Unlock()
			}(service, instance)
		}
	}
	group.Wait()
	return c.Store.ApplyHealth(snapshot.Version, results)
}

func (c *Checker) probe(ctx context.Context, check registry.HealthCheck, instance registry.Instance) bool {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	target := net.JoinHostPort(instance.Address, fmt.Sprint(instance.Port))
	if check.Type != "http" {
		connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
		if err != nil {
			return false
		}
		connection.Close()
		return true
	}
	path := check.Path
	if path == "" {
		path = "/health"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+target+path, nil)
	if err != nil {
		return false
	}
	client := http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}
