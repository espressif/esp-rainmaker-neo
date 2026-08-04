// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"errors"
	"reflect"
)

func InterfaceToSlice(slice interface{}) ([]interface{}, error) {
	sliceVal := reflect.ValueOf(slice)

	if sliceVal.Kind() != reflect.Slice {
		return make([]interface{}, 0), errors.New("Invalid data type")
	}

	interfaceSlice := make([]interface{}, sliceVal.Len())

	for index := 0; index < sliceVal.Len(); index++ {
		interfaceSlice[index] = sliceVal.Index(index).Interface()
	}
	return interfaceSlice, nil
}
