// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// rejectDuplicateKeys fails a batch that names one key twice, as the service does with a ValidationException.
func (m *DynamoDBMock) rejectDuplicateKeys(table string, keys []map[string]types.AttributeValue) error {
	details := m.tables[table]
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		pkey, skey, err := ExtractKeys(key, details.PrimaryKey, details.SortKey)
		if err != nil {
			return err
		}
		id := pkey + "\x00" + skey
		if seen[id] {
			return fmt.Errorf("ValidationException: Provided list of item keys contains duplicates")
		}
		seen[id] = true
	}
	return nil
}
