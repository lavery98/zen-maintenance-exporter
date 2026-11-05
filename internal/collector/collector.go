package collector

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lavery98/zen-maintenance-exporter/internal/zen"
)

var maintenanceLabels = []string{"service", "id", "relevance", "type", "impact", "title", "window"}

var (
	maintenanceStart = prometheus.NewDesc("zen_maintenance_start_timestamp_seconds",
		"Start of a relevant maintenance window.", maintenanceLabels, nil)
	maintenanceEnd = prometheus.NewDesc("zen_maintenance_end_timestamp_seconds",
		"End of a relevant maintenance window, estimated if Zen gives no usable end.", maintenanceLabels, nil)
	up = prometheus.NewDesc("zen_up", "Whether the last poll of Zen's data succeeded.",
		nil, nil)
	polls = prometheus.NewDesc("zen_exporter_polls_total", "Polls of Zen's data.",
		nil, nil)
	pollFailures = prometheus.NewDesc("zen_exporter_poll_failures_total",
		"Polls of Zen's data that failed.", nil, nil)
	lastSuccess = prometheus.NewDesc("zen_exporter_last_success_timestamp_seconds",
		"When Zen's data was last polled successfully, or 0 if it never has been.", nil, nil)
	lastDuration = prometheus.NewDesc("zen_exporter_last_poll_duration_seconds",
		"How long the last poll took.", nil, nil)
	alerts = prometheus.NewDesc("zen_exporter_alerts",
		"Alerts read from the configured services on the last successful poll.", nil, nil)
	skippedAlerts = prometheus.NewDesc("zen_exporter_skipped_alerts",
		"Relevant alerts without a usable start time on the last successful poll.", nil, nil)
)

type Source interface {
	Snapshot() zen.Snapshot
}

type Collector struct {
	source Source
}

func New(source Source) *Collector {
	return &Collector{source: source}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		maintenanceStart, maintenanceEnd, up, polls, pollFailures,
		lastSuccess, lastDuration, alerts, skippedAlerts,
	} {
		ch <- desc
	}
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	snapshot := c.source.Snapshot()

	for _, item := range snapshot.Maintenance {
		labels := []string{
			item.Service, strconv.FormatInt(item.ID, 10), string(item.Relevance),
			item.Type, item.Impact, item.Title, item.Window,
		}
		ch <- prometheus.MustNewConstMetric(maintenanceStart, prometheus.GaugeValue, timestamp(item.Start), labels...)
		ch <- prometheus.MustNewConstMetric(maintenanceEnd, prometheus.GaugeValue, timestamp(item.End), labels...)
	}

	upValue := 0.0
	if snapshot.Up {
		upValue = 1
	}
	ch <- prometheus.MustNewConstMetric(up, prometheus.GaugeValue, upValue)
	ch <- prometheus.MustNewConstMetric(polls, prometheus.CounterValue, float64(snapshot.Polls))
	ch <- prometheus.MustNewConstMetric(pollFailures, prometheus.CounterValue, float64(snapshot.Failures))
	ch <- prometheus.MustNewConstMetric(lastSuccess, prometheus.GaugeValue, timestamp(snapshot.LastSuccess))
	ch <- prometheus.MustNewConstMetric(lastDuration, prometheus.GaugeValue, snapshot.LastDuration.Seconds())
	ch <- prometheus.MustNewConstMetric(alerts, prometheus.GaugeValue, float64(snapshot.Alerts))
	ch <- prometheus.MustNewConstMetric(skippedAlerts, prometheus.GaugeValue, float64(snapshot.Skipped))
}

func timestamp(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.Unix())
}
