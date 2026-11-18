package health

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fkzhao/lightwight-ns/core/internal/registry"
)

type Checker struct {
	Store       *registry.Store
	Interval    time.Duration
	Timeout     time.Duration
	Concurrency int
}

type probeTask struct {
	check    registry.HealthCheck
	instance registry.Instance
}

func (c *Checker) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	interval := c.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	workers := c.concurrency()
	tasks := make(chan probeTask)
	results := make(chan registry.ProbeObservation, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case current := <-tasks:
					observation := c.observe(ctx, current.check, current.instance)
					select {
					case results <- observation:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	defer func() { cancel(); group.Wait() }()
	pending := make(map[string]bool)
	nextCheck := make(map[string]time.Time)
	var targets []probeTask
	var version uint64
	refresh := func() {
		snapshot, changed := c.Store.SnapshotSince(version)
		if !changed {
			return
		}
		version = snapshot.Version
		targets = nil
		present := make(map[string]bool)
		for _, service := range snapshot.Services {
			for _, instance := range service.Instances {
				if instance.Enabled {
					targets = append(targets, probeTask{service.HealthCheck, instance})
					present[instance.ID] = true
				}
			}
		}
		for id := range nextCheck {
			if !present[id] {
				delete(nextCheck, id)
			}
		}
	}
	var batch []registry.ProbeObservation
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := c.Store.ApplyProbeObservations(batch); err != nil {
			log.Printf("health check: %v", err)
			return
		}
		batch = nil
	}
	refreshInterval := min(interval, 200*time.Millisecond)
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	expiryTicker := time.NewTicker(min(interval, time.Second))
	defer expiryTicker.Stop()
	refresh()
	cursor := 0
	for {
		var output chan probeTask
		var candidate probeTask
		for checked := 0; checked < len(targets); checked++ {
			index := (cursor + checked) % len(targets)
			current := targets[index]
			if !pending[current.instance.ID] && !time.Now().Before(nextCheck[current.instance.ID]) {
				candidate = current
				output = tasks
				cursor = index
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case output <- candidate:
			pending[candidate.instance.ID] = true
			cursor++
		case observation := <-results:
			delete(pending, observation.InstanceID)
			delay := interval
			if observation.HealthCheck.IntervalMillis > 0 {
				delay = time.Duration(observation.HealthCheck.IntervalMillis) * time.Millisecond
			}
			delay = time.Duration(float64(delay) * (.9 + rand.Float64()*.2))
			nextCheck[observation.InstanceID] = time.Now().Add(delay)
			batch = append(batch, observation)
			if len(batch) >= 64 {
				flush()
			}
		case <-ticker.C:
			flush()
			refresh()
		case <-expiryTicker.C:
			if err := c.Store.ExpireLeases(time.Now()); err != nil {
				log.Printf("expire leases: %v", err)
			}
		}
	}
}

func (c *Checker) observe(ctx context.Context, check registry.HealthCheck, instance registry.Instance) registry.ProbeObservation {
	started := time.Now()
	err := c.probeError(ctx, check, instance)
	status := registry.HealthHealthy
	message := ""
	if err != nil {
		status = registry.HealthUnhealthy
		message = err.Error()
	}
	return registry.ProbeObservation{InstanceID: instance.ID, Address: instance.Address, Port: instance.Port, HealthCheck: check, Status: status, Generation: instance.ProbeGeneration, Error: message, CheckedAt: time.Now().UnixMilli(), DurationMillis: time.Since(started).Milliseconds()}
}

func (c *Checker) concurrency() int {
	if c.Concurrency > 0 {
		return c.Concurrency
	}
	return 128
}

func (c *Checker) RunOnce(ctx context.Context) error {
	if err := c.Store.ExpireLeases(time.Now()); err != nil {
		return err
	}
	snapshot := c.Store.Snapshot()
	var results []registry.ProbeObservation
	var resultsMu sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, c.concurrency())
	for _, service := range snapshot.Services {
		for _, instance := range service.Instances {
			if !instance.Enabled {
				continue
			}
			group.Add(1)
			slots <- struct{}{}
			go func(check registry.HealthCheck, instance registry.Instance) {
				defer group.Done()
				defer func() { <-slots }()
				observation := c.observe(ctx, check, instance)
				resultsMu.Lock()
				results = append(results, observation)
				resultsMu.Unlock()
			}(service.HealthCheck, instance)
		}
	}
	group.Wait()
	return c.Store.ApplyProbeObservations(results)
}

func (c *Checker) probe(ctx context.Context, check registry.HealthCheck, instance registry.Instance) bool {
	return c.probeError(ctx, check, instance) == nil
}

func (c *Checker) probeError(ctx context.Context, check registry.HealthCheck, instance registry.Instance) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if check.TimeoutMillis > 0 {
		timeout = time.Duration(check.TimeoutMillis) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	target := net.JoinHostPort(instance.Address, fmt.Sprint(instance.Port))
	if check.Type == "grpc" {
		var transport credentials.TransportCredentials = insecure.NewCredentials()
		if check.TLS {
			config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: check.ServerName}
			if check.CAPEM != "" {
				roots := x509.NewCertPool()
				if !roots.AppendCertsFromPEM([]byte(check.CAPEM)) {
					return errors.New("invalid health check CA certificate")
				}
				config.RootCAs = roots
			}
			transport = credentials.NewTLS(config)
		}
		connection, err := grpc.NewClient("passthrough:///"+target, grpc.WithTransportCredentials(transport))
		if err != nil {
			return err
		}
		defer connection.Close()
		reply, err := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{Service: check.GRPCService})
		if err != nil {
			return err
		}
		if reply.Status != healthpb.HealthCheckResponse_SERVING {
			return fmt.Errorf("gRPC health: %s", reply.Status.String())
		}
		return nil
	}
	if check.Type != "http" {
		connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
		if err != nil {
			return err
		}
		connection.Close()
		return nil
	}
	path := check.Path
	if path == "" {
		path = "/health"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+target+path, nil)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP health: %d", response.StatusCode)
	}
	return nil
}
