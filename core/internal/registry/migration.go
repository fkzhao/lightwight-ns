package registry

import (
	"database/sql"
	"encoding/json"
)

type serviceConfig struct {
	HealthCheck HealthCheck      `json:"healthCheck"`
	Protection  ProtectionPolicy `json:"protection"`
}

type instanceConfig struct {
	Version   string            `json:"version,omitempty"`
	Weight    *int              `json:"weight,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Lifecycle string            `json:"lifecycle"`
}

func serviceJSON(service Service) string {
	data, _ := json.Marshal(serviceConfig{service.HealthCheck, service.Protection})
	return string(data)
}
func instanceJSON(instance Instance) string {
	data, _ := json.Marshal(instanceConfig{instance.Version, instance.Weight, instance.Metadata, instance.Lifecycle})
	return string(data)
}

// migrateSchema changes the old global name constraint to a scoped identity atomically.
func migrateSchema(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 2 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE services_v2 (id TEXT PRIMARY KEY, namespace TEXT NOT NULL COLLATE NOCASE DEFAULT 'default', service_group TEXT NOT NULL COLLATE NOCASE DEFAULT 'default', name TEXT NOT NULL COLLATE NOCASE, description TEXT NOT NULL, health_type TEXT NOT NULL, health_path TEXT NOT NULL, config_json TEXT NOT NULL DEFAULT '{}', UNIQUE(namespace,service_group,name))`,
		`CREATE TABLE instances_v2 (id TEXT PRIMARY KEY, service_id TEXT NOT NULL REFERENCES services_v2(id) ON DELETE CASCADE, address TEXT NOT NULL, port INTEGER NOT NULL, enabled INTEGER NOT NULL, source TEXT NOT NULL, token_hash TEXT NOT NULL, lease_expires_at INTEGER NOT NULL, config_json TEXT NOT NULL DEFAULT '{}', UNIQUE(address,port))`,
		`INSERT INTO services_v2 (id,name,description,health_type,health_path) SELECT id,name,description,health_type,health_path FROM services`,
		`INSERT INTO instances_v2 (id,service_id,address,port,enabled,source,token_hash,lease_expires_at) SELECT id,service_id,address,port,enabled,source,token_hash,lease_expires_at FROM instances`,
		`DROP TABLE instances`, `DROP TABLE services`,
		`ALTER TABLE services_v2 RENAME TO services`, `ALTER TABLE instances_v2 RENAME TO instances`,
		`CREATE INDEX instances_by_service ON instances(service_id)`,
		`CREATE UNIQUE INDEX instances_by_token ON instances(token_hash) WHERE source='sdk'`,
		`CREATE INDEX instances_by_expiry ON instances(lease_expires_at) WHERE source='sdk'`,
		`PRAGMA user_version=2`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
