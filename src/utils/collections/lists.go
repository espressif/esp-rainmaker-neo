// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package collections

import (
	"reflect"

	"github.com/espressif/esp-cloud-common/go/rbac/pkg/logger"
)

// SubtractListBFromListA returns the items in listA that are not in listB.
// It uses reflect.DeepEqual rather than == so elements may hold uncomparable
// values (e.g. Alexa ContextProperty.Value can be a map such as a light's
// color spectrumHsv); == on such a value panics at runtime with
// "comparing uncomparable type map[string]interface {}".
func SubtractListBFromListA[T any](listA, listB []T) []T {
	filteredList := []T{}
	for _, item := range listA {
		found := false
		for _, itemB := range listB {
			if reflect.DeepEqual(item, itemB) {
				found = true
				break
			}
		}
		if !found {
			filteredList = append(filteredList, item)
		}
	}
	return filteredList
}

func ItemExists[T comparable](slice []T, item T) (exists bool, index int) {
	for index = 0; index < len(slice); index++ {
		if slice[index] == item {
			logger.LogDebug("Items exists in slice")
			return true, index
		}
	}
	logger.LogDebug("Item does not exists in slice")
	return false, -1
}
