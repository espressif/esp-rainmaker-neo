// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
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

// boolWords are the spellings a boolean arrives in when it is not a JSON bool.
var boolWords = map[string]bool{
	"true": true, "false": false,
	"on": true, "off": false,
	"yes": true, "no": false,
	"1": true, "0": false,
}

// ToNumber reads a number out of a decoded value: JSON's float64 plus the Go numeric types, with NaN and infinities reporting false. Strings are excluded on purpose — parsing one is ParseNumber's job, so a caller asking whether a value is a number is never told a string is one.
func ToNumber(i interface{}) (float64, bool) {
	var number float64
	switch v := i.(type) {
	case float64:
		number = v
	case float32:
		number = float64(v)
	case int:
		number = float64(v)
	case int8:
		number = float64(v)
	case int16:
		number = float64(v)
	case int32:
		number = float64(v)
	case int64:
		number = float64(v)
	case uint:
		number = float64(v)
	case uint8:
		number = float64(v)
	case uint16:
		number = float64(v)
	case uint32:
		number = float64(v)
	case uint64:
		number = float64(v)
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

// ParseNumber reads a number out of its text form, ignoring surrounding space; NaN and infinities report false.
func ParseNumber(text string) (float64, bool) {
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

// ToInt64 narrows a number to an int64, reporting false for a fraction and for anything outside int64, where the conversion is undefined.
func ToInt64(number float64) (int64, bool) {
	if math.Trunc(number) != number {
		return 0, false
	}
	if number < math.MinInt64 || number > math.MaxInt64 {
		return 0, false
	}
	return int64(number), true
}

// ToBool reads a boolean out of a bool, out of the words one is written with ("true", "on", "yes" and their opposites, case- and space-insensitive), or out of the numbers 1 and 0; anything else reports false.
func ToBool(i interface{}) (bool, bool) {
	switch v := i.(type) {
	case bool:
		return v, true
	case string:
		flag, known := boolWords[strings.ToLower(strings.TrimSpace(v))]
		return flag, known
	}
	if number, isNumber := ToNumber(i); isNumber {
		switch number {
		case 1:
			return true, true
		case 0:
			return false, true
		}
	}
	return false, false
}
