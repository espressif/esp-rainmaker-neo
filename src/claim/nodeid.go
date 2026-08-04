// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package claim

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/google/uuid"
)

// NodeIDLength is the fixed width of a claimed node ID: the 36 characters of a
// canonical RFC 4122 UUID (32 hex digits plus four hyphens).
const NodeIDLength = 36

// GenerateNodeID returns a cloud-assigned node ID: a canonical RFC 4122
// version-4 UUID in the standard hyphenated lowercase form, e.g.
// "1b4e28ba-2fa1-4d3b-a3f5-ce6f8a9b0c2d".
//
// This is the same format the DAC and RainMaker pre-provisioning services emit,
// so a node's ID reads identically whichever path minted it. One value serves as
// the IoT Thing name, the MQTT client ID and the certificate Common Name.
//
// A claimed node that later joins a Matter fabric has its Matter operational
// Node ID derived from this value exactly as it is for every other node whose ID
// is not already 16 hex characters (group.MatterNodeIDFromThingName). The node
// ID is not itself a Matter Node ID — that identity is fabric-scoped and assigned
// during association, not at claim time.
func GenerateNodeID() (string, error) {
	// NewRandom surfaces the (very rare) entropy failure; uuid.New would panic
	// on it instead.
	u, err := uuid.NewRandom()
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to generate node ID")
	}
	return u.String(), nil
}
