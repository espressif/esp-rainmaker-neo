// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
)

// parsePresenceEvent parses an SQS message body into a node.PresenceEvent.
// The SQS message body contains the JSON-encoded presence event from IoT Core rules.
func parsePresenceEvent(body string) (node.PresenceEvent, error) {
	var event node.PresenceEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return node.PresenceEvent{}, fmt.Errorf("failed to unmarshal presence event: %w", err)
	}

	if event.ClientID == "" {
		return node.PresenceEvent{}, fmt.Errorf("missing required field: clientId")
	}
	if event.EventType == "" {
		return node.PresenceEvent{}, fmt.Errorf("missing required field: eventType")
	}

	return event, nil
}
