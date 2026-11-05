package zen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultURL is the undocumented data file behind Zen's service alerts site.
	DefaultURL = "https://servicealerts.zen.co.uk/data.json"
	// DefaultService is the part of the data file that holds broadband alerts.
	DefaultService = "connectivity.broadband"

	maxBodyBytes = 64 << 20
)

// Alert is one service alert.
type Alert struct {
	ID                 int64    `json:"id"`
	Title              string   `json:"title"`
	Active             bool     `json:"active"`
	StartDateTime      string   `json:"startDateTime"`
	EndDateTime        string   `json:"endDateTime"`
	ConnectivityStatus string   `json:"connectivityStatus"`
	MaintenanceType    string   `json:"maintenanceType"`
	Details            []Detail `json:"details"`
}

// Detail is one entry in an alert's timeline.
type Detail struct {
	ID            int64  `json:"id"`
	Text          string `json:"text"`
	StartDateTime string `json:"startDateTime"`
	EndDateTime   string `json:"endDateTime"`
}

type Feed map[string]json.RawMessage

func (f Feed) Alerts(service string) ([]Alert, error) {
	groupName, serviceName, ok := strings.Cut(service, ".")
	if !ok {
		return nil, fmt.Errorf("invalid service %q: expected group.name", service)
	}
	rawGroup, ok := f[groupName]
	if !ok {
		return nil, fmt.Errorf("service group %q not found", groupName)
	}
	var group map[string]json.RawMessage
	if err := json.Unmarshal(rawGroup, &group); err != nil {
		return nil, fmt.Errorf("decoding service group %q: %w", groupName, err)
	}
	rawAlerts, ok := group[serviceName]
	if !ok {
		return nil, fmt.Errorf("service %q not found", service)
	}
	var alerts []Alert
	if err := json.Unmarshal(rawAlerts, &alerts); err != nil {
		return nil, fmt.Errorf("decoding service %q: %w", service, err)
	}
	return alerts, nil
}

type Client struct {
	url       string
	userAgent string
	http      *http.Client
}

func NewClient(url, userAgent string, timeout time.Duration) *Client {
	return &Client{
		url:       url,
		userAgent: userAgent,
		http:      &http.Client{Timeout: timeout},
	}
}

func (c *Client) Fetch(ctx context.Context) (Feed, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodyBytes {
		return nil, errors.New("response is too large")
	}

	var feed Feed
	if err := json.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return feed, nil
}
