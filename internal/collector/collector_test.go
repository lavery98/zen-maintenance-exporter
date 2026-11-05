package collector

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/lavery98/zen-maintenance-exporter/internal/zen"
)

type staticSource zen.Snapshot

func (s staticSource) Snapshot() zen.Snapshot { return zen.Snapshot(s) }

func TestCollect(t *testing.T) {
	start := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	collector := New(staticSource{
		Maintenance: []zen.Maintenance{{
			Service:   zen.DefaultService,
			ID:        12592,
			Relevance: zen.RelevanceLocation,
			Type:      "Planned",
			Impact:    "Outage",
			Title:     `Quote " and \ backslash`,
			Window:    "Wed 18 Nov 00:00 GMT to Wed 18 Nov 06:00 GMT",
			Start:     start,
			End:       start.Add(6 * time.Hour),
		}},
		Alerts:      100,
		Skipped:     1,
		Up:          true,
		LastSuccess: start,
		Polls:       3,
		Failures:    1,
	})

	expected := `
# HELP zen_exporter_alerts Alerts read from the configured services on the last successful poll.
# TYPE zen_exporter_alerts gauge
zen_exporter_alerts 100
# HELP zen_exporter_last_success_timestamp_seconds When Zen's data was last polled successfully, or 0 if it never has been.
# TYPE zen_exporter_last_success_timestamp_seconds gauge
zen_exporter_last_success_timestamp_seconds 1.79496e+09
# HELP zen_exporter_poll_failures_total Polls of Zen's data that failed.
# TYPE zen_exporter_poll_failures_total counter
zen_exporter_poll_failures_total 1
# HELP zen_exporter_polls_total Polls of Zen's data.
# TYPE zen_exporter_polls_total counter
zen_exporter_polls_total 3
# HELP zen_exporter_skipped_alerts Relevant alerts without a usable start time on the last successful poll.
# TYPE zen_exporter_skipped_alerts gauge
zen_exporter_skipped_alerts 1
# HELP zen_maintenance_end_timestamp_seconds End of a relevant maintenance window, estimated if Zen gives no usable end.
# TYPE zen_maintenance_end_timestamp_seconds gauge
zen_maintenance_end_timestamp_seconds{id="12592",impact="Outage",relevance="location",service="connectivity.broadband",title="Quote \" and \\ backslash",type="Planned",window="Wed 18 Nov 00:00 GMT to Wed 18 Nov 06:00 GMT"} 1.7949816e+09
# HELP zen_maintenance_start_timestamp_seconds Start of a relevant maintenance window.
# TYPE zen_maintenance_start_timestamp_seconds gauge
zen_maintenance_start_timestamp_seconds{id="12592",impact="Outage",relevance="location",service="connectivity.broadband",title="Quote \" and \\ backslash",type="Planned",window="Wed 18 Nov 00:00 GMT to Wed 18 Nov 06:00 GMT"} 1.79496e+09
# HELP zen_up Whether the last poll of Zen's data succeeded.
# TYPE zen_up gauge
zen_up 1
`
	names := []string{
		"zen_exporter_alerts",
		"zen_exporter_last_success_timestamp_seconds",
		"zen_exporter_poll_failures_total",
		"zen_exporter_polls_total",
		"zen_exporter_skipped_alerts",
		"zen_maintenance_end_timestamp_seconds",
		"zen_maintenance_start_timestamp_seconds",
		"zen_up",
	}
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expected), names...); err != nil {
		t.Error(err)
	}
}

func TestCollectBeforeFirstPoll(t *testing.T) {
	collector := New(staticSource{})

	if count := testutil.CollectAndCount(collector, "zen_maintenance_start_timestamp_seconds"); count != 0 {
		t.Errorf("got %d maintenance series, want 0", count)
	}
	expected := `
# HELP zen_exporter_last_success_timestamp_seconds When Zen's data was last polled successfully, or 0 if it never has been.
# TYPE zen_exporter_last_success_timestamp_seconds gauge
zen_exporter_last_success_timestamp_seconds 0
# HELP zen_up Whether the last poll of Zen's data succeeded.
# TYPE zen_up gauge
zen_up 0
`
	err := testutil.CollectAndCompare(collector, strings.NewReader(expected),
		"zen_exporter_last_success_timestamp_seconds", "zen_up")
	if err != nil {
		t.Error(err)
	}
}

func TestMetricNames(t *testing.T) {
	problems, err := testutil.CollectAndLint(New(staticSource{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Errorf("%s: %s", problem.Metric, problem.Text)
	}
}
