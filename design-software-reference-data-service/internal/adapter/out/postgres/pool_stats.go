package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

const defaultPoolStatsInterval = 5 * time.Second

type dbStatsProvider interface {
	Stats() sql.DBStats
}

// StartPoolStatsLogger records only aggregate database/sql pool counters. It
// uses the caller's existing *sql.DB and does not create or configure a pool.
func StartPoolStatsLogger(ctx context.Context, db dbStatsProvider, interval time.Duration) {
	if db == nil {
		return
	}
	if interval <= 0 {
		interval = defaultPoolStatsInterval
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		runPoolStatsLogger(ctx, db, ticker.C)
	}()
}

func runPoolStatsLogger(ctx context.Context, db dbStatsProvider, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			logPoolStats(db.Stats())
		}
	}
}

func logPoolStats(stats sql.DBStats) {
	log.Print(poolStatsLogLine(stats))
}

// poolStatsLogLine contains a closed set of numeric-only pool counters.
func poolStatsLogLine(stats sql.DBStats) string {
	return fmt.Sprintf(
		"db_pool_stats max_open_connections=%d open_connections=%d in_use=%d idle=%d wait_count=%d wait_duration_ms=%d max_idle_closed=%d max_idle_time_closed=%d max_lifetime_closed=%d",
		stats.MaxOpenConnections,
		stats.OpenConnections,
		stats.InUse,
		stats.Idle,
		stats.WaitCount,
		stats.WaitDuration.Milliseconds(),
		stats.MaxIdleClosed,
		stats.MaxIdleTimeClosed,
		stats.MaxLifetimeClosed,
	)
}
