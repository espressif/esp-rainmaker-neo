// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package nodelifecycle

import (
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// CapabilityUnavailableError reports a requested capability whose optional stack is not deployed; handlers answer it with 400, not 500.
type CapabilityUnavailableError struct {
	Capability string
	Err        error
}

func (e *CapabilityUnavailableError) Error() string {
	return fmt.Sprintf("capability %q is not available on this deployment", e.Capability)
}

func (e *CapabilityUnavailableError) Unwrap() error {
	return e.Err
}

// NodeRegisterHook runs capability-specific work during registration — e.g.
// attaching an IoT policy that a capability owns — and optionally classifies the
// node. The returned node_type is persisted verbatim on the node_details row;
// core never interprets the value.
type NodeRegisterHook func(ctx *rmngctx.RmngContext, nodeID string, capabilities []string, certArn string) (string, error)

var nodeRegisterHooks []NodeRegisterHook

// RegisterNodeRegisterHook adds a node-register hook; hooks accumulate, and nil clears them (tests only). Call it from a
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
	if hook == nil {
		nodeRegisterHooks = nil
		return
	}
	nodeRegisterHooks = append(nodeRegisterHooks, hook)
}

// OnNodeRegister runs every hook in registration order and returns the first non-empty node_type.
// Empty when no capabilities are requested (plain-node registration, including
// the bulk path, does no hook work), when no hook is registered, or when no
// hook assigns a type.
//
// Called during registration after the base IoT policy is attached. A non-nil
// error must fail registration: a hook that cannot complete — a required
// capability policy that could not be attached, say — would leave the node
// unusable.
func OnNodeRegister(ctx *rmngctx.RmngContext, nodeID string, capabilities []string, certArn string) (string, error) {
	if len(capabilities) == 0 || len(nodeRegisterHooks) == 0 {
		return "", nil
	}
	nodeType := ""
	for _, hook := range nodeRegisterHooks {
		assigned, err := hook(ctx, nodeID, capabilities, certArn)
		if err != nil {
			return "", err
		}
		if assigned == "" {
			continue
		}
		if nodeType == "" {
			nodeType = assigned
			continue
		}
		// node_details holds one node_type; a second classification is a misconfiguration to log, not a reason to fail a registration whose policies all attached.
		rlog.Warn(ctx).Str("nid", nodeID).Str("kept", nodeType).Str("ignored", assigned).
			Msg("two node-register hooks classified the same node")
	}
	return nodeType, nil
}
