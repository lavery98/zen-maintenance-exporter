package zen

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testFilter(t *testing.T) Filter {
	t.Helper()
	zone, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	return Filter{
		Location:   regexp.MustCompile(`(?i)\b(EAIPS|Ipswich)\b`),
		Network:    regexp.MustCompile(`(?i)\bcity\s*fi(?:b)?re\b`),
		VagueTitle: regexp.MustCompile(`(?i)` + DefaultVagueTitleRegex),
		Types:      []string{"Planned", "Emergency"},
		MaxWindow:  7 * 24 * time.Hour,
		Zone:       zone,
	}
}

func planned(id int64, title, start, end string, text ...string) Alert {
	alert := Alert{
		ID:                 id,
		Title:              title,
		Active:             true,
		StartDateTime:      start,
		EndDateTime:        end,
		ConnectivityStatus: "Outage",
		MaintenanceType:    "Planned",
	}
	for i, t := range text {
		alert.Details = append(alert.Details, Detail{
			ID: id*10 + int64(i), Text: t, StartDateTime: start, EndDateTime: end,
		})
	}
	return alert
}

func TestSelectRelevance(t *testing.T) {
	const start, end = "2026-11-18T00:00:00.000Z", "2026-11-18T06:00:00.000Z"

	tests := []struct {
		name  string
		alert Alert
		want  Relevance
	}{
		{"town in title", planned(1, "CityFibre Planned Maintenance - Bath, and Ipswich", start, end), RelevanceLocation},
		{"exchange code in title", planned(2, "Planned Maintenance - EAIPS-Ipswich", start, end), RelevanceLocation},
		{"exchange code only in text", planned(3, "Planned Maintenance - Multiple exchanges", start, end, "City fibre work at EMMATLO, EAIPS"), RelevanceLocation},
		{"different case", planned(4, "planned work in IPSWICH", start, end), RelevanceLocation},
		{"partial word", planned(5, "Planned Maintenance - Ipswichshire", start, end), ""},
		{"vague for our network", planned(6, "Planned maintenance - Multiple Regions - 21813", start, end, "CityFibre network upgrade"), RelevancePossible},
		{"vague, Cityfire typo", planned(7, "Network works across multiple areas", start, end, "Cityfire works"), RelevancePossible},
		{"vague for another network", planned(8, "Multiple regions - Openreach upgrades", start, end, "local Openreach networks"), ""},
		{"named towns on our network", planned(9, "CityFibre Maintenance Firmware Updates - 21337", start, end, "Blackpool, Rugby, Norwich"), ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, skipped := testFilter(t).Select(DefaultService, []Alert{test.alert})
			if skipped != 0 {
				t.Fatalf("skipped = %d", skipped)
			}
			if test.want == "" {
				if len(got) != 0 {
					t.Fatalf("got %+v, want no match", got)
				}
				return
			}
			if len(got) != 1 || got[0].Relevance != test.want {
				t.Fatalf("got %+v, want relevance %q", got, test.want)
			}
		})
	}
}

func TestSelectIgnoresInactiveAndOtherTypes(t *testing.T) {
	closed := planned(1, "Planned Maintenance - EAIPS", "2026-11-01T00:00:00Z", "2026-11-01T06:00:00Z")
	closed.Active = false
	fault := planned(2, "Exchange flap - EAIPS", "2026-11-02T00:00:00Z", "")
	fault.MaintenanceType = "None"
	emergency := planned(3, "Emergency Maintenance - EAIPS", "2026-11-03T00:00:00Z", "")
	emergency.MaintenanceType = "Emergency"

	got, _ := testFilter(t).Select(DefaultService, []Alert{closed, fault, emergency})
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("got %+v, want only alert 3", got)
	}
}

