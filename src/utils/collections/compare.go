// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package collections

import (
	"encoding/json"
)

func StructsEqual(a, b interface{}) bool {
	jsonA, _ := json.Marshal(a)
	jsonB, _ := json.Marshal(b)
	return string(jsonA) == string(jsonB)
}
