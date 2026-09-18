// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package nodelifecycle

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// NodeRegisterHook runs capability-specific work during registration — e.g.
// attaching an IoT policy that a capability owns — and optionally classifies the
// node. The returned node_type is persisted verbatim on the node_details row;
// core never interprets the value.
type NodeRegisterHook func(ctx *rmngctx.RmngContext, nodeID string, capabilities []string, certArn string) (string, error)

var nodeRegisterHook NodeRegisterHook

// RegisterNodeRegisterHook installs the node-register hook. Call it from a
// module's init: the write then happens during package initialisation, before
// main starts and before anything can register a node, so no synchronisation is
// needed.
//
// What "optional" means here, since the hook is now a link-time dependency
// rather than a Lambda discovered by name: a module stays optional to *deploy*
// (core never references its stacks, and an IAM grant naming a table that does
// not exist is inert) and optional at *runtime* (OnNodeRegister does nothing
// until some module registers a hook, and a registered hook is expected to
// return early for capabilities it does not own). It is not optional to
// *link* — importing the module compiles it into the binary whether or not its
// stack group is deployed.
func RegisterNodeRegisterHook(hook NodeRegisterHook) {
	nodeRegisterHook = hook
}

// OnNodeRegister runs the registered hook and returns the node_type it assigned.
// Empty when no capabilities are requested (plain-node registration, including
// the bulk path, does no hook work), when no hook is registered, or when the
// hook assigns no type.
//
// Called during registration after the base IoT policy is attached. A non-nil
// error must fail registration: a hook that cannot complete — a required
// capability policy that could not be attached, say — would leave the node
// unusable.
func OnNodeRegister(ctx *rmngctx.RmngContext, nodeID string, capabilities []string, certArn string) (string, error) {
	if len(capabilities) == 0 || nodeRegisterHook == nil {
		return "", nil
	}
	return nodeRegisterHook(ctx, nodeID, capabilities, certArn)
}
