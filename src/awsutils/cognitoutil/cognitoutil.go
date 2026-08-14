// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package cognitoutil provides Cognito authentication utilities
package cognitoutil

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// CognitoService provides abstraction for Cognito operations
type CognitoService struct {
	client           awscommon.CognitoProviderInterface
	UserPoolID       string
	UserPoolClientID string
	// ClientSecret is set when the app client is confidential. Cognito then requires SECRET_HASH on
	// every unauthenticated operation, which is what stops anyone holding only the client id from
	// driving sign-up or password-reset against the pool.
	ClientSecret string
	UserPoolJWKS jwk.Set
}

// withSecretHash adds SECRET_HASH to auth parameters when the client is confidential; InitiateAuth
// carries it there rather than as a top-level field.
func (c *CognitoService) withSecretHash(username string, params map[string]string) map[string]string {
	if h := c.secretHash(username); h != nil {
		params["SECRET_HASH"] = *h
	}
	return params
}

// secretHash is the RFC 2104 HMAC Cognito requires from a confidential app client:
// base64(HMAC-SHA256(username + client_id, client_secret)). Empty when the client is public.
func (c *CognitoService) secretHash(username string) *string {
	if c.ClientSecret == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(c.ClientSecret))
	mac.Write([]byte(username + c.UserPoolClientID))
	return aws.String(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// SignupRequest represents user signup data
type SignupRequest struct {
	Email       string
	PhoneNumber string
	Password    string
	Username    string
}

// SignupResponse represents signup result
type SignupResponse struct {
	UserID               string
	Success              bool
	RequiresVerification bool
	Message              string
}

// VerifyRequest represents verification data
type VerifyRequest struct {
	Username string
	Code     string
}

// VerifyResponse represents verification result
type VerifyResponse struct {
	Success bool
	Message string
}

// LoginRequest represents login data
type LoginRequest struct {
	Username     string
	Password     string
	UseAdminPool bool
}

// LoginResponse represents login result
type LoginResponse struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	Success      bool
	Message      string
}

// NewCognitoService builds a service for a pool we own: the pool id enables the Admin* operations
// and the JWKS is loaded from the SSM snapshot our deploy writes.
func NewCognitoService(ctx context.Context, userPoolID, userPoolClientID, userPoolJWKSParameterName string) (*CognitoService, error) {
	userPoolJWKS, err := ssmutil.GetParameterWithCaching(ctx, userPoolJWKSParameterName, false) // Caching as the info is not expected to change frequently and leads to throttling errors for sign in
	if err != nil {
		return nil, rmerror.NewRMError(err, fmt.Sprintf("failed to get JWKS from SSM parameter %s", userPoolJWKSParameterName))
	}
	if userPoolJWKS == "" {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("JWKS value is empty for parameter %s", userPoolJWKSParameterName))
	}
	keySet, err := jwk.Parse([]byte(userPoolJWKS))
	if err != nil {
		return nil, rmerror.NewRMError(err, fmt.Sprintf("failed to parse JWKS for parameter %s", userPoolJWKSParameterName))
	}

	return &CognitoService{
		client:           awscommon.GetCognitoProviderClient(),
		UserPoolID:       userPoolID,
		UserPoolClientID: userPoolClientID,
		UserPoolJWKS:     keySet,
	}, nil
}

// NewAppClientService builds a service for a pool we do not own, reached only through the
// unauthenticated app-client operations (sign-up, password auth, password recovery). It takes no
// pool id and no key set because none of those operations needs either, so the Admin* methods on
// this value have nothing to act on.
func NewAppClientService(clientID, clientSecret string) *CognitoService {
	return NewAppClientServiceForRegion(clientID, clientSecret, "")
}

// NewAppClientServiceForRegion is NewAppClientService for a pool in a specific region — the
// unauthenticated app-client operations (InitiateAuth/SignUp/recovery) must hit that region's
// Cognito endpoint. An empty region uses the deployment-region client.
func NewAppClientServiceForRegion(clientID, clientSecret, region string) *CognitoService {
	return &CognitoService{
		client:           awscommon.CognitoProviderClientForRegion(region),
		UserPoolClientID: clientID,
		ClientSecret:     clientSecret,
	}
}

