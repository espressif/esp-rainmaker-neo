// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import "time"

// SetBackoffDelay makes every batch retry wait d, for tests, and returns a func restoring the SDK backoff.
func SetBackoffDelay(d time.Duration) (restore func()) {
	old := backoffDelay
	backoffDelay = func(int, error) (time.Duration, error) { return d, nil }
	return func() { backoffDelay = old }
}
