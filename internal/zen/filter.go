package zen

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"
)

// DefaultVagueTitleRegex matches titles that don't name an area.
const DefaultVagueTitleRegex = `\bmultiple\s+(?:regions?|exchanges?|areas?|locations?|sites?)\b|\bnationwide\b|\bacross\s+(?:multiple|the\s+network)\b`

const windowLayout = "Mon 02 Jan 15:04 MST"

// Relevance says how an alert relates to the configured location.
type Relevance string

const (
	// RelevanceLocation means the alert names the configured location.
	RelevanceLocation Relevance = "location"
	// RelevancePossible means the alert is for the configured network but
	// doesn't list its areas, so the location can't be ruled in or out.
	RelevancePossible Relevance = "possible"
)

// Filter selects relevant alerts and works out their maintenance windows.
type Filter struct {
	// Location is required.
	Location *regexp.Regexp
	// Network is optional. Without it, no alert is RelevancePossible.
	Network    *regexp.Regexp
	VagueTitle *regexp.Regexp
	// Types lists the maintenance types to include, ignoring case.
	Types []string
	// MaxWindow replaces a missing or implausibly late end time.
	MaxWindow time.Duration
	// Zone is the timezone used for Maintenance.Window.
	Zone *time.Location
}

// Maintenance is a relevant alert with its resolved window.
type Maintenance struct {
	Service   string
	ID        int64
	Relevance Relevance
	Type      string
	Impact    string
	Title     string
	Window    string
	Start     time.Time
	End       time.Time
}

func (f Filter) Select(service string, alerts []Alert) ([]Maintenance, int) {
	var (
		selected []Maintenance
		skipped  int
		seen     = make(map[int64]struct{}, len(alerts))
	)
	for _, alert := range alerts {
		// Duplicate IDs would produce duplicate series and fail the scrape.
		if _, duplicate := seen[alert.ID]; duplicate {
			continue
		}
		seen[alert.ID] = struct{}{}

		relevance, ok := f.relevance(alert)
		if !ok {
			continue
		}
		item, ok := f.resolve(service, alert, relevance)
		if !ok {
			skipped++
			continue
		}
		selected = append(selected, item)
	}
	slices.SortStableFunc(selected, func(a, b Maintenance) int {
		return a.Start.Compare(b.Start)
	})
	return selected, skipped
}

func (f Filter) relevance(alert Alert) (Relevance, bool) {
	if !alert.Active || !f.includes(alert.MaintenanceType) {
		return "", false
	}

	text := searchText(alert)
	if f.Location.MatchString(text) {
		return RelevanceLocation, true
	}
	if f.Network != nil && f.VagueTitle.MatchString(alert.Title) && f.Network.MatchString(text) {
		return RelevancePossible, true
	}
	return "", false
}

func (f Filter) includes(maintenanceType string) bool {
	return slices.ContainsFunc(f.Types, func(wanted string) bool {
		return strings.EqualFold(wanted, maintenanceType)
	})
}

func searchText(alert Alert) string {
	var text strings.Builder
	text.WriteString(alert.Title)
	for _, detail := range alert.Details {
		text.WriteByte('\n')
		text.WriteString(detail.Text)
	}
	return text.String()
}

func (f Filter) resolve(service string, alert Alert, relevance Relevance) (Maintenance, bool) {
	start, ok := parseTime(latest(alert, alert.StartDateTime, func(d Detail) string { return d.StartDateTime }))
	if !ok {
		return Maintenance{}, false
	}

	end, ok := parseTime(latest(alert, alert.EndDateTime, func(d Detail) string { return d.EndDateTime }))
	limit := start.Add(f.MaxWindow)
	endKnown := ok && end.After(start) && !end.After(limit)
	if !endKnown {
		end = limit
	}

	return Maintenance{
		Service:   service,
		ID:        alert.ID,
		Relevance: relevance,
		Type:      alert.MaintenanceType,
		Impact:    alert.ConnectivityStatus,
		Title:     strings.Join(strings.Fields(alert.Title), " "),
		Window:    f.formatWindow(start, end, endKnown),
		Start:     start,
		End:       end,
	}, true
}

func latest(alert Alert, fallback string, field func(Detail) string) string {
	details := slices.SortedStableFunc(slices.Values(alert.Details), func(a, b Detail) int {
		return cmp.Compare(b.ID, a.ID)
	})
	for _, detail := range details {
		if value := field(detail); value != "" {
			return value
		}
	}
	return fallback
}

func parseTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, value)
	return parsed, err == nil
}

func (f Filter) formatWindow(start, end time.Time, endKnown bool) string {
	window := start.In(f.Zone).Format(windowLayout)
	if !endKnown {
		return window + ", end unknown"
	}
	return window + " to " + end.In(f.Zone).Format(windowLayout)
}