// SignUp creates a new user in Cognito user pool
func (c *CognitoService) SignUp(ctx context.Context, username, email, phone, password string) (*SignupResponse, error) {
	var attributes []types.AttributeType

	if email != "" {
		attributes = append(attributes, types.AttributeType{
			Name:  &[]string{"email"}[0],
			Value: &email,
		})
	}
	if phone != "" {
		attributes = append(attributes, types.AttributeType{
			Name:  &[]string{"phone_number"}[0],
			Value: &phone,
		})
	}

	signupInput := &cognitoidentityprovider.SignUpInput{
		ClientId:       aws.String(c.UserPoolClientID),
		SecretHash:     c.secretHash(username),
		Username:       aws.String(username),
		Password:       aws.String(password),
		UserAttributes: attributes,
	}

	output, err := c.client.SignUp(ctx, signupInput)
	if err != nil {
		return &SignupResponse{
			Success: false,
			Message: "Failed to create user account",
		}, err
	}

	if output.CodeDeliveryDetails == nil {
		rlog.Warn(ctx).Msg("SignUp successful but NO CodeDeliveryDetails returned - email may not have been sent")
	}

	return &SignupResponse{
		UserID:               *output.UserSub,
		Success:              true,
		RequiresVerification: !output.UserConfirmed,
		Message:              "User registered successfully",
	}, nil
}

// IsUserUnconfirmed checks if a user exists in Cognito but has not confirmed their account
func (c *CognitoService) IsUserUnconfirmed(ctx context.Context, username string) (bool, error) {
	output, err := c.AdminGetUser(ctx, username)
	if err != nil {
		return false, err
	}
	return output.UserStatus == types.UserStatusTypeUnconfirmed, nil
}

// ResendSignupCode resends the signup confirmation code
func (c *CognitoService) ResendSignupCode(ctx context.Context, username string) error {
	input := &cognitoidentityprovider.ResendConfirmationCodeInput{
		ClientId:   aws.String(c.UserPoolClientID),
		SecretHash: c.secretHash(username),
		Username:   aws.String(username),
	}

	_, err := c.client.ResendConfirmationCode(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to resend confirmation code")
	}

	return nil
}

// ConfirmSignUp verifies user signup with confirmation code
func (c *CognitoService) ConfirmSignUp(ctx context.Context, username, code string) error {
	confirmInput := &cognitoidentityprovider.ConfirmSignUpInput{
		ClientId:         aws.String(c.UserPoolClientID),
		SecretHash:       c.secretHash(username),
		Username:         aws.String(username),
		ConfirmationCode: aws.String(code),
	}

	_, err := c.client.ConfirmSignUp(ctx, confirmInput)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to confirm signup")
	}

	return nil
}

// InitiateAuth handles user authentication
func (c *CognitoService) InitiateAuth(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	authInput := &cognitoidentityprovider.InitiateAuthInput{
		ClientId: aws.String(c.UserPoolClientID),
		AuthFlow: types.AuthFlowTypeUserPasswordAuth,
		AuthParameters: c.withSecretHash(req.Username, map[string]string{
			"USERNAME": req.Username,
			"PASSWORD": req.Password,
		}),
	}

	output, err := c.client.InitiateAuth(ctx, authInput)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Message: "Authentication failed",
		}, err
	}

	// A challenge (MFA, ...) verifies the password but issues no tokens; nil would panic below.
	if output.AuthenticationResult == nil {
		return &LoginResponse{
			Success: false,
			Message: "Authentication requires a challenge this surface does not support",
		}, nil
	}

	return &LoginResponse{
		AccessToken:  *output.AuthenticationResult.AccessToken,
		RefreshToken: *output.AuthenticationResult.RefreshToken,
		IDToken:      *output.AuthenticationResult.IdToken,
		Success:      true,
		Message:      "Login successful",
	}, nil
}

