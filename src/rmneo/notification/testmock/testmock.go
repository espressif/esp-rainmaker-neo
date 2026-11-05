// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package testmock locates the itest webhook mock. The test-infra stack publishes its address and API key in one SSM parameter, so an account without itests (every production deployment) has no mock and nothing here ever routes to one.
package testmock

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

// PARAM_NAME must match SSM_PARAMETERS['WEBHOOK_MOCK'] in test/infra/stacks/test_constants.py.
const PARAM_NAME = "/rmng/test/webhook-mock"

// An answer, found or not, is kept this long: production sends do not query SSM each time, and a mock deployed or replaced while a container is warm is picked up.
const CACHE_TTL = 5 * time.Minute

type Config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

var (
	mu        sync.Mutex
	cached    Config
	found     bool
	checkedAt time.Time
)

// Get returns the account's webhook mock, or false where none is deployed. A failed read is not cached, so one throttled call does not hide the mock for CACHE_TTL.
func Get(ctx context.Context) (Config, bool) {
	mu.Lock()
	if !checkedAt.IsZero() && time.Since(checkedAt) < CACHE_TTL {
		cfg, ok := cached, found
		mu.Unlock()
		return cfg, ok
	}
	mu.Unlock()

	// Read outside the lock: every outbound notification POST passes through here.
	cfg, ok, err := read(ctx)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("failed to read the itest webhook mock parameter")
		return Config{}, false
	}

	mu.Lock()
	cached, found, checkedAt = cfg, ok, time.Now()
	mu.Unlock()
	return cfg, ok
}

// read reports a missing or malformed parameter as no mock, and any other failure as an error.
func read(ctx context.Context) (Config, bool, error) {
	raw, err := ssmutil.GetParameterWithCaching(ctx, PARAM_NAME, false, false)
	if ssmutil.IsParameterNotFound(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, rmerror.NewRMError(err, "failed to get the itest webhook mock parameter")
	}

	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil || cfg.BaseURL == "" {
		rlog.Warn(ctx).Err(err).Msg("ignoring a malformed itest webhook mock parameter")
		return Config{}, false, nil
	}
	return cfg, true, nil
}

// Owns reports whether url is addressed to the mock: "https://x/prod/v1/alexa/data" is owned by base "https://x/prod", "https://x/production" is not.
func (c Config) Owns(url string) bool {
	return c.BaseURL != "" && strings.HasPrefix(url, strings.TrimSuffix(c.BaseURL, "/")+"/")
}

// URL joins a mock path onto the base: URL("/v1/alexa/data").
func (c Config) URL(path string) string {
	return strings.TrimSuffix(c.BaseURL, "/") + path
}

// SetAPIKey adds the mock's API key to req when, and only when, req is addressed to the mock.
func SetAPIKey(ctx context.Context, req *http.Request) {
	if cfg, ok := Get(ctx); ok && cfg.Owns(req.URL.String()) {
		req.Header.Set("x-api-key", cfg.APIKey)
	}
}

// Reset forgets the cached answer. Tests that seed or delete the parameter call it.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	checkedAt = time.Time{}
}
