package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/bootstrap"

	"github.com/lavery98/zen-maintenance-exporter/internal/collector"
	"github.com/lavery98/zen-maintenance-exporter/internal/zen"
)

const (
	name           = "zen-maintenance-exporter"
	metricsProgram = "zen_exporter"
	defaultAddress = ":10062"
)

func main() {
	var (
		url = kingpin.Flag("zen.url",
			"URL of Zen's service alerts data.").
			Default(zen.DefaultURL).String()
		locationRegex = kingpin.Flag("zen.location-regex",
			"Case-insensitive regex for your town and exchange code, matched against alert titles and text.").
			Required().String()
		networkRegex = kingpin.Flag("zen.network-regex",
			"Case-insensitive regex for you network supplier. Reports alerts that mention it but list no areas. Empty disabled this.").
			Default("").String()
		vagueTitleRegex = kingpin.Flag("zen.vague-title-regex",
			"Case-insensitive regex for titles that don't name an area.").
			Default(zen.DefaultVagueTitleRegex).String()
		maintenanceTypes = kingpin.Flag("zen.maintenance-type",
			"Alert types to include: Planned, Emergency or None. Repeat for several.").
			Default("Planned", "Emergency").Strings()
		services = kingpin.Flag("zen.service",
			"Service to check, as group.name. Repeat for several.").
			Default(zen.DefaultService).Strings()
		pollInterval = kingpin.Flag("zen.poll-interval",
			"How often to fetch Zen's data.").
			Default("1h").Duration()
		timeout = kingpin.Flag("zen.timeout",
			"Timeout for each fetch.").
			Default("30s").Duration()
		maxWindow = kingpin.Flag("zen.max-window",
			"Longest window to trust. Missing or later end times are replaced with start plus this.").
			Default("168h").Duration()
		displayTimezone = kingpin.Flag("zen.display-timezone",
			"Timezone for the window label.").
			Default("Europe/London").String()
	)

	kingpin.CommandLine.UsageWriter(os.Stdout)
	runner := bootstrap.New(bootstrap.Config{
		App:            kingpin.CommandLine,
		Name:           name,
		Description:    "Prometheus exporter for Zen Internet maintenance",
		DefaultAddress: defaultAddress,
		MetricsHandlerFactory: func(b *bootstrap.Bootstrap) (http.Handler, error) {
			if err := validatePollInterval(*pollInterval); err != nil {
				return nil, err
			}
			filter, err := newFilter(*locationRegex, *networkRegex, *vagueTitleRegex, *displayTimezone, *maintenanceTypes, *maxWindow)
			if err != nil {
				return nil, err
			}
			for _, service := range *services {
				if !strings.Contains(service, ".") {
					return nil, fmt.Errorf("invalid service %q: expected group.name", service)
				}
			}

			client := zen.NewClient(*url, name+"/"+version.Version, *timeout)
			poller := zen.NewPoller(client, *services, filter, *pollInterval, b.Logger)
			go poller.Run(context.Background())

			b.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("ok\n"))
			})
			return newHandler(collector.New(poller), !b.DisableExporterMetrics, b.Logger), nil
		},
	})

	if err := runner.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validatePollInterval(interval time.Duration) error {
	if interval <= 0 {
		return errors.New("--zen.poll-interval must be greater than zero")
	}
	return nil
}

func newFilter(location, network, vagueTitle, timezone string, types []string, maxWindow time.Duration) (zen.Filter, error) {
	if maxWindow <= 0 {
		return zen.Filter{}, errors.New("--zen.max-window must be greater than zero")
	}
	locationRegex, err := compile("--zen.location-regex", location)
	if err != nil {
		return zen.Filter{}, err
	}
	vagueTitleRegex, err := compile("--zen.vague-title-regex", vagueTitle)
	if err != nil {
		return zen.Filter{}, err
	}
	var networkRegex *regexp.Regexp
	if network != "" {
		if networkRegex, err = compile("--zen.network-regex", network); err != nil {
			return zen.Filter{}, err
		}
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return zen.Filter{}, fmt.Errorf("--zen.display-timezone: %w", err)
	}
	return zen.Filter{
		Location:   locationRegex,
		Network:    networkRegex,
		VagueTitle: vagueTitleRegex,
		Types:      types,
		MaxWindow:  maxWindow,
		Zone:       zone,
	}, nil
}

func compile(flag, pattern string) (*regexp.Regexp, error) {
	compiled, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", flag, err)
	}
	return compiled, nil
}

func newHandler(c prometheus.Collector, exporterMetrics bool, logger *slog.Logger) http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(versioncollector.NewCollector(metricsProgram), c)

	opts := promhttp.HandlerOpts{
		ErrorLog:      slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ErrorHandling: promhttp.ContinueOnError,
	}
	if !exporterMetrics {
		return promhttp.HandlerFor(registry, opts)
	}

	registry.MustRegister(
		promcollectors.NewProcessCollector(promcollectors.ProcessCollectorOpts{}),
		promcollectors.NewGoCollector(),
	)
	opts.Registry = registry
	return promhttp.InstrumentMetricHandler(registry, promhttp.HandlerFor(registry, opts))
}
