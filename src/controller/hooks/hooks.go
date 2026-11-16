// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package hooks registers the controller's node-register hook from init(); binaries that register nodes blank-import it, so core never references the controller.
package hooks

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func init() {
	nodelifecycle.RegisterNodeRegisterHook(OnNodeRegister)
}

var controllerPolicy = nodelifecycle.CapabilityPolicy{
	Capability:        "controller",
	PolicyEnvVar:      "CONTROLLER_POLICY_NAME",
	DefaultPolicyName: "rmng-controller-policy",
}

// OnNodeRegister attaches the controller IoT policy to a node registering with the "controller" capability. It assigns no node_type: the session mint reads reg_capabilities, so a controller never collides with a capability that does classify.
func OnNodeRegister(rmngCtx *rmngctx.RmngContext, nodeID string, capabilities []string, certArn string) (string, error) {
	_, err := controllerPolicy.Attach(rmngCtx, capabilities, certArn)
	return "", err
}
