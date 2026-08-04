// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package cognitoutil

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
)

// Placeholder URL used during CDK deployment to satisfy validation requirements
const PlaceholderCallbackURL = "https://placeholder.example.com/callback"

// GetCognitoAppClientURLs retrieves the callback URLs configured for a Cognito app client
func GetCognitoAppClientURLs(ctx context.Context, userPoolID, appClientID string) ([]string, error) {
	cognitoClient := awscommon.GetCognitoProviderClient()

	describeOutput, err := cognitoClient.DescribeUserPoolClient(ctx, &cognitoidentityprovider.DescribeUserPoolClientInput{
		UserPoolId: aws.String(userPoolID),
		ClientId:   aws.String(appClientID),
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to describe Cognito app client")
	}

	return describeOutput.UserPoolClient.CallbackURLs, nil
}

// UpdateCognitoAppClientURLs updates the callback and logout URLs for a Cognito app client
// It appends new URLs to existing ones and avoids duplicates
func UpdateCognitoAppClientURLs(ctx context.Context, userPoolID, appClientID string, redirectURIs []string, logoutURIs []string) error {
	cognitoClient := awscommon.GetCognitoProviderClient()

	// Get current app client configuration
	describeUserPoolClientInput := &cognitoidentityprovider.DescribeUserPoolClientInput{
		UserPoolId: aws.String(userPoolID),
		ClientId:   aws.String(appClientID),
	}
	describeUserPoolClientOutput, err := cognitoClient.DescribeUserPoolClient(ctx, describeUserPoolClientInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to describe Cognito app client")
	}

	// Get existing callback URLs and logout URLs
	var existingCallbackURLs []string
	var existingLogoutURLs []string

	if describeUserPoolClientOutput.UserPoolClient.CallbackURLs != nil {
		existingCallbackURLs = describeUserPoolClientOutput.UserPoolClient.CallbackURLs
	}
	if describeUserPoolClientOutput.UserPoolClient.LogoutURLs != nil {
		existingLogoutURLs = describeUserPoolClientOutput.UserPoolClient.LogoutURLs
	}

	// Filter out placeholder URLs and append new redirect URIs (avoid duplicates)
	var updatedCallbackURLs []string

	// Add existing URLs that are not placeholders
	for _, existingURI := range existingCallbackURLs {
		if existingURI != PlaceholderCallbackURL {
			updatedCallbackURLs = append(updatedCallbackURLs, existingURI)
		}
	}

	// Add new redirect URIs (avoid duplicates)
	for _, newURI := range redirectURIs {
		found := false
		for _, existingURI := range updatedCallbackURLs {
			if existingURI == newURI {
				found = true
				break
			}
		}
		if !found {
			updatedCallbackURLs = append(updatedCallbackURLs, newURI)
		}
	}

	// Append new logout URIs to existing ones (avoid duplicates)
	updatedLogoutURLs := make([]string, len(existingLogoutURLs))
	copy(updatedLogoutURLs, existingLogoutURLs)

	// Add new logout URIs (avoid duplicates)
	for _, newURI := range logoutURIs {
		found := false
		for _, existingURI := range existingLogoutURLs {
			if existingURI == newURI {
				found = true
				break
			}
		}
		if !found {
			updatedLogoutURLs = append(updatedLogoutURLs, newURI)
		}
	}

	// UpdateUserPoolClient is a full replacement: any field left unset here is
	// reset to its Cognito default. Omitting WriteAttributes/ReadAttributes in
	// particular resets them to "all standard attributes", which would let a
	// user overwrite custom:user_id via their own access token and take over
	// another tenant. So we echo the client's full described
	// configuration and change only the callback/logout URLs.
	client := describeUserPoolClientOutput.UserPoolClient
	updateUserPoolClientInput := &cognitoidentityprovider.UpdateUserPoolClientInput{
		UserPoolId:                               aws.String(userPoolID),
		ClientId:                                 aws.String(appClientID),
		CallbackURLs:                             updatedCallbackURLs,
		LogoutURLs:                               updatedLogoutURLs,
		AccessTokenValidity:                      client.AccessTokenValidity,
		AllowedOAuthFlows:                        client.AllowedOAuthFlows,
		AllowedOAuthFlowsUserPoolClient:          aws.ToBool(client.AllowedOAuthFlowsUserPoolClient),
		AllowedOAuthScopes:                       client.AllowedOAuthScopes,
		AnalyticsConfiguration:                   client.AnalyticsConfiguration,
		AuthSessionValidity:                      client.AuthSessionValidity,
		ClientName:                               client.ClientName,
		DefaultRedirectURI:                       client.DefaultRedirectURI,
		EnablePropagateAdditionalUserContextData: client.EnablePropagateAdditionalUserContextData,
		EnableTokenRevocation:                    client.EnableTokenRevocation,
		ExplicitAuthFlows:                        client.ExplicitAuthFlows,
		IdTokenValidity:                          client.IdTokenValidity,
		PreventUserExistenceErrors:               client.PreventUserExistenceErrors,
		ReadAttributes:                           client.ReadAttributes,
		RefreshTokenValidity:                     client.RefreshTokenValidity,
		SupportedIdentityProviders:               client.SupportedIdentityProviders,
		TokenValidityUnits:                       client.TokenValidityUnits,
		WriteAttributes:                          client.WriteAttributes,
	}

	_, err = cognitoClient.UpdateUserPoolClient(ctx, updateUserPoolClientInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update Cognito app client")
	}
	return nil
}
