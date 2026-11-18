package registry

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func populatedStore(t testing.TB) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	err = store.Update(func(snapshot *Snapshot) error {
		for i := 0; i < 100; i++ {
			service := Service{ID: fmt.Sprintf("service-%d", i), Name: fmt.Sprintf("service-%d", i), HealthCheck: HealthCheck{Type: "tcp"}}
			for j := 0; j < 10; j++ {
				n := i*10 + j
				hash, err := registrationHash(fmt.Sprintf("%064x", n+1))
				if err != nil {
					return err
				}
				service.Instances = append(service.Instances, Instance{
					ID: fmt.Sprintf("instance-%d", n), Address: fmt.Sprintf("10.0.%d.%d", n/250, n%250+1), Port: 8080,
					Enabled: true, Source: "sdk", LeaseTokenHash: hash, LeaseExpiresAt: time.Now().Add(time.Minute).Unix(), Health: HealthUnknown,
				})
			}
			snapshot.Services = append(snapshot.Services, service)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestConcurrentRenewalsWith100ServicesAnd1000Instances(t *testing.T) {
	store := populatedStore(t)
	version := store.Snapshot().Version
	var beforeChanges int64
	if err := store.db.QueryRow("SELECT total_changes()").Scan(&beforeChanges); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var group sync.WaitGroup
	for worker := 0; worker < 20; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for i := worker; i < 1000; i += 20 {
				if _, err := store.RenewRegistration(fmt.Sprintf("instance-%d", i), fmt.Sprintf("%064x", i+1)); err != nil {
					t.Errorf("renew instance %d: %v", i, err)
				}
				if _, changed := store.SnapshotSince(version); changed {
					t.Error("lease renewal changed the agent revision")
				}
			}
		}(worker)
	}
	group.Wait()
	var afterChanges int64
	if err := store.db.QueryRow("SELECT total_changes()").Scan(&afterChanges); err != nil {
		t.Fatal(err)
	}
	if afterChanges-beforeChanges != 1000 {
		t.Fatalf("1000 renewals changed %d SQL rows", afterChanges-beforeChanges)
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM instances").Scan(&count); err != nil || count != 1000 {
		t.Fatalf("database instance count = %d, error = %v", count, err)
	}
	t.Logf("1000 persisted renewals with 20 clients: %s", time.Since(start))
}

func BenchmarkLeaseRenewal1000Instances(b *testing.B) {
	store := populatedStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := i % 1000
		// Alternate a real expiry change so fast repeated runs still exercise the WAL.
		store.mu.Lock()
		instance := &store.data.Services[n/10].Instances[n%10]
		err := store.renewLease(instance, time.Now().Add(time.Minute).Unix()+1+int64((i/1000)%2), false)
		store.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
}
