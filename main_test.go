package main

import (
	"testing"
	"time"
)

func TestValidatePollInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		wantErr  bool
	}{
		{"zero", 0, true},
		{"negative", -time.Second, true},
		{"positive", time.Millisecond, false},
		{"default", time.Hour, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePollInterval(test.interval)
			if (err != nil) != test.wantErr {
				t.Fatalf("validatePollInterval(%s) = %v, wantErr = %v", test.interval, err, test.wantErr)
			}
			if err != nil && err.Error() != "--zen.poll-interval must be greater than zero" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
