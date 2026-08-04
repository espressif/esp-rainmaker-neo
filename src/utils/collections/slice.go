// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package collections

func Contains[T comparable](slice []T, item T) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// SlicesEqual checks if two slices contain the same elements (order doesn't matter)
// This is essentially set equality for slices
func SlicesEqual[T comparable](slice1, slice2 []T) bool {
	if len(slice1) != len(slice2) {
		return false
	}

	// Create map for O(1) lookup
	elementMap := make(map[T]bool)
	for _, element := range slice1 {
		elementMap[element] = true
	}

	// Check if all elements in slice2 exist in slice1
	for _, element := range slice2 {
		if !elementMap[element] {
			return false
		}
	}

	return true
}
