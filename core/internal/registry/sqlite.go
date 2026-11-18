package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// NewStore opens a single-center SQLite database and imports a sibling JSON snapshot once.
func NewStore(path string) (*Store, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return nil, fmt.Errorf("SQLite database path must differ from the legacy JSON path; use a .db file")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := dsn.Query()
	for _, pragma := range []string{"synchronous(FULL)", "foreign_keys(ON)", "busy_timeout(5000)"} {
		query.Add("_pragma", pragma)
	}
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := configureSQLite(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := initializeSQLite(db, path); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	snapshot, err := loadSQLiteSnapshot(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Restarted health observations are unknown, so agents need a new revision.
	snapshot.Version++
	if _, err := db.Exec("UPDATE metadata SET value = ? WHERE key = 'version'", snapshot.Version); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, data: snapshot, changed: make(chan struct{}), startedAt: time.Now(), beats: make(map[string]time.Time), protections: make(map[string]ProtectionStatus)}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func configureSQLite(db *sql.DB) error {
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return err
	}
	if journalMode != "wal" {
		return fmt.Errorf("SQLite WAL mode unavailable: %s", journalMode)
	}
	for _, statement := range []string{
		"PRAGMA synchronous = FULL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func initializeSQLite(db *sql.DB, path string) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS retired_registrations (token_hash TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS services (
			id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			description TEXT NOT NULL, health_type TEXT NOT NULL, health_path TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS instances (
			id TEXT PRIMARY KEY, service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
			address TEXT NOT NULL, port INTEGER NOT NULL, enabled INTEGER NOT NULL,
			source TEXT NOT NULL, token_hash TEXT NOT NULL, lease_expires_at INTEGER NOT NULL,
			UNIQUE(address, port)
		)`,
		`CREATE INDEX IF NOT EXISTS instances_by_service ON instances(service_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS instances_by_token ON instances(token_hash) WHERE source = 'sdk'`,
		`CREATE INDEX IF NOT EXISTS instances_by_expiry ON instances(lease_expires_at) WHERE source = 'sdk'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var initialized int
	err = tx.QueryRow("SELECT value FROM metadata WHERE key = 'initialized'").Scan(&initialized)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	legacyPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
	legacy, err := loadLegacySnapshot(legacyPath)
	if err != nil {
		return err
	}
	for _, service := range legacy.Services {
		if _, err := tx.Exec("INSERT INTO services (id, name, description, health_type, health_path) VALUES (?, ?, ?, ?, ?)", service.ID, service.Name, service.Description, service.HealthCheck.Type, service.HealthCheck.Path); err != nil {
			return fmt.Errorf("import service %s: %w", service.Name, err)
		}
		for _, instance := range service.Instances {
			if _, err := tx.Exec("INSERT INTO instances (id, service_id, address, port, enabled, source, token_hash, lease_expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", instance.ID, service.ID, instance.Address, instance.Port, instance.Enabled, instance.Source, instance.LeaseTokenHash, instance.LeaseExpiresAt); err != nil {
				return fmt.Errorf("import instance %s: %w", instance.ID, err)
			}
		}
	}
	if _, err := tx.Exec("INSERT INTO metadata (key, value) VALUES ('version', ?), ('initialized', 1)", legacy.Version); err != nil {
		return err
	}
	return tx.Commit()
}

func loadSQLiteSnapshot(db *sql.DB) (Snapshot, error) {
	snapshot := Snapshot{Services: []Service{}}
	if err := db.QueryRow("SELECT value FROM metadata WHERE key = 'version'").Scan(&snapshot.Version); err != nil {
		return Snapshot{}, err
	}
	rows, err := db.Query("SELECT id, name, description, health_type, health_path, namespace, service_group, config_json FROM services ORDER BY rowid")
	if err != nil {
		return Snapshot{}, err
	}
	byID := make(map[string]int)
	for rows.Next() {
		service := Service{Instances: []Instance{}}
		var config string
		if err := rows.Scan(&service.ID, &service.Name, &service.Description, &service.HealthCheck.Type, &service.HealthCheck.Path, &service.Namespace, &service.Group, &config); err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		if config != "{}" {
			var parsed serviceConfig
			if err := json.Unmarshal([]byte(config), &parsed); err != nil {
				rows.Close()
				return Snapshot{}, err
			}
			service.HealthCheck = parsed.HealthCheck
			service.Protection = parsed.Protection
		}
		var policyErr error
		service.HealthCheck, policyErr = normalizeHealthCheck(service.HealthCheck)
		if policyErr != nil {
			rows.Close()
			return Snapshot{}, fmt.Errorf("service %s health policy: %w", service.ID, policyErr)
		}
		service.Protection, policyErr = normalizeProtection(service.Protection)
		if policyErr != nil {
			rows.Close()
			return Snapshot{}, fmt.Errorf("service %s protection policy: %w", service.ID, policyErr)
		}
		byID[service.ID] = len(snapshot.Services)
		snapshot.Services = append(snapshot.Services, service)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Snapshot{}, err
	}
	rows.Close()
	rows, err = db.Query("SELECT id, service_id, address, port, enabled, source, token_hash, lease_expires_at, config_json FROM instances ORDER BY rowid")
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var instance Instance
		var serviceID, config string
		var enabled int
		if err := rows.Scan(&instance.ID, &serviceID, &instance.Address, &instance.Port, &enabled, &instance.Source, &instance.LeaseTokenHash, &instance.LeaseExpiresAt, &config); err != nil {
			return Snapshot{}, err
		}
		var parsed instanceConfig
		if err := json.Unmarshal([]byte(config), &parsed); err != nil {
			return Snapshot{}, err
		}
		instance.Version = parsed.Version
		instance.Weight = parsed.Weight
		instance.Metadata = parsed.Metadata
		instance.Lifecycle = parsed.Lifecycle
		if err := normalizeInstance(&instance); err != nil {
			return Snapshot{}, err
		}
		instance.Enabled = enabled != 0
		instance.Health = HealthUnknown
		instance.ProbeGeneration = 1
		index, ok := byID[serviceID]
		if !ok {
			return Snapshot{}, fmt.Errorf("instance %s references unknown service", instance.ID)
		}
		snapshot.Services[index].Instances = append(snapshot.Services[index].Instances, instance)
	}
	return snapshot, rows.Err()
}

type persistedInstance struct {
	Instance
	ServiceID string
}

// commit writes only changed rows, then publishes the new in-memory snapshot.
// The caller holds s.mu and assigns s.data only after commit succeeds.
func (s *Store) commit(next Snapshot) error { return s.commitWithRetired(next, false) }

func (s *Store) commitWithRetired(next Snapshot, retireRemoved bool) error {
	oldServices := make(map[string]Service, len(s.data.Services))
	newServices := make(map[string]Service, len(next.Services))
	oldInstances := make(map[string]persistedInstance)
	newInstances := make(map[string]persistedInstance)
	for _, service := range s.data.Services {
		oldServices[service.ID] = service
		for _, instance := range service.Instances {
			oldInstances[instance.ID] = persistedInstance{Instance: instance, ServiceID: service.ID}
		}
	}
	for _, service := range next.Services {
		newServices[service.ID] = service
		for _, instance := range service.Instances {
			newInstances[instance.ID] = persistedInstance{Instance: instance, ServiceID: service.ID}
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id := range oldInstances {
		if _, ok := newInstances[id]; !ok {
			old := oldInstances[id]
			if old.Source == "sdk" && (retireRemoved || old.Lifecycle == "draining") {
				if _, err := tx.Exec("INSERT OR IGNORE INTO retired_registrations (token_hash) VALUES (?)", old.LeaseTokenHash); err != nil {
					return err
				}
			}
			if _, err := tx.Exec("DELETE FROM instances WHERE id = ?", id); err != nil {
				return err
			}
		}
	}
	for id := range oldServices {
		if _, ok := newServices[id]; !ok {
			if _, err := tx.Exec("DELETE FROM services WHERE id = ?", id); err != nil {
				return err
			}
		}
	}
	for _, service := range next.Services {
		old, exists := oldServices[service.ID]
		if !exists {
			if _, err := tx.Exec("INSERT INTO services (id, name, description, health_type, health_path, namespace, service_group, config_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", service.ID, service.Name, service.Description, service.HealthCheck.Type, service.HealthCheck.Path, scope(service.Namespace), scope(service.Group), serviceJSON(service)); err != nil {
				return err
			}
		} else if old.Name != service.Name || old.Description != service.Description || old.HealthCheck != service.HealthCheck || old.Namespace != service.Namespace || old.Group != service.Group || old.Protection != service.Protection {
			if _, err := tx.Exec("UPDATE services SET name = ?, description = ?, health_type = ?, health_path = ?, namespace = ?, service_group = ?, config_json = ? WHERE id = ?", service.Name, service.Description, service.HealthCheck.Type, service.HealthCheck.Path, scope(service.Namespace), scope(service.Group), serviceJSON(service), service.ID); err != nil {
				return err
			}
		}
		for _, instance := range service.Instances {
			old, exists := oldInstances[instance.ID]
			if !exists {
				if _, err := tx.Exec("INSERT INTO instances (id, service_id, address, port, enabled, source, token_hash, lease_expires_at, config_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", instance.ID, service.ID, instance.Address, instance.Port, instance.Enabled, instance.Source, instance.LeaseTokenHash, instance.LeaseExpiresAt, instanceJSON(instance)); err != nil {
					return err
				}
			} else if old.ServiceID != service.ID || old.Address != instance.Address || old.Port != instance.Port || old.Enabled != instance.Enabled || old.Source != instance.Source || old.LeaseTokenHash != instance.LeaseTokenHash || instanceJSON(old.Instance) != instanceJSON(instance) {
				if _, err := tx.Exec("UPDATE instances SET service_id = ?, address = ?, port = ?, enabled = ?, source = ?, token_hash = ?, lease_expires_at = ?, config_json = ? WHERE id = ?", service.ID, instance.Address, instance.Port, instance.Enabled, instance.Source, instance.LeaseTokenHash, instance.LeaseExpiresAt, instanceJSON(instance), instance.ID); err != nil {
					return err
				}
			} else if old.LeaseExpiresAt != instance.LeaseExpiresAt {
				if _, err := tx.Exec("UPDATE instances SET lease_expires_at = ? WHERE id = ?", instance.LeaseExpiresAt, instance.ID); err != nil {
					return err
				}
			}
		}
	}
	if next.Version != s.data.Version {
		if _, err := tx.Exec("UPDATE metadata SET value = ? WHERE key = 'version'", next.Version); err != nil {
			return err
		}
	}
	return tx.Commit()
}
