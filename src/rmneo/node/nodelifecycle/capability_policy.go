// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package nodelifecycle

import (
	"errors"
	"fmt"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	"github.com/aws/smithy-go"
)

// CapabilityPolicy is the IoT policy a capability module attaches to the cert of a node registering with its capability.
type CapabilityPolicy struct {
	Capability string
	// PolicyEnvVar overrides DefaultPolicyName when set.
	PolicyEnvVar      string
	DefaultPolicyName string
}

// Attach attaches the policy when capabilities include p.Capability and reports whether it did. A missing policy means the module's stack is not deployed and yields *CapabilityUnavailableError.
func (p CapabilityPolicy) Attach(rmngCtx *rmngctx.RmngContext, capabilities []string, certArn string) (bool, error) {
	if !iotutil.HasCapability(capabilities, p.Capability) {
		return false, nil
	}
	policyName := os.Getenv(p.PolicyEnvVar)
	if policyName == "" {
		policyName = p.DefaultPolicyName
	}

	// AttachPolicy is idempotent, which a re-claim against a replacement cert relies on.
	_, err := awscommon.GetIoTClient().AttachPolicy(rmngCtx.Context, &iot.AttachPolicyInput{
		PolicyName: aws.String(policyName),
		Target:     aws.String(certArn),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
			return false, &CapabilityUnavailableError{Capability: p.Capability, Err: err}
		}
		return false, fmt.Errorf("failed to attach %s policy %q to %q: %w", p.Capability, policyName, certArn, err)
	}

	rlog.Info(rmngCtx).Str("policy", policyName).Str("certArn", certArn).Msgf("register_hook: %s policy attached", p.Capability)
	return true, nil
}