func TestSelectWindows(t *testing.T) {
	const start = "2026-11-18T00:00:00.000Z"
	startTime := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	capped := startTime.Add(7 * 24 * time.Hour)

	nullAlertEnd := planned(1, "EAIPS", start, "")
	nullAlertEnd.Details = []Detail{{ID: 1, StartDateTime: start, EndDateTime: "2026-11-18T06:00:00.000Z"}}

	moved := planned(2, "EAIPS", "2026-11-04T01:00:00.000Z", "2026-11-04T06:00:00.000Z")
	moved.Details = []Detail{
		{ID: 2, StartDateTime: "2026-11-05T01:00:00.000Z", EndDateTime: "2026-11-05T06:00:00.000Z"},
		{ID: 1, StartDateTime: "2026-11-03T01:00:00.000Z", EndDateTime: "2026-11-03T06:00:00.000Z"},
	}

	tests := []struct {
		name       string
		alert      Alert
		wantStart  time.Time
		wantEnd    time.Time
		wantWindow string
	}{
		{
			"end only in details", nullAlertEnd,
			startTime, startTime.Add(6 * time.Hour),
			"Wed 18 Nov 00:00 GMT to Wed 18 Nov 06:00 GMT",
		},
		{
			"newest detail wins", moved,
			time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC), time.Date(2026, 11, 5, 6, 0, 0, 0, time.UTC),
			"Thu 05 Nov 01:00 GMT to Thu 05 Nov 06:00 GMT",
		},
		{"no end", planned(3, "EAIPS", start, ""), startTime, capped, "Wed 18 Nov 00:00 GMT, end unknown"},
		{"year 2202", planned(4, "EAIPS", start, "2202-11-26T06:00:00.000Z"), startTime, capped, "Wed 18 Nov 00:00 GMT, end unknown"},
		{"year 9999", planned(5, "EAIPS", start, "9999-01-01T00:00:00.000Z"), startTime, capped, "Wed 18 Nov 00:00 GMT, end unknown"},
		{"end before start", planned(6, "EAIPS", start, "2026-11-17T00:00:00.000Z"), startTime, capped, "Wed 18 Nov 00:00 GMT, end unknown"},
		{"invalid end", planned(7, "EAIPS", start, "soon"), startTime, capped, "Wed 18 Nov 00:00 GMT, end unknown"},
		{
			"BST", planned(8, "EAIPS", "2026-10-13T23:05:00.000Z", "2026-10-14T05:00:00.000Z"),
			time.Date(2026, 10, 13, 23, 5, 0, 0, time.UTC), time.Date(2026, 10, 14, 5, 0, 0, 0, time.UTC),
			"Wed 14 Oct 00:05 BST to Wed 14 Oct 06:00 BST",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := testFilter(t).Select(DefaultService, []Alert{test.alert})
			if len(got) != 1 {
				t.Fatalf("got %d results, want 1", len(got))
			}
			if !got[0].Start.Equal(test.wantStart) {
				t.Errorf("start = %s, want %s", got[0].Start, test.wantStart)
			}
			if !got[0].End.Equal(test.wantEnd) {
				t.Errorf("end = %s, want %s", got[0].End, test.wantEnd)
			}
			if got[0].Window != test.wantWindow {
				t.Errorf("window = %q, want %q", got[0].Window, test.wantWindow)
			}
		})
	}
}

func TestSelectSkipsUnusableStart(t *testing.T) {
	got, skipped := testFilter(t).Select(DefaultService, []Alert{planned(1, "EAIPS", "soon", "")})
	if len(got) != 0 || skipped != 1 {
		t.Fatalf("got %+v and %d skipped, want one skipped", got, skipped)
	}
}

func TestSelectOrdersAndDeduplicates(t *testing.T) {
	alerts := []Alert{
		planned(2, "Later EAIPS", "2026-11-20T00:00:00Z", ""),
		planned(1, "Earlier EAIPS", "2026-11-19T00:00:00Z", ""),
		planned(2, "Duplicate EAIPS", "2026-11-21T00:00:00Z", ""),
	}
	got, _ := testFilter(t).Select(DefaultService, alerts)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 || got[1].Title != "Later EAIPS" {
		t.Fatalf("got %+v", got)
	}
}

