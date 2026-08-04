// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package httpclient owns the process-wide outbound HTTP client so every outbound call shares one
// timeout and one seam tests can replace. It depends on nothing else in the tree, which is what
// lets low-level packages (jwtutils) make network calls without importing the utils grab-bag.
package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Client is the outbound HTTP surface. An interface so tests substitute a transport-level double.
type Client interface {
	Do(req *http.Request) (res *http.Response, err error)
	Get(url string) (res *http.Response, err error)
	Post(urstring, bodyType string, body io.Reader) (res *http.Response, err error)
	PostForm(url string, values url.Values) (res *http.Response, err error)
	Head(url string) (res *http.Response, err error)
}

// defaultTimeout keeps a hung outbound call (webhook, OAuth refresh, LWA profile) from burning the
// whole Lambda invocation budget.
const defaultTimeout = 10 * time.Second

var (
	mu      sync.RWMutex
	current Client
)

// Set replaces the shared client.
func Set(c Client) {
	mu.Lock()
	current = c
	mu.Unlock()
}

// Get returns the shared client, building the default on first use so a caller never gets nil
// because initialization order put it ahead of the setup path.
func Get() Client {
	mu.RLock()
	c := current
	mu.RUnlock()
	if c != nil {
		return c
	}
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		current = &http.Client{Timeout: defaultTimeout}
	}
	return current
}

// FetchBounded GETs url and reads at most maxBytes of the body, so a hostile or misbehaving peer
// cannot stream unbounded data into memory. A nil client uses the shared one.
func FetchBounded(ctx context.Context, url string, c Client, maxBytes int64) ([]byte, error) {
	if c == nil {
		c = Get()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("httpclient: build request for %s: %w", url, err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpclient: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("httpclient: GET %s returned %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("httpclient: read %s: %w", url, err)
	}
	return body, nil
}
