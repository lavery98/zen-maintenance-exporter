package zen

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Fetcher provides the data file.
type Fetcher interface {
	Fetch(ctx context.Context) (Feed, error)
}

// Snapshot is the exporter's current view of Zen's data.
type Snapshot struct {
	Maintenance []Maintenance
	Alerts      int
	Skipped     int

	Up           bool
	LastSuccess  time.Time
	LastDuration time.Duration
	Polls        uint64
	Failures     uint64
}

// Poller refreshes a Snapshot in the background.
type Poller struct {
	fetcher  Fetcher
	services []string
	filter   Filter
	interval time.Duration
	logger   *slog.Logger

	mu       sync.RWMutex
	snapshot Snapshot
}

func NewPoller(fetcher Fetcher, services []string, filter Filter, interval time.Duration, logger *slog.Logger) *Poller {
	return &Poller{
		fetcher:  fetcher,
		services: services,
		filter:   filter,
		interval: interval,
		logger:   logger,
	}
}

func (p *Poller) Run(ctx context.Context) {
	p.poll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshot
}

func (p *Poller) poll(ctx context.Context) {
	started := time.Now()
	maintenance, alerts, skipped, err := p.load(ctx)
	duration := time.Since(started)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.snapshot.Polls++
	p.snapshot.LastDuration = duration
	if err != nil {
		p.snapshot.Up = false
		p.snapshot.Failures++
		p.logger.Error("Poll failed, keeping the last good data", "err", err)
		return
	}

	p.snapshot.Up = true
	p.snapshot.LastSuccess = time.Now()
	p.snapshot.Maintenance = maintenance
	p.snapshot.Alerts = alerts
	p.snapshot.Skipped = skipped
	p.logger.Debug("Poll succeeded", "alerts", alerts, "relevant", len(maintenance), "skipped", skipped)
}

func (p *Poller) load(ctx context.Context) ([]Maintenance, int, int, error) {
	feed, err := p.fetcher.Fetch(ctx)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("fetching data: %w", err)
	}

	var (
		maintenance []Maintenance
		total       int
		skipped     int
	)
	for _, service := range p.services {
		alerts, err := feed.Alerts(service)
		if err != nil {
			return nil, 0, 0, err
		}
		selected, serviceSkipped := p.filter.Select(service, alerts)
		maintenance = append(maintenance, selected...)
		total += len(alerts)
		skipped += serviceSkipped
	}

	slices.SortStableFunc(maintenance, func(a, b Maintenance) int {
		return a.Start.Compare(b.Start)
	})
	return maintenance, total, skipped, nil
}
