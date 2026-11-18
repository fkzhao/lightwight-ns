// Package registration registers a running instance with the registry center.
package registration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"
)

const DefaultCenterURL = "http://127.0.0.1:8080"

type Client struct {
	baseURL string
	http    *http.Client
}

type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string { return fmt.Sprintf("registration API %d: %s", e.Status, e.Code) }

type registrationInput struct {
	ServiceName string `json:"serviceName"`
	Options
	Address string `json:"address"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
}

type registrationResult struct {
	InstanceID      string `json:"instanceId"`
	LeaseTTLSeconds int    `json:"leaseTtlSeconds"`
}

func NewClient(centerURL string) *Client {
	if centerURL == "" {
		centerURL = DefaultCenterURL
	}
	return &Client{baseURL: strings.TrimRight(centerURL, "/"), http: &http.Client{Timeout: 3 * time.Second}}
}

func (c *Client) request(ctx context.Context, method, path string, body any, result any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Code string `json:"code"`
		}
		json.NewDecoder(response.Body).Decode(&problem)
		return &APIError{Status: response.StatusCode, Code: problem.Code}
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(result)
}

type Lease struct {
	client  *Client
	input   registrationInput
	mu      sync.RWMutex
	id      string
	err     error
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	closeMu sync.Mutex
	closed  bool
}

// Register starts automatic lease renewal until Close is called.
func (c *Client) Register(ctx context.Context, serviceName, address string, port int) (*Lease, error) {
	return c.RegisterWithOptions(ctx, serviceName, address, port, Options{})
}

func (c *Client) RegisterWithOptions(ctx context.Context, serviceName, address string, port int, options Options) (*Lease, error) {
	options.Metadata = maps.Clone(options.Metadata)
	if options.Weight != nil {
		weight := *options.Weight
		options.Weight = &weight
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	input := registrationInput{ServiceName: serviceName, Address: address, Port: port, Token: hex.EncodeToString(secret), Options: options}
	var result registrationResult
	if err := c.request(ctx, http.MethodPost, "/api/registrations", input, &result); err != nil {
		return nil, err
	}
	lease := &Lease{client: c, input: input, id: result.InstanceID, stop: make(chan struct{}), done: make(chan struct{})}
	interval := time.Duration(result.LeaseTTLSeconds) * time.Second / 3
	if interval <= 0 {
		interval = 10 * time.Second
	}
	go lease.renewLoop(interval)
	return lease, nil
}

func (l *Lease) renewLoop(interval time.Duration) {
	defer close(l.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			var result registrationResult
			err := l.client.request(ctx, http.MethodPost, "/api/registrations", l.input, &result)
			cancel()
			l.mu.Lock()
			l.err = err
			if err == nil {
				l.id = result.InstanceID
			}
			l.mu.Unlock()
		}
	}
}

func (l *Lease) InstanceID() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.id
}

func (l *Lease) LastRenewalError() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.err
}

// Close stops renewal and deregisters the instance.
func (l *Lease) Close(ctx context.Context) error {
	l.closeMu.Lock()
	defer l.closeMu.Unlock()
	if l.closed {
		return nil
	}
	l.once.Do(func() { close(l.stop) })
	select {
	case <-l.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := l.client.request(ctx, http.MethodDelete, "/api/registrations/"+l.InstanceID(), map[string]string{"token": l.input.Token}, nil); err != nil {
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.Status != http.StatusNotFound {
			return err
		}
	}
	l.closed = true
	return nil
}

// Options are immutable registration metadata for this lease.
type Options struct {
	Namespace string            `json:"namespace,omitempty"`
	Group     string            `json:"group,omitempty"`
	Version   string            `json:"version,omitempty"`
	Weight    *int              `json:"weight,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Drain removes the instance from discovery while renewal continues.
func (l *Lease) Drain(ctx context.Context) error {
	return l.client.request(ctx, http.MethodPost, "/api/registrations/"+l.InstanceID()+"/drain", map[string]string{"token": l.input.Token}, nil)
}