// ForgotPassword initiates password reset flow
func (c *CognitoService) ForgotPassword(ctx context.Context, username string) error {
	input := &cognitoidentityprovider.ForgotPasswordInput{
		ClientId:   aws.String(c.UserPoolClientID),
		SecretHash: c.secretHash(username),
		Username:   aws.String(username),
	}

	_, err := c.client.ForgotPassword(ctx, input)
	return err
}

// ConfirmForgotPassword completes password reset with confirmation code
func (c *CognitoService) ConfirmForgotPassword(ctx context.Context, username, code, newPassword string) error {
	input := &cognitoidentityprovider.ConfirmForgotPasswordInput{
		ClientId:         aws.String(c.UserPoolClientID),
		SecretHash:       c.secretHash(username),
		Username:         aws.String(username),
		ConfirmationCode: aws.String(code),
		Password:         aws.String(newPassword),
	}

	_, err := c.client.ConfirmForgotPassword(ctx, input)
	return err
}

// ChangePassword changes user password when authenticated
func (c *CognitoService) ChangePassword(ctx context.Context, accessToken, previousPassword, proposedPassword string) error {
	input := &cognitoidentityprovider.ChangePasswordInput{
		AccessToken:      aws.String(accessToken),
		PreviousPassword: aws.String(previousPassword),
		ProposedPassword: aws.String(proposedPassword),
	}

	_, err := c.client.ChangePassword(ctx, input)
	return err
}

// RevokeToken revokes a refresh token
func (c *CognitoService) RevokeToken(ctx context.Context, refreshToken string) error {
	input := &cognitoidentityprovider.RevokeTokenInput{
		ClientId: aws.String(c.UserPoolClientID),
		Token:    aws.String(refreshToken),
	}

	_, err := c.client.RevokeToken(ctx, input)
	return err
}

// getUser retrieves user information using an access token.
//
// Deliberately unexported. cognito-idp:GetUser is an anonymous operation whose only
// input is the token, so Cognito resolves the user pool from the token's iss claim and
// answers from whichever pool issued it, including one an attacker controls. It also
// carries no AWS identity, so the caller's IAM policy is never evaluated. On its own it
// establishes that a token is genuine, not that it is ours.
//
// Callers must use GetVerifiedUser.
func (c *CognitoService) getUser(ctx context.Context, accessToken string) (*cognitoidentityprovider.GetUserOutput, error) {
	input := &cognitoidentityprovider.GetUserInput{
		AccessToken: aws.String(accessToken),
	}

	return c.client.GetUser(ctx, input)
}

// GetVerifiedUser resolves a caller-supplied token to a user in this service's pool and
// returns that user's Cognito attributes.
//
// Two independent checks, both required:
//
//  1. Offline. The token is verified against this pool's JWKS and issuer. The pool comes
//     from c.UserPoolID and the keys from c.UserPoolJWKS, both fixed at construction from
//     the Lambda's configuration, so the token cannot nominate its own validator. A token
//     minted in any other Cognito pool fails here.
//  2. Online. Cognito is asked for the user. This catches state a JWT cannot express:
//     revoked tokens, global sign-out, and disabled or deleted users.
//
// Neither check subsumes the other. Offline verification proves who issued the token but
// not that it is still valid. The online call proves it is still valid but not which pool
// issued it. Dropping either one reopens a real attack.
func (c *CognitoService) GetVerifiedUser(ctx context.Context, token string) ([]types.AttributeType, error) {
	if token == "" {
		return nil, rmerror.NewRMError(nil, "token is empty")
	}

	if _, err := jwtutil.ExtractCogntioClaimsFromOurToken(c.UserPoolJWKS, c.UserPoolID, token); err != nil {
		return nil, rmerror.NewRMError(err, "token failed validation against this user pool")
	}

	output, err := c.getUser(ctx, token)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user from Cognito")
	}

	return output.UserAttributes, nil
}

