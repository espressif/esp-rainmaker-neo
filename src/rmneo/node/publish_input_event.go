// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node

// PublishInputEvent is the wire shape produced by IoT topic rules with
// the projection `SELECT topic(3) as thing_name, * as data FROM ...`.
// Produced by the core `node_to_cloud_rule` → publish_input_event_handler
// (filter `rainmaker/nodes/+/to_cloud`), and reusable by any optional rule
// that keeps the same SELECT projection so this struct can decode its
// input too.
type PublishInputEvent struct {
	ThingName string                 `json:"thing_name"`
	Data      map[string]interface{} `json:"data"`
}