func TestSelectTidiesTitle(t *testing.T) {
	got, _ := testFilter(t).Select(DefaultService, []Alert{planned(1, "  Multiple   EAIPS \n", "2026-11-19T00:00:00Z", "")})
	if len(got) != 1 || got[0].Title != "Multiple EAIPS" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodesRealFieldShapes(t *testing.T) {
	const body = `{
		"network": {"routingAndCoreNetwork": []},
		"connectivity": {"broadband": [{
			"id": 12592,
			"serviceType": "Broadband (xDSL)",
			"title": "CityFibre Planned Maintenance - Ipswich",
			"category": "Connectivity",
			"active": true,
			"startDateTime": "2026-11-18T00:00:00.000Z",
			"endDateTime": null,
			"clearDateTime": null,
			"connectivityStatus": "Outage",
			"maintenanceType": "Planned",
			"details": [{
				"id": 31276,
				"text": null,
				"type": "New",
				"startDateTime": "2026-11-18T00:00:00.000Z",
				"endDateTime": "2026-11-18T06:00:00.000Z"
			}]
		}]},
		"somethingNew": "not an object"
	}`
	var feed Feed
	if err := json.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatal(err)
	}
	alerts, err := feed.Alerts(DefaultService)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := testFilter(t).Select(DefaultService, alerts)
	if len(got) != 1 || got[0].Window != "Wed 18 Nov 00:00 GMT to Wed 18 Nov 06:00 GMT" {
		t.Fatalf("got %+v", got)
	}
}

func TestFeedAlertsErrors(t *testing.T) {
	feed := Feed{"connectivity": json.RawMessage(`{"broadband":"oops"}`), "other": json.RawMessage(`[]`)}
	for _, service := range []string{"broadband", "missing.broadband", "connectivity.missing", "connectivity.broadband", "other.thing"} {
		if _, err := feed.Alerts(service); err == nil {
			t.Errorf("Alerts(%q) returned no error", service)
		}
	}
}

func TestClientFetch(t *testing.T) {
	var userAgent atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent.Store(r.UserAgent())
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Error("request doesn't accept gzip")
		}
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, `{"connectivity":{"broadband":[{"id":1,"title":"EAIPS"}]}}`)
	}))
	defer server.Close()

	feed, err := NewClient(server.URL, "test-agent/1.0", time.Second).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := feed.Alerts(DefaultService)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].ID != 1 {
		t.Fatalf("got %+v", alerts)
	}
	if got := userAgent.Load(); got != "test-agent/1.0" {
		t.Errorf("user agent = %v", got)
	}
}

func TestClientFetchErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }},
		{"not JSON", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>") }},
		{"timeout", func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			if _, err := NewClient(server.URL, "test", 50*time.Millisecond).Fetch(context.Background()); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

type fetcherFunc func() (Feed, error)

func (f fetcherFunc) Fetch(context.Context) (Feed, error) { return f() }

func feedOf(alerts ...Alert) Feed {
	encoded, err := json.Marshal(alerts)
	if err != nil {
		panic(err)
	}
	return Feed{"connectivity": json.RawMessage(fmt.Sprintf(`{"broadband":%s}`, encoded))}
}

func TestPollerKeepsLastGoodData(t *testing.T) {
	good := feedOf(planned(1, "EAIPS", "2026-11-18T00:00:00Z", "2026-11-18T06:00:00Z", "text"))
	var (
		feed Feed
		err  error
	)
	poller := NewPoller(fetcherFunc(func() (Feed, error) { return feed, err }),
		[]string{DefaultService}, testFilter(t), time.Hour, slog.New(slog.DiscardHandler))

	if snapshot := poller.Snapshot(); snapshot.Up || !snapshot.LastSuccess.IsZero() {
		t.Fatalf("unexpected initial snapshot: %+v", snapshot)
	}

	feed = good
	poller.poll(context.Background())
	first := poller.Snapshot()
	if !first.Up || first.Alerts != 1 || len(first.Maintenance) != 1 || first.LastSuccess.IsZero() {
		t.Fatalf("after success: %+v", first)
	}

	failures := []struct {
		name string
		feed Feed
		err  error
	}{
		{"fetch error", nil, fmt.Errorf("boom")},
		{"invalid service", Feed{"connectivity": json.RawMessage(`{"broadband":{}}`)}, nil},
		{"service removed", Feed{"connectivity": json.RawMessage(`{}`)}, nil},
	}
	for i, failure := range failures {
		feed, err = failure.feed, failure.err
		poller.poll(context.Background())
		got := poller.Snapshot()
		if got.Up || got.Failures != uint64(i+1) {
			t.Errorf("%s: up = %v, failures = %d", failure.name, got.Up, got.Failures)
		}
		if len(got.Maintenance) != 1 || got.Alerts != 1 || !got.LastSuccess.Equal(first.LastSuccess) {
			t.Errorf("%s: lost last good data: %+v", failure.name, got)
		}
	}

	feed, err = feedOf(planned(2, "Somewhere else", "2026-11-18T00:00:00Z", "")), nil
	poller.poll(context.Background())
	got := poller.Snapshot()
	if !got.Up || len(got.Maintenance) != 0 || got.Polls != 5 {
		t.Fatalf("after recovery: %+v", got)
	}
}

func TestPollerClearsLastGoodDataOnEmptyService(t *testing.T) {
	feed := feedOf(planned(1, "EAIPS", "2026-11-18T00:00:00Z", "2026-11-18T06:00:00Z"))
	poller := NewPoller(fetcherFunc(func() (Feed, error) { return feed, nil }),
		[]string{DefaultService}, testFilter(t), time.Hour, slog.New(slog.DiscardHandler))
	poller.poll(context.Background())
	if got := poller.Snapshot(); !got.Up || len(got.Maintenance) != 1 {
		t.Fatalf("after initial poll: %+v", got)
	}

	feed = Feed{"connectivity": json.RawMessage(`{"broadband":[]}`)}
	poller.poll(context.Background())
	got := poller.Snapshot()
	if !got.Up || len(got.Maintenance) != 0 || got.Alerts != 0 || got.Skipped != 0 || got.Polls != 2 || got.Failures != 0 {
		t.Fatalf("after empty service: %+v", got)
	}
}

func TestPollerCombinesServices(t *testing.T) {
	alerts, err := json.Marshal([]Alert{planned(1, "EAIPS", "2026-11-20T00:00:00Z", "")})
	if err != nil {
		t.Fatal(err)
	}
	core, err := json.Marshal([]Alert{planned(1, "EAIPS core", "2026-11-19T00:00:00Z", "")})
	if err != nil {
		t.Fatal(err)
	}
	feed := Feed{
		"connectivity": json.RawMessage(fmt.Sprintf(`{"broadband":%s}`, alerts)),
		"network":      json.RawMessage(fmt.Sprintf(`{"routingAndCoreNetwork":%s}`, core)),
	}
	poller := NewPoller(fetcherFunc(func() (Feed, error) { return feed, nil }),
		[]string{DefaultService, "network.routingAndCoreNetwork"}, testFilter(t), time.Hour, slog.New(slog.DiscardHandler))
	poller.poll(context.Background())

	got := poller.Snapshot()
	if got.Alerts != 2 || len(got.Maintenance) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Maintenance[0].Service != "network.routingAndCoreNetwork" || got.Maintenance[1].Service != DefaultService {
		t.Fatalf("not ordered by start: %+v", got.Maintenance)
	}
}

func TestPollerRunStopsWhenCancelled(t *testing.T) {
	var polls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	poller := NewPoller(fetcherFunc(func() (Feed, error) {
		if polls.Add(1) >= 3 {
			cancel()
		}
		return feedOf(planned(1, "EAIPS", "2026-11-18T00:00:00Z", "")), nil
	}), []string{DefaultService}, testFilter(t), time.Millisecond, slog.New(slog.DiscardHandler))

	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop")
	}
}
