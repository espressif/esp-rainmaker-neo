// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package timeutil

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strconv"
)

// millisTimestampDigits is the digit width of a millisecond Unix timestamp; shorter values (e.g. seconds) are padded up to it.
const millisTimestampDigits = 13

// NormalizeTimestampMs returns a Unix timestamp string in milliseconds, right-padding shorter (e.g. second-precision) values so both units are accepted; empty yields 0 (no bound).
func NormalizeTimestampMs(raw, paramName string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, rmerror.NewRMError(err, fmt.Sprintf("invalid %s parameter", paramName))
	}
	if ts < 0 {
		return 0, rmerror.NewRMError(nil, fmt.Sprintf("%s must be a non-negative Unix timestamp", paramName))
	}
	for ts > 0 && len(strconv.FormatInt(ts, 10)) < millisTimestampDigits {
		ts *= 10
	}
	return ts, nil
}
