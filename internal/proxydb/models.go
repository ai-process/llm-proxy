// Package proxydb is the pgx data layer: api keys, the model registry, routing
// rules, encrypted vendor keys, and the config version counter.
package proxydb

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("proxydb: not found")

type APIKey struct {
	ID         string
	Name       string
	KeyLookup  string
	KeyHash    string
	Scopes     []string
	DisabledAt *time.Time
	CreatedAt  time.Time
}

func (k *APIKey) Active() bool { return k.DisabledAt == nil }

type Model struct {
	ID              string
	Vendor          string
	Endpoint        string
	Efforts         []string
	Capabilities    []string
	RPM             int32
	PriceInPerMtok  float64
	PriceOutPerMtok float64
	// Peak rates, 0 when the vendor bills one price around the clock. DeepSeek
	// V4 doubles for two windows a day, so one price understates its cost.
	PriceInPeakPerMtok  float64
	PriceOutPeakPerMtok float64
	DailyTokensPerKey   int64
	DailyTokensPerUser  int64
	Enabled             bool
}

type Rule struct {
	ID         string
	Name       string
	Priority   int32
	Effort     string // "" = any
	Attributes map[string]string
	Enabled    bool
	Use        []string // ordered model ids
}

type VendorKey struct {
	ID            string
	APIKeyID      string
	APIKeyName    string // joined for admin listings
	Vendor        string
	KeyCiphertext []byte
	KeyVersion    int32
	CreatedAt     time.Time
}
