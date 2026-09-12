package retention

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Daemon periodically triggers data retention and media purge routines across all workspaces.
type Daemon struct {
	purger   *Purger
	interval time.Duration
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(purger *Purger, interval time.Duration) *Daemon {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	return &Daemon{
		purger:   purger,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// SetInterval overrides the tick interval (useful for tests).
func (d *Daemon) SetInterval(interval time.Duration) {
	if interval > 0 {
		d.interval = interval
	}
}

// Run starts the daemon loop. It performs an initial purge run immediately, then on every ticker tick.
func (d *Daemon) Run(ctx context.Context) {
	slog.Info("retention daemon started", "interval", d.interval)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	// Initial purge run on startup
	d.runPurge(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("retention daemon stopping via context cancellation")
			return
		case <-d.stopCh:
			slog.Info("retention daemon stopping via stop signal")
			return
		case <-ticker.C:
			d.runPurge(ctx)
		}
	}
}

// Stop signals the daemon to stop.
func (d *Daemon) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
	})
}

func (d *Daemon) runPurge(ctx context.Context) {
	if d.purger == nil {
		return
	}
	slog.Debug("retention daemon running purge cycle")
	if _, err := d.purger.PurgeAll(ctx); err != nil {
		slog.Error("retention daemon purge cycle error", "error", err)
	}
}
