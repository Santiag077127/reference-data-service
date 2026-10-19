package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestPoolStatsLogLineIsNumericOnlyAndContainsNoConnectionData(t *testing.T) {
	line := poolStatsLogLine(sql.DBStats{
		MaxOpenConnections: 0,
		OpenConnections:    12,
		InUse:              7,
		Idle:               5,
		WaitCount:          21,
		WaitDuration:       3456 * time.Millisecond,
		MaxIdleClosed:      2,
		MaxIdleTimeClosed:  3,
		MaxLifetimeClosed:  4,
	})
	want := "db_pool_stats max_open_connections=0 open_connections=12 in_use=7 idle=5 wait_count=21 wait_duration_ms=3456 max_idle_closed=2 max_idle_time_closed=3 max_lifetime_closed=4"
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
	for _, prohibited := range []string{"postgres://", "password", "Bearer", "Authorization", "host", "database", "SELECT"} {
		if strings.Contains(line, prohibited) {
			t.Fatalf("pool stats line leaked %q", prohibited)
		}
	}
}

func TestRunPoolStatsLoggerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		runPoolStatsLogger(ctx, staticDBStats{}, make(chan time.Time))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pool stats logger did not stop after cancellation")
	}
}

type staticDBStats struct{}

func (staticDBStats) Stats() sql.DBStats { return sql.DBStats{} }
