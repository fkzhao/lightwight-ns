package registry

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func featureStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestScopedMetadataDrainAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := store.SaveService("", Service{Name: "orders", Namespace: "prod", Group: "blue"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveService("", Service{Name: "orders"}); err != nil {
		t.Fatal(err)
	}
	weight := 75
	input := RegistrationInput{ServiceName: "orders", Namespace: "prod", Group: "blue", Address: "127.0.0.1", Port: 8081, Token: fmt.Sprintf("%064x", 1), Version: "v2", Weight: &weight, Metadata: map[string]string{"zone": "a"}}
	registered, err := store.RegisterInstance(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{registered.InstanceID: HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	query := ResolveQuery{Service: "orders", Namespace: "prod", Group: "blue", Version: "v2", Metadata: map[string]string{"zone": "a"}}
	result, err := Resolve(store.Snapshot(), query, time.Now())
	if err != nil || len(result.Endpoints) != 1 || result.Endpoints[0].Weight != 75 {
		t.Fatalf("resolve: %+v %v", result, err)
	}
	query.Version = "v1"
	result, err = Resolve(store.Snapshot(), query, time.Now())
	if err != nil || len(result.Endpoints) != 0 {
		t.Fatal("version filter was ignored")
	}
	query.Version = "v2"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	version := store.Snapshot().Version
	notification := make(chan Snapshot, 1)
	go func() { snapshot, _ := store.Watch(ctx, version); notification <- snapshot }()
	if err := store.DrainRegistration(registered.InstanceID, input.Token); err != nil {
		t.Fatal(err)
	}
	select {
	case snapshot := <-notification:
		if snapshot.Version <= version {
			t.Fatal("watch missed drain")
		}
	case <-ctx.Done():
		t.Fatal("watch blocked")
	}
	if _, err := store.RegisterInstance(input); err != nil {
		t.Fatal(err)
	}
	result, _ = Resolve(store.Snapshot(), query, time.Now())
	if len(result.Endpoints) != 0 {
		t.Fatal("renewal reactivated drained instance")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	got := reloaded.Snapshot().Services[0]
	if got.ID != service.ID || got.Namespace != "prod" || got.Group != "blue" || got.Instances[0].Lifecycle != "draining" || got.Instances[0].Metadata["zone"] != "a" || *got.Instances[0].Weight != 75 {
		t.Fatalf("metadata not durable: %+v", got)
	}
}

func TestHealthThresholdsAndRuntimeDetails(t *testing.T) {
	store := featureStore(t)
	service, err := store.SaveService("", Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := store.SaveInstance(service.ID, "", Instance{Address: "127.0.0.1", Port: 8080, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	observe := func(status string) {
		t.Helper()
		err := store.ApplyProbeObservations([]ProbeObservation{{InstanceID: instance.ID, Address: instance.Address, Port: instance.Port, HealthCheck: service.HealthCheck, Generation: instance.ProbeGeneration, Status: status, Error: "probe detail", DurationMillis: 12, CheckedAt: time.Now().UnixMilli()}})
		if err != nil {
			t.Fatal(err)
		}
	}
	observe(HealthHealthy)
	if store.Snapshot().Services[0].Instances[0].Health != HealthUnknown {
		t.Fatal("one success bypassed threshold")
	}
	observe(HealthHealthy)
	for i := 0; i < 2; i++ {
		observe(HealthUnhealthy)
		if store.Snapshot().Services[0].Instances[0].Health != HealthHealthy {
			t.Fatal("failure threshold applied too early")
		}
	}
	observe(HealthUnhealthy)
	got := store.Snapshot().Services[0].Instances[0]
	if got.Health != HealthUnhealthy || got.ConsecutiveFailures != 3 || got.DurationMillis != 12 || got.LastError != "probe detail" {
		t.Fatalf("incorrect runtime details: %+v", got)
	}
	observe(HealthHealthy)
	observe(HealthHealthy)
	if store.Snapshot().Services[0].Instances[0].Health != HealthHealthy {
		t.Fatal("did not recover")
	}
}

func TestProtectionIsBoundedAndStrictByDefault(t *testing.T) {
	store := featureStore(t)
	now := time.Now()
	store.startedAt = now.Add(-2 * time.Minute)
	service, err := store.SaveService("", Service{Name: "orders", Protection: ProtectionPolicy{Enabled: true, AllowStale: true}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		registered, err := store.RegisterInstance(RegistrationInput{ServiceName: "orders", Address: "127.0.0.1", Port: 8000 + i, Token: fmt.Sprintf("%064x", i+1)})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{registered.InstanceID: HealthHealthy}); err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	for i := range store.data.Services[0].Instances {
		store.data.Services[0].Instances[i].LastHealthyAt = now.UnixMilli()
	}
	store.mu.Unlock()
	expired := now.Add(31 * time.Second)
	if err := store.ExpireLeases(expired); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if !snapshot.Services[0].ProtectionStatus.Active || len(snapshot.Services[0].Instances) != 10 {
		t.Fatal("mass expiry was not protected")
	}
	result, _ := Resolve(snapshot, ResolveQuery{Service: "orders"}, expired)
	if len(result.Endpoints) != 0 {
		t.Fatal("strict discovery returned expired endpoints")
	}
	result, _ = Resolve(snapshot, ResolveQuery{Service: "orders", AllowStale: true}, expired)
	if len(result.Endpoints) != 10 || !result.Stale {
		t.Fatal("explicit historical fallback was not marked stale")
	}
	disabled := false
	if err := store.SetInstanceState(service.ID, snapshot.Services[0].Instances[0].ID, &disabled, false); err != nil {
		t.Fatal(err)
	}
	result, _ = Resolve(store.Snapshot(), ResolveQuery{Service: "orders", AllowStale: true}, expired)
	if len(result.Endpoints) != 9 {
		t.Fatal("fallback included administratively disabled endpoint")
	}
	result, _ = Resolve(snapshot, ResolveQuery{Service: "orders", AllowStale: true}, now.Add(61*time.Second))
	if len(result.Endpoints) != 0 {
		t.Fatal("historical fallback exceeded its lifetime")
	}
	if err := store.ExpireLeases(expired.Add(121 * time.Second)); err != nil {
		t.Fatal(err)
	}
	snapshot = store.Snapshot()
	if snapshot.Services[0].ProtectionStatus.Active || len(snapshot.Services[0].Instances) != 9 {
		t.Fatal("protection was not bounded or cleanup was not gradual")
	}
}

func TestMigratesExistingSQLiteSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureSQLite(db); err != nil {
		t.Fatal(err)
	}
	if err := initializeSQLite(db, path); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO services (id,name,description,health_type,health_path) VALUES ('old','orders','','tcp','')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := store.Snapshot().Services[0]; got.ID != "old" || got.Namespace != "default" || got.HealthCheck.SuccessThreshold != 2 {
		t.Fatalf("migration failed: %+v", got)
	}
	if _, err := store.SaveService("", Service{Name: "orders", Namespace: "prod"}); err != nil {
		t.Fatalf("global name constraint survived migration: %v", err)
	}
	if err := store.DeleteService("old"); err != nil {
		t.Fatal(err)
	}
}

func TestDrainedTokensCannotRecreateExpiredInstances(t *testing.T) {
	store := featureStore(t)
	if _, err := store.SaveService("", Service{Name: "orders"}); err != nil {
		t.Fatal(err)
	}
	input := RegistrationInput{ServiceName: "orders", Address: "127.0.0.1", Port: 8080, Token: fmt.Sprintf("%064x", 1)}
	result, err := store.RegisterInstance(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DrainRegistration(result.InstanceID, input.Token); err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireLeases(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterInstance(input); err == nil {
		t.Fatal("background renewal recreated a drained instance after expiry")
	}
	if len(store.Snapshot().Services[0].Instances) != 0 {
		t.Fatal("retired token created a new endpoint")
	}
}

func TestGradualCleanupNeverServesExpiredLeases(t *testing.T) {
	store := featureStore(t)
	if _, err := store.SaveService("", Service{Name: "orders", Protection: ProtectionPolicy{Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		result, err := store.RegisterInstance(RegistrationInput{ServiceName: "orders", Address: "127.0.0.1", Port: 8000 + i, Token: fmt.Sprintf("%064x", i+1)})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ApplyHealth(store.Snapshot().Version, map[string]string{result.InstanceID: HealthHealthy}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().Add(31 * time.Second)
	if err := store.ExpireLeases(now); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if snapshot.Services[0].ProtectionStatus.Active || len(snapshot.Services[0].Instances) != 9 {
		t.Fatal("expected gradual cleanup during warmup")
	}
	result, err := Resolve(snapshot, ResolveQuery{Service: "orders"}, now)
	if err != nil || len(result.Endpoints) != 0 {
		t.Fatal("gradual cleanup left an expired endpoint discoverable")
	}
}

func TestLegacySnapshotsExcludeOtherScopes(t *testing.T) {
	store := featureStore(t)
	for _, namespace := range []string{"default", "prod"} {
		if _, err := store.SaveService("", Service{Name: "orders", Namespace: namespace}); err != nil {
			t.Fatal(err)
		}
	}
	var reply SnapshotReply
	if err := (&RPCServer{Store: store}).GetSnapshot(SnapshotRequest{}, &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Snapshot.Services) != 1 || reply.Snapshot.Services[0].Namespace != "default" {
		t.Fatal("legacy Agent could read a non-default namespace")
	}
}

func TestMetadataDoesNotExposeMutableState(t *testing.T) {
	store := featureStore(t)
	service, err := store.SaveService("", Service{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	weight := 25
	input := Instance{Address: "127.0.0.1", Port: 8080, Enabled: true, Weight: &weight, Metadata: map[string]string{"zone": "a"}}
	instance, err := store.SaveInstance(service.ID, "", input)
	if err != nil {
		t.Fatal(err)
	}
	input.Metadata["zone"] = "input-mutated"
	weight = 999
	instance.Metadata["zone"] = "output-mutated"
	*instance.Weight = 888
	if got := store.Snapshot().Services[0].Instances[0]; got.Metadata["zone"] != "a" || *got.Weight != 25 {
		t.Fatal("instance metadata leaked mutable state")
	}
	updated, err := store.SaveService(service.ID, service)
	if err != nil {
		t.Fatal(err)
	}
	updated.Instances[0].Metadata["zone"] = "service-mutated"
	if store.Snapshot().Services[0].Instances[0].Metadata["zone"] != "a" {
		t.Fatal("service update leaked mutable state")
	}
}
