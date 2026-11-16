// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package principal builds the RBAC context for a data API request: a user from their Cognito identity, or a device from the certificate behind its IoT credential-provider credentials.
package principal

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
)

// FromAPIRequest builds the RBAC context for whoever made the request. A nil result means the caller could not be identified and the handler should answer 401.
//
// App users sign with Identity Pool credentials, which carry a Cognito identity; any other IAM signer is resolved as a device and never falls through to the user path.
func FromAPIRequest(ctx context.Context, request events.APIGatewayProxyRequest) *rmngctx.RmngContext {
	if identity := request.RequestContext.Identity; signedByNonCognitoPrincipal(identity) {
		return fromDeviceIdentity(ctx, identity)
	}
	return user.NewContextWithAPIRequest(ctx, request)
}

func signedByNonCognitoPrincipal(identity events.APIGatewayRequestIdentity) bool {
	return identity.UserArn != "" && identity.CognitoIdentityID == "" && identity.CognitoAuthenticationProvider == ""
}
