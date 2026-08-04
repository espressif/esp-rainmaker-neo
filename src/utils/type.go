// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/json"
	"reflect"
	"strconv"
)

//For all type conversions

func Ptr[T any](v T) *T {
	return &v
}

// ConvertAnyToAny converts between different types using JSON marshaling/unmarshaling
// This function supports the following conversions:
// - Map to struct
// - Struct to struct
// - Struct to map
// - Map to map
//
// Note:
// This function does not overwrite fields in the target that are not present in the source. Like MERGES both.
// All numbers are unmarshaled to float64 when using map as the target.
// If you convert from map to struct, the extra fields will be LOST
func ConvertAnyToAny(source any, ptrToTarget any) error {
	if source == nil {
		return nil
	}
	if ptrToTarget == nil {
		return nil
	}

	// Try to marshal the map
	byteStr, errMarshal := json.Marshal(source)
	if errMarshal != nil {
		return errMarshal
	}

	// Try to convert the map to the given struct
	errUnmarshal := json.Unmarshal(byteStr, ptrToTarget)
	if errUnmarshal != nil {
		return errUnmarshal
	}
	return nil
}

// PtrValue returns the value pointed to by v or the zero value if v is nil
func PtrValue[T any](v *T) T {
	if v == nil {
		var zero T
		return zero
	}
	return *v
}

// GetOptional returns the first element of a slice or the zero value if the slice is empty
func GetOptional[T any](v []T) T {
	if len(v) == 0 {
		var zero T
		return zero
	}
	return v[0]
}

func IsEmpty[T any](v T) bool {
	zeroValue := reflect.Zero(reflect.TypeOf(v)).Interface()
	return reflect.DeepEqual(v, zeroValue)
}

func ToString(i interface{}) string {
	switch v := i.(type) {
	case int, int8, int16, int32, int64:
		return strconv.Itoa(v.(int))
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(uint64(v.(uint)), 10)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case string:
		return v
	default:
		tmp, _ := json.Marshal(v)
		return string(tmp)
	}
}
