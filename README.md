# zen-maintenance-exporter

[![Go](https://img.shields.io/github/go-mod/go-version/lavery98/zen-maintenance-exporter)](https://go.dev/)
[![License](https://img.shields.io/github/license/lavery98/zen-maintenance-exporter)](LICENSE)
[![Release](https://img.shields.io/github/v/release/lavery98/zen-maintenance-exporter)](https://github.com/lavery98/zen-maintenance-exporter/releases)

A Prometheus exporter for [Zen Internet](https://www.zen.co.uk/) maintenance. It filters Zen's service alerts down to your location, so you can be warned before planned work starts and silence your own connectivity alerts while it runs.

This is an unofficial project and isn't affiliated with or supported by Zen Internet. It reads the undocumented data file behind Zen's [service alerts](https://servicealerts.zen.co.uk/) site, which could change at any time.

## Installation

### Using Go

```bash
go install github.com/lavery98/zen-maintenance-exporter@latest
```

### Download Pre-built Binary

Download the latest release for your platform from the [releases page](https://github.com/lavery98/zen-maintenance-exporter/releases).

## Usage

Find your town and exchange code on the [service alerts](https://servicealerts.zen.co.uk/) site, then match both. Exchange codes appear in titles such as `EAIPS-Ipswich`.

### Run Locally

```bash
zen-maintenance-exporter \
  --zen.location-regex='\b(EAIPS|Ipswich)\b' \
  --zen.network-regex='\bcity\s*fi(?:b)?re\b'
```

### Test the Endpoint

```bash
curl http://localhost:10062/metrics
```

## Filtering

An active alert of an included type is exposed in one of two ways:

| `relevance` | Meaning |
|-------------|---------|
| `location` | The title or any update matches `--zen.location-regex`. |
| `possible` | It has a vague title, such as "Multiple Regions", and mentions the network in `--zen.network-regex`. Zen hasn't said which areas are affected. |

Some alerts name no location or network at all, so no filter can match them. Matching your town and exchange code is more reliable than either one alone, as Zen's spelling varies: for example, one alert titled "Bourne" says "Borne" in its text. If your town's name has an apostrophe or more than one word, match the part least likely to vary.

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--zen.location-regex` | Required | Case-insensitive regex for your town and exchange code |
| `--zen.network-regex` | Empty | Case-insensitive regex for your network supplier. Empty disables `possible` alerts |
| `--zen.vague-title-regex` | See `--help` | Case-insensitive regex for titles that don't name an area |
| `--zen.maintenance-type` | `Planned`, `Emergency` | Alert types to include: `Planned`, `Emergency` or `None`. Repeat for several. `None` adds faults and outages |
| `--zen.service` | `connectivity.broadband` | Service to check. Repeat for several, for example `network.routingAndCoreNetwork` |
| `--zen.poll-interval` | `1h` | How often to fetch Zen's data |
| `--zen.timeout` | `30s` | Timeout for each fetch |
| `--zen.max-window` | `168h` | Longest window to trust. Missing or later end times become start plus this |
| `--zen.display-timezone` | `Europe/London` | Timezone for the `window` label |
| `--zen.url` | Zen's data file | Where to fetch Zen's data from |

The usual exporter flags are also available, including `--web.listen-address`, `--web.telemetry-path`, `--web.config.file`, `--web.disable-exporter-metrics`, `--log.level` and `--log.format`. Run `zen-maintenance-exporter --help` for the full list.

Please keep the poll interval reasonable. The data file is over 1 MB, and scrapes are served from the last poll, so scraping more often doesn't fetch it more often.

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `zen_maintenance_start_timestamp_seconds` | Gauge | Start of a relevant maintenance window |
| `zen_maintenance_end_timestamp_seconds` | Gauge | End of a relevant maintenance window, estimated if Zen gives no usable end |
| `zen_up` | Gauge | Whether the last poll succeeded |
| `zen_exporter_polls_total` | Counter | Polls of Zen's data |
| `zen_exporter_poll_failures_total` | Counter | Polls that failed |
| `zen_exporter_last_success_timestamp_seconds` | Gauge | When Zen's data was last polled successfully |
| `zen_exporter_last_poll_duration_seconds` | Gauge | How long the last poll took |
| `zen_exporter_alerts` | Gauge | Alerts read on the last successful poll |
| `zen_exporter_skipped_alerts` | Gauge | Relevant alerts without a usable start time |
| `zen_exporter_build_info` | Gauge | Version information |

Maintenance metrics have the labels `service`, `id`, `relevance`, `type`, `impact`, `title` and `window`. `impact` is the status Zen lists for the alert: `Up`, `Partial`, `Outage` or `Suspended`.

Standard `go_*`, `process_*` and `promhttp_*` metrics are included unless you set `--web.disable-exporter-metrics`.

If a poll fails, the exporter keeps serving its last good data and sets `zen_up` to 0, so a temporary problem doesn't clear your alerts.

## Building from Source

### Prerequisites

- Go 1.25 or later

### Build Binary

```bash
make build
```

### Run Tests

```bash
make test
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