// GlobalSignOut signs out user from all devices
func (c *CognitoService) GlobalSignOut(ctx context.Context, accessToken string) error {
	input := &cognitoidentityprovider.GlobalSignOutInput{
		AccessToken: aws.String(accessToken),
	}

	_, err := c.client.GlobalSignOut(ctx, input)
	return err
}

// InitiateAuthAdvanced provides advanced authentication with custom parameters
func (c *CognitoService) InitiateAuthAdvanced(ctx context.Context, authFlow types.AuthFlowType, authParams map[string]string) (*cognitoidentityprovider.InitiateAuthOutput, error) {
	input := &cognitoidentityprovider.InitiateAuthInput{
		ClientId:       aws.String(c.UserPoolClientID),
		AuthFlow:       authFlow,
		AuthParameters: authParams,
	}

	return c.client.InitiateAuth(ctx, input)
}

// RespondToAuthChallenge handles authentication challenges
func (c *CognitoService) RespondToAuthChallenge(ctx context.Context, challengeName types.ChallengeNameType, session *string, challengeResponses map[string]string) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error) {
	input := &cognitoidentityprovider.RespondToAuthChallengeInput{
		ClientId:           aws.String(c.UserPoolClientID),
		ChallengeName:      challengeName,
		Session:            session,
		ChallengeResponses: challengeResponses,
	}

	return c.client.RespondToAuthChallenge(ctx, input)
}

// AdminGetUser retrieves user information using admin privileges
func (c *CognitoService) AdminGetUser(ctx context.Context, username string) (*cognitoidentityprovider.AdminGetUserOutput, error) {
	input := &cognitoidentityprovider.AdminGetUserInput{
		UserPoolId: aws.String(c.UserPoolID),
		Username:   aws.String(username),
	}

	return c.client.AdminGetUser(ctx, input)
}

// AdminCreateUser creates a new user in Cognito user pool using admin privileges
func (c *CognitoService) AdminCreateUser(ctx context.Context, username string, attributes map[string]string, temporaryPassword *string, messageAction types.MessageActionType) (*cognitoidentityprovider.AdminCreateUserOutput, error) {
	var userAttributes []types.AttributeType
	for name, value := range attributes {
		userAttributes = append(userAttributes, types.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(value),
		})
	}

	input := &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId:     aws.String(c.UserPoolID),
		Username:       aws.String(username),
		UserAttributes: userAttributes,
	}

	if temporaryPassword != nil {
		input.TemporaryPassword = temporaryPassword
	}

	if messageAction != "" {
		input.MessageAction = messageAction
	}

	return c.client.AdminCreateUser(ctx, input)
}

// AdminSetUserPassword sets a permanent password for a user using admin privileges
func (c *CognitoService) AdminSetUserPassword(ctx context.Context, username, password string) error {
	input := &cognitoidentityprovider.AdminSetUserPasswordInput{
		UserPoolId: aws.String(c.UserPoolID),
		Username:   aws.String(username),
		Password:   aws.String(password),
		Permanent:  true,
	}

	_, err := c.client.AdminSetUserPassword(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to set user password")
	}

	return nil
}

// AdminUpdateUserAttributes updates user attributes using admin privileges
func (c *CognitoService) AdminUpdateUserAttributes(ctx context.Context, username string, attributes map[string]string) error {
	var userAttributes []types.AttributeType
	for name, value := range attributes {
		userAttributes = append(userAttributes, types.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(value),
		})
	}

	input := &cognitoidentityprovider.AdminUpdateUserAttributesInput{
		UserPoolId:     aws.String(c.UserPoolID),
		Username:       aws.String(username),
		UserAttributes: userAttributes,
	}

	_, err := c.client.AdminUpdateUserAttributes(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to update user attributes")
	}

	return nil
}

func ExtractUserPoolIDFromIssuer(issuer string) string {
	prefix := "https://cognito-idp."
	suffix := ".amazonaws.com/"

	if !strings.HasPrefix(issuer, prefix) {
		return ""
	}

	idx := strings.Index(issuer, suffix)
	if idx == -1 {
		return ""
	}

	userPoolID := issuer[idx+len(suffix):]
	return userPoolID
}
