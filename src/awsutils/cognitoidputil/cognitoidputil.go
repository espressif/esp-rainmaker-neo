// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package cognitoidputil provides Cognito Identity Pool utilities
package cognitoidputil

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	"github.com/golang-jwt/jwt/v5"
)

// CognitoIdentityPoolService provides abstraction for Cognito Identity Pool operations
type CognitoIdentityPoolService struct {
	identityClient awscommon.CognitoIdentityInterface
	IdentityPoolID string
}

// NewCognitoIdentityPoolService creates a new Cognito Identity Pool service instance
func NewCognitoIdentityPoolService(identityPoolID string) *CognitoIdentityPoolService {
	return &CognitoIdentityPoolService{
		identityClient: awscommon.GetCognitoIdentityClient(),
		IdentityPoolID: identityPoolID,
	}
}

// GetIdentityIDRequest represents request for getting identity ID
type GetIdentityIDRequest struct {
	IDToken string
	Issuer  string
}

// GetIdentityIDResponse represents response for getting identity ID
type GetIdentityIDResponse struct {
	IdentityID string
}

// GetIdentityID retrieves the Cognito Identity Pool identity ID for a user
func (c *CognitoIdentityPoolService) GetIdentityID(ctx context.Context, req *GetIdentityIDRequest) (*GetIdentityIDResponse, error) {
	if c.identityClient == nil {
		return nil, rmerror.NewRMError(nil, "Identity client is not initialized")
	}

	if c.IdentityPoolID == "" {
		return nil, rmerror.NewRMError(nil, "Identity Pool ID is not set")
	}

	if req.Issuer == "" {
		return nil, rmerror.NewRMError(nil, "Issuer is required")
	}

	loginKey := issuerToLoginKey(req.Issuer)
	logins := map[string]string{
		loginKey: req.IDToken,
	}

	getIDInput := &cognitoidentity.GetIdInput{
		IdentityPoolId: aws.String(c.IdentityPoolID),
		Logins:         logins,
	}

	getIDOutput, err := c.identityClient.GetId(ctx, getIDInput)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get identity ID")
	}

	return &GetIdentityIDResponse{
		IdentityID: *getIDOutput.IdentityId,
	}, nil
}

// GetCredentialsRequest represents request for getting AWS credentials
type GetCredentialsRequest struct {
	IdentityID string
	IDToken    string
	Issuer     string
}

// GetCredentialsResponse represents response for getting AWS credentials
type GetCredentialsResponse struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      int64
}

// issuerToLoginKey converts issuer URL to login key format
// Input: https://cognito-idp.{region}.amazonaws.com/{userPoolID}
// Output: cognito-idp.{region}.amazonaws.com/{userPoolID}
func issuerToLoginKey(issuer string) string {
	return strings.TrimPrefix(issuer, "https://")
}

// GetCredentialsForIdentity retrieves AWS credentials for a Cognito Identity Pool identity
func (c *CognitoIdentityPoolService) GetCredentialsForIdentity(ctx context.Context, req *GetCredentialsRequest) (*GetCredentialsResponse, error) {
	if c.identityClient == nil {
		return nil, rmerror.NewRMError(nil, "Identity client is not initialized")
	}

	if req.Issuer == "" {
		return nil, rmerror.NewRMError(nil, "Issuer is required")
	}

	loginKey := issuerToLoginKey(req.Issuer)
	logins := map[string]string{
		loginKey: req.IDToken,
	}

	getCredentialsInput := &cognitoidentity.GetCredentialsForIdentityInput{
		IdentityId: aws.String(req.IdentityID),
		Logins:     logins,
	}

	getCredentialsOutput, err := c.identityClient.GetCredentialsForIdentity(ctx, getCredentialsInput)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get credentials")
	}

	credentials := getCredentialsOutput.Credentials
	if credentials == nil {
		return nil, rmerror.NewRMError(nil, "No credentials returned")
	}

	return &GetCredentialsResponse{
		AccessKeyID:     *credentials.AccessKeyId,
		SecretAccessKey: *credentials.SecretKey,
		SessionToken:    *credentials.SessionToken,
		Expiration:      credentials.Expiration.Unix(),
	}, nil
}

// extractIssuerFromToken extracts issuer from ID token
func extractIssuerFromToken(idToken string) (string, error) {
	token, _, parseErr := new(jwt.Parser).ParseUnverified(idToken, jwt.MapClaims{})
	if parseErr != nil {
		return "", rmerror.NewRMError(parseErr, "Failed to parse ID token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", rmerror.NewRMError(nil, "Invalid token claims")
	}

	issuer, ok := claims["iss"].(string)
	if !ok || issuer == "" {
		return "", rmerror.NewRMError(nil, "Issuer claim not found in token")
	}

	return issuer, nil
}

// GetUserCredentials is a convenience method that combines GetIdentityID and GetCredentialsForIdentity
// It extracts issuer from the ID token's issuer claim, so it can be passed directly
func (c *CognitoIdentityPoolService) GetUserCredentials(ctx context.Context, idToken string) (*GetCredentialsResponse, error) {
	issuer, err := extractIssuerFromToken(idToken)
	if err != nil {
		return nil, err
	}

	getIDReq := &GetIdentityIDRequest{
		IDToken: idToken,
		Issuer:  issuer,
	}

	getIDResp, err := c.GetIdentityID(ctx, getIDReq)
	if err != nil {
		return nil, err
	}

	getCredsReq := &GetCredentialsRequest{
		IdentityID: getIDResp.IdentityID,
		IDToken:    idToken,
		Issuer:     issuer,
	}

	return c.GetCredentialsForIdentity(ctx, getCredsReq)
}
