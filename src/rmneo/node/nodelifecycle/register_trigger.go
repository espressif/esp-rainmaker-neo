// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package nodelifecycle

import (
	"encoding/json"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/lambdautil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// OnNodeRegisterFunctionName is the conventional name of the synchronous
// node-register hook Lambda. A separately-deployed optional stack creates a
// function with this name to run capability-specific registration work — e.g.
// attaching an IoT policy that the stack owns for a node registered with its
// capability. Core links no downstream code; the hook is discovered by this
// convention name and is a no-op when not deployed.
const OnNodeRegisterFunctionName = "rmng-node-register-hook"

// NodeRegisterEvent is the payload the node-register hook Lambda receives.
type NodeRegisterEvent struct {
	NodeID       string   `json:"node_id"`
	Capabilities []string `json:"capabilities"`
	CertArn      string   `json:"cert_arn"`
}

// NodeRegisterResponse is the payload the node-register hook Lambda returns.
// node_type is an optional classification the hook assigns (e.g. an optional
// stack that owns a capability labels the node so its own downstream code can
// filter on it). Core persists it verbatim on the node_details row and never
// interprets the value — the string "bridge" and friends live entirely in the
// owning stack.
type NodeRegisterResponse struct {
	NodeType string `json:"node_type"`
}

// OnNodeRegister synchronously invokes the node-register hook Lambda
// (OnNodeRegisterFunctionName) and returns the node_type it assigned. The
// node_type is empty when no capabilities are requested — plain-node
// registration (including the bulk-registration path) must not pay a hook
// round-trip — when the hook Lambda is not deployed, or when the hook assigns
// no type.
//
// Called during registration after the base IoT policy is attached. A non-nil
// error must fail registration: a hook that cannot complete (e.g. a required
// capability policy could not be attached) would leave the node unusable.
func OnNodeRegister(ctx *rmngctx.RmngContext, nodeID string, capabilities []string, certArn string) (string, error) {
	if len(capabilities) == 0 {
		return "", nil
	}
	payload := NodeRegisterEvent{
		NodeID:       nodeID,
		Capabilities: capabilities,
		CertArn:      certArn,
	}
	respBytes, err := lambdautil.InvokeSync(ctx.Context, OnNodeRegisterFunctionName, payload)
	if err != nil {
		return "", err
	}
	if len(respBytes) == 0 {
		// Hook not deployed, or returned an empty body.
		return "", nil
	}
	var resp NodeRegisterResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		// A hook that succeeded but returned an unparseable body must not fail
		// registration — the node is provisioned, just left unclassified.
		rlog.Warn(ctx).Err(err).Msg("node-register hook returned an unparseable response; treating node_type as empty")
		return "", nil
	}
	return resp.NodeType, nil
}
