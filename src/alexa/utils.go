// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

const alexaEndpointIDSeparator = "#"

func storeSSMParameter(ctx context.Context, name, value string) error {
	return ssmutil.StoreParameter(ctx, name, value)
}

func StoreAlexaClientDetails(ctx context.Context, clientID, clientSecret, skillID string) error {
	err := storeSSMParameter(ctx, AlexaSSMClientIDParam, clientID)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to store client ID")
	}

	err = storeSSMParameter(ctx, AlexaSSMClientSecretParam, clientSecret)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to store client secret")
	}

	err = storeSSMParameter(ctx, AlexaSSMSkillIDParam, skillID)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to store skill ID")
	}

	return nil
}

func GetAlexaClientDetails(ctx context.Context) (string, string, error) {
	alexaClientID, err := ssmutil.GetParameterWithCaching(ctx, AlexaSSMClientIDParam, true, false) // Not caching, as on delete, we need to clear the cache which we haven't handled yet
	if err != nil {
		return "", "", rmerror.NewRMError(err, "failed to get client id")
	}

	alexaClientSecret, err := ssmutil.GetParameterWithCaching(ctx, AlexaSSMClientSecretParam, true, false) // Not caching, as on delete, we need to clear the cache which we haven't handled yet
	if err != nil {
		return "", "", rmerror.NewRMError(err, "failed to get client secret")
	}

	return alexaClientID, alexaClientSecret, nil
}

// StoreAlexaManufacturerName persists the brand advertised in Alexa discovery. Unlike the
// client credentials it is stored as a plain String, not a SecureString: the value is public
// by construction, since it ships in every discovery response.
//
// An empty name clears the override so the default brand applies again. That has to delete the
// parameter rather than store "": SSM rejects an empty value (PutParameter requires length >= 1),
// so storing one fails the whole request.
func StoreAlexaManufacturerName(ctx context.Context, manufacturerName string) error {
	if manufacturerName == "" {
		// Reads are cached, so clear before deleting: a cached value would otherwise outlive the
		// parameter it came from and keep being served.
		ssmutil.ClearCachedParameter(AlexaSSMManufacturerNameParam)
		if err := ssmutil.DeleteParameter(ctx, AlexaSSMManufacturerNameParam); err != nil {
			// Nothing to clear is the expected state for a deployment that never set a brand.
			if !isParameterNotFound(err) {
				return rmerror.NewRMError(err, "Failed to clear manufacturer name")
			}
		}

		return nil
	}

	err := ssmutil.StoreParameterWithType(ctx, AlexaSSMManufacturerNameParam, manufacturerName, ssm_types.ParameterTypeString)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to store manufacturer name")
	}

	// Reads are cached, so drop the old value: otherwise a GET served by the same warm container
	// would keep reporting the brand this call just replaced.
	ssmutil.ClearCachedParameter(AlexaSSMManufacturerNameParam)

	return nil
}

// isParameterNotFound reports whether an error is SSM's "no such parameter", which callers that
// delete best-effort treat as success.
func isParameterNotFound(err error) bool {
	var notFound *ssm_types.ParameterNotFound
	return errors.As(err, &notFound)
}

// GetAlexaManufacturerName returns the configured brand, falling back to
// DefaultManufacturerName when none is set. An absent parameter is the expected state for a
// deployment that was never rebranded, so this never fails the caller — discovery must not
// break over branding. Cached (unlike the client credentials, which must reflect a rotation
// immediately) because the brand is set once per deployment; a change to it takes effect as
// lambda containers recycle.
func GetAlexaManufacturerName(ctx context.Context) string {
	manufacturerName, err := ssmutil.GetParameterWithCaching(ctx, AlexaSSMManufacturerNameParam, false)
	if err != nil {
		rlog.Debug(ctx).Err(err).Msg("no manufacturer name configured, using the default brand")
	}
	if manufacturerName == "" {
		return DefaultManufacturerName
	}

	return manufacturerName
}

func GetUserNodeFromRequest(ctx context.Context, request AlexaRequest) (*rmngctx.RmngContext, *node.Node, string, error) {
	endpointID := request.Directive.Endpoint.EndpointID
	parts := strings.Split(endpointID, alexaEndpointIDSeparator)
	if len(parts) != 2 {
		return nil, nil, "", fmt.Errorf("invalid endpointID: %s", endpointID)
	}
	nodeID := parts[0]
	deviceName := parts[1]

	groupID, ok := request.Directive.Endpoint.Cookie["groupID"].(string)
	if !ok {
		return nil, nil, "", fmt.Errorf("missing groupID in cookie")
	}

	userID, err := user.GetUserIDFromToken(ctx, request.Directive.Endpoint.Scope.Token)
	if err != nil {
		return nil, nil, "", rmerror.NewRMError(err, "failed to get identity id")
	}
	userCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	err = user.LoadNodePermissions(userCtx, groupID, nodeID)
	if err != nil {
		return nil, nil, "", rmerror.NewRMError(err, "failed to load node permissions")
	}

	node := node.NewNode(nodeID)

	return userCtx, node, deviceName, nil
}

func GetEndpointId(nodeID, deviceName string) string {
	return fmt.Sprintf("%s%s%s", nodeID, alexaEndpointIDSeparator, deviceName)
}
