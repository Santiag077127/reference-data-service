package main

import (
	"database/sql"
	"testing"
	"time"
)

func TestDefaultDatabasePoolConfig(t *testing.T) {
	if defaultDatabasePoolConfig.maxOpenConns != 40 {
		t.Fatalf("maxOpenConns=%d, want 40", defaultDatabasePoolConfig.maxOpenConns)
	}
	if defaultDatabasePoolConfig.maxIdleConns != 10 {
		t.Fatalf("maxIdleConns=%d, want 10", defaultDatabasePoolConfig.maxIdleConns)
	}
	if defaultDatabasePoolConfig.maxConnLifetime != 30*time.Minute {
		t.Fatalf("maxConnLifetime=%s, want 30m", defaultDatabasePoolConfig.maxConnLifetime)
	}
	if defaultDatabasePoolConfig.maxConnIdleTime != 5*time.Minute {
		t.Fatalf("maxConnIdleTime=%s, want 5m", defaultDatabasePoolConfig.maxConnIdleTime)
	}
}

func TestConfigureDatabasePoolAppliesConnectionLimits(t *testing.T) {
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	configureDatabasePool(db, defaultDatabasePoolConfig)
	stats := db.Stats()
	if stats.MaxOpenConnections != defaultDatabasePoolConfig.maxOpenConns {
		t.Fatalf("MaxOpenConnections=%d, want %d", stats.MaxOpenConnections, defaultDatabasePoolConfig.maxOpenConns)
	}
}
