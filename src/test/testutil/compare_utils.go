// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
)

// Helper function to recursively normalize a map or list by sorting list
func NormalizeObject(data interface{}) interface{} {
	var okMap bool
	var okArray bool
	// Try to marshal and unmarshal to map[string]interface{}
	var mapdata map[string]interface{}
	if jsonBytes, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(jsonBytes, &mapdata); err == nil {
			okMap = true
		}
	}

	// Try to marshal and unmarshal to []interface{}
	var interArray []interface{}
	if jsonBytes, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(jsonBytes, &interArray); err == nil {
			okArray = true
		}
	}

	switch {
	case okMap:
		normalized := make(map[string]interface{})
		for key, value := range mapdata {
			normalized[key] = NormalizeObject(value)
		}
		return normalized
	case okArray:
		normalized := make([]interface{}, len(interArray))
		for i, value := range interArray {
			normalized[i] = NormalizeObject(value)
		}
		// Sort lists of comparable elements
		sort.Slice(normalized, func(i, j int) bool {
			return fmt.Sprintf("%v", normalized[i]) < fmt.Sprintf("%v", normalized[j])
		})
		return normalized
	default:
		return data
	}
}

func AssertNormalizedEqual(expected, actual interface{}) {
	if diff := cmp.Diff(NormalizeObject(expected), NormalizeObject(actual)); diff != "" {
		/* Quite frustrated with the transient test failure that happens in testFormation.
		 * Adding this information first, to help debugging that issue, coz otherwise, I can't
		 * make any heads or tails out of it.
		 */
		rlog.Error(context.TODO()).Msg("Comparison failed. Additional Information by using normal comparison")
		rlog.Error(context.TODO()).Interface("Expected", expected).Send()
		rlog.Error(context.TODO()).Interface("Actual", actual).Send()
		gomega.ExpectWithOffset(1, diff).To(gomega.BeEmpty())
	}
}
