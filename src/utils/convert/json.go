// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"reflect"
)

// computeDelta recursively compares old and new and returns a delta
func ComputeJSONDelta(old, new interface{}) interface{} {
	switch oldVal := old.(type) {
	case map[string]interface{}:
		newVal, ok := new.(map[string]interface{})
		if !ok {
			return new // entire value changed
		}
		delta := make(map[string]interface{})
		for k, newV := range newVal {
			oldV, exists := oldVal[k]
			if !exists || !reflect.DeepEqual(oldV, newV) {
				delta[k] = ComputeJSONDelta(oldV, newV)
			}
		}
		return delta

	case []interface{}:
		// for simplicity, if arrays differ, return new array
		if !reflect.DeepEqual(old, new) {
			return new
		}
		return nil

	default:
		if !reflect.DeepEqual(old, new) {
			return new
		}
		return nil
	}
}
