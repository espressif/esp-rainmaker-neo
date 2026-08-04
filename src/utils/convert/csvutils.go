// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"encoding/csv"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"strings"
)

func ReadCSVToStruct(csvFileContent []byte) ([]map[string]string, error) {
	_, results, err := ReadCSVToStructWithHeaders(csvFileContent)
	return results, err
}

// ReadCSVToStructWithHeaders parses a CSV into per-row maps and also returns
// the (trimmed) header row in its original column order. The row maps lose
// column order, so callers that need to reproduce the input layout — e.g. the
// bulk container writing a filtered failed-rows CSV — use the header to drive
// emission order.
func ReadCSVToStructWithHeaders(csvFileContent []byte) ([]string, []map[string]string, error) {
	reader := csv.NewReader(bytes.NewReader(csvFileContent))
	var results []map[string]string

	// Read headers
	headers, err := reader.Read()
	if err != nil {
		return nil, nil, rmerror.NewRMError(err, "failed to read CSV header")
	}

	// Clean headers
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}

	// Read and parse each record
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, rmerror.NewRMError(err, "failed to read CSV record")
		}
		if len(record) == 0 {
			continue
		}

		// Create map for current record
		rowMap := make(map[string]string)
		for i, value := range record {
			if i < len(headers) {
				value = strings.TrimSpace(value)
				if value != "" {
					rowMap[headers[i]] = value
				}
			}
		}

		results = append(results, rowMap)
	}

	return headers, results, nil
}
