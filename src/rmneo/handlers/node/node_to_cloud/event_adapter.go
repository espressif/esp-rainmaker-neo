// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
)

// parsePublishInputEvent parses an SQS message body into a PublishInputEvent
// The SQS message body contains the JSON-encoded publish input event from IoT Core rules
func parsePublishInputEvent(body string) (node.PublishInputEvent, error) {
	var event node.PublishInputEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return node.PublishInputEvent{}, fmt.Errorf("failed to unmarshal publish input event: %w", err)
	}

	// Validate required fields
	if event.ThingName == "" {
		return node.PublishInputEvent{}, fmt.Errorf("missing required field: thing_name")
	}
	if event.Data == nil {
		return node.PublishInputEvent{}, fmt.Errorf("missing required field: data")
	}

	return event, nil
}
