// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package mock provides mock implementations for AWS Cognito Provider
package mock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/golang-jwt/jwt/v5"
)

// UserState represents the current state of a user in the mock
type UserState struct {
	Username         string
	Email            string
	PhoneNumber      string
	Password         string
	UserSub          string
	UserPoolID       string
	IsConfirmed      bool
	IsEnabled        bool
	CreatedAt        time.Time
	LastLoginAt      *time.Time
	Attributes       map[string]string
	ConfirmationCode string
	ResetCode        string
}

// AuthSession represents an active authentication session
type AuthSession struct {
	Username     string
	UserPoolID   string
	ClientID     string
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    time.Time
}

type CognitoProviderMock struct {
	poolsMutex    sync.RWMutex
	usersMutex    sync.RWMutex
	sessionsMutex sync.RWMutex

	// Pool and client management
	Pools map[string]map[string]cognitoidentityprovider.UpdateUserPoolClientInput // key: user_pool_id -> client_id

	// User state management
	Sessions map[string]map[string]map[string]*AuthSession // key: user_pool_id -> client_id -> access_token

	// Alias indexes for O(1) lookups (supporting Cognito aliases)
	Users        map[string]map[string]*UserState // key: user_pool_id -> username
	UsersByEmail map[string]map[string]*UserState // key: user_pool_id -> email -> user
	UsersByPhone map[string]map[string]*UserState // key: user_pool_id -> phone -> user

	// Error injection for testing
	DescribeUserPoolClientError    error
	UpdateUserPoolClientError      error
	SignUpError                    error
	ConfirmSignUpError             error
	InitiateAuthError              error
	IDTokenMinter                  func(user *UserState) string
	RespondToAuthChallengeError    error
	ForgotPasswordError            error
	ConfirmForgotPasswordError     error
	ChangePasswordError            error
	RevokeTokenError               error
	GetUserError                   error
	GlobalSignOutError             error
	AdminGetUserError              error
	AdminCreateUserError           error
	AdminSetUserPasswordError      error
	AdminUpdateUserAttributesError error
	ResendConfirmationCodeError    error

	// Configuration
	DefaultConfirmationCode string
	DefaultResetCode        string
	TokenExpirationMinutes  int
	DefaultUserPoolID       string
}

// NewCognitoProviderMock creates a new stateful Cognito provider mock
func NewCognitoProviderMock() *CognitoProviderMock {
	return &CognitoProviderMock{
		Pools:                   make(map[string]map[string]cognitoidentityprovider.UpdateUserPoolClientInput),
		Users:                   make(map[string]map[string]*UserState),
		UsersByEmail:            make(map[string]map[string]*UserState),
		UsersByPhone:            make(map[string]map[string]*UserState),
		Sessions:                make(map[string]map[string]map[string]*AuthSession),
		DefaultConfirmationCode: "123456",
		DefaultResetCode:        "654321",
		TokenExpirationMinutes:  60,
		DefaultUserPoolID:       "us-east-1_TestPool",
	}
}

// generateToken creates a random token for testing
func (m *CognitoProviderMock) generateToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// generateUserSub creates a unique user sub identifier
func (m *CognitoProviderMock) generateUserSub() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// getUserPoolIDFromClientID finds the user pool ID for a given client ID
// For simplicity, this mock uses the default user pool ID
// In a real implementation, you would maintain a mapping of client IDs to user pool IDs
func (m *CognitoProviderMock) getUserPoolIDFromClientID(clientID string) string {
	m.poolsMutex.RLock()
	defer m.poolsMutex.RUnlock()

	// Check if we have this client ID in our pools
	for userPoolID, clients := range m.Pools {
		for storedClientID := range clients {
			if storedClientID == clientID {
				return userPoolID
			}
		}
	}
	// Return default user pool ID if not found
	return m.DefaultUserPoolID
}

func (m *CognitoProviderMock) AddTestUserPoolDirect(userPoolID, clientID string) {
	m.poolsMutex.Lock()
	defer m.poolsMutex.Unlock()

	// Initialize user pool map if it doesn't exist
	if m.Pools[userPoolID] == nil {
		m.Pools[userPoolID] = make(map[string]cognitoidentityprovider.UpdateUserPoolClientInput)
	}

	m.Pools[userPoolID][clientID] = cognitoidentityprovider.UpdateUserPoolClientInput{
		UserPoolId: aws.String(userPoolID),
		ClientId:   aws.String(clientID),
		AllowedOAuthFlows: []types.OAuthFlowType{
			types.OAuthFlowTypeCode,
		},
		AllowedOAuthScopes:              []string{"openid", "email"},
		AllowedOAuthFlowsUserPoolClient: true,
		SupportedIdentityProviders:      []string{"COGNITO"},
	}
}

// AddTestUserDirect adds a pre-configured user for testing
func (m *CognitoProviderMock) AddTestUserDirect(userPoolID, username, email, password string, confirmed bool) *UserState {
	user := &UserState{
		Username:         username,
		Email:            email,
		Password:         password,
		UserSub:          m.generateUserSub(),
		UserPoolID:       userPoolID,
		IsConfirmed:      confirmed,
		IsEnabled:        true,
		CreatedAt:        time.Now(),
		Attributes:       make(map[string]string),
		ConfirmationCode: m.DefaultConfirmationCode,
		ResetCode:        m.DefaultResetCode,
	}

	if email != "" {
		user.Attributes["email"] = email
	}

	m.usersMutex.Lock()
	defer m.usersMutex.Unlock()

	// Initialize user pool map if it doesn't exist
	if m.Users[userPoolID] == nil {
		m.Users[userPoolID] = make(map[string]*UserState)
	}

	// Store user in primary map
	m.Users[userPoolID][username] = user

	// Store in alias index maps for O(1) lookups
	if user.Email != "" {
		if m.UsersByEmail[userPoolID] == nil {
			m.UsersByEmail[userPoolID] = make(map[string]*UserState)
		}
		m.UsersByEmail[userPoolID][user.Email] = user
	}
	if user.PhoneNumber != "" {
		if m.UsersByPhone[userPoolID] == nil {
			m.UsersByPhone[userPoolID] = make(map[string]*UserState)
		}
		m.UsersByPhone[userPoolID][user.PhoneNumber] = user
	}

	return user
}

// GetUserByUsername retrieves a user by username from a specific user pool
func (m *CognitoProviderMock) GetUserByUsername(userPoolID, username string) *UserState {
	m.usersMutex.RLock()
	defer m.usersMutex.RUnlock()

	if pool, exists := m.Users[userPoolID]; exists {
		return pool[username]
	}
	return nil
}

// findUserByIdentifier finds a user by username, email, or phone (supporting aliases)
// Uses O(1) index lookups for optimal performance
func (m *CognitoProviderMock) findUserByIdentifier(userPoolID, identifier string) *UserState {
	m.usersMutex.RLock()
	defer m.usersMutex.RUnlock()

	// Try direct username lookup - O(1)
	if pool, exists := m.Users[userPoolID]; exists {
		if user, exists := pool[identifier]; exists {
			return user
		}
	}

	// Try email alias lookup - O(1)
	if emailPool, exists := m.UsersByEmail[userPoolID]; exists {
		if user, exists := emailPool[identifier]; exists {
			return user
		}
	}

	// Try phone alias lookup - O(1)
	if phonePool, exists := m.UsersByPhone[userPoolID]; exists {
		if user, exists := phonePool[identifier]; exists {
			return user
		}
	}

	return nil
}

// GetSessionByToken retrieves a session by access token
func (m *CognitoProviderMock) GetSessionByToken(token string) *AuthSession {
	m.sessionsMutex.RLock()
	defer m.sessionsMutex.RUnlock()

	// Search through all user pools and clients for the token
	for _, poolSessions := range m.Sessions {
		for _, clientSessions := range poolSessions {
			if session, exists := clientSessions[token]; exists {
				return session
			}
		}
	}
	return nil
}

// GetSessionByRefreshToken retrieves a session by refresh token
func (m *CognitoProviderMock) GetSessionByRefreshToken(refreshToken string) *AuthSession {
	m.sessionsMutex.RLock()
	defer m.sessionsMutex.RUnlock()

	// Search through all user pools and clients for the refresh token
	for _, poolSessions := range m.Sessions {
		for _, clientSessions := range poolSessions {
			for _, session := range clientSessions {
				if session.RefreshToken == refreshToken {
					return session
				}
			}
		}
	}
	return nil
}

// RegisterSessionForToken creates a session for a given access token
// This is useful for test scenarios where JWT tokens are created directly
// without going through InitiateAuth
func (m *CognitoProviderMock) RegisterSessionForToken(accessToken, userPoolID, clientID, username string) error {
	m.sessionsMutex.Lock()
	defer m.sessionsMutex.Unlock()

	// Initialize nested maps if they don't exist
	if m.Sessions[userPoolID] == nil {
		m.Sessions[userPoolID] = make(map[string]map[string]*AuthSession)
	}
	if m.Sessions[userPoolID][clientID] == nil {
		m.Sessions[userPoolID][clientID] = make(map[string]*AuthSession)
	}

	// Create session
	session := &AuthSession{
		Username:     username,
		UserPoolID:   userPoolID,
		ClientID:     clientID,
		AccessToken:  accessToken,
		RefreshToken: m.generateToken(),
		IDToken:      m.generateToken(),
		ExpiresAt:    time.Now().Add(time.Duration(m.TokenExpirationMinutes) * time.Minute),
	}

	m.Sessions[userPoolID][clientID][accessToken] = session
	return nil
}

// SetErrorForMethod sets an error to be returned by a specific method
func (m *CognitoProviderMock) SetErrorForMethod(method string, err error) {
	switch strings.ToLower(method) {
	case "signup":
		m.SignUpError = err
	case "confirmsignup":
		m.ConfirmSignUpError = err
	case "initiateauth":
		m.InitiateAuthError = err
	case "respondtoauthchallenge":
		m.RespondToAuthChallengeError = err
	case "forgotpassword":
		m.ForgotPasswordError = err
	case "confirmforgotpassword":
		m.ConfirmForgotPasswordError = err
	case "changepassword":
		m.ChangePasswordError = err
	case "revoketoken":
		m.RevokeTokenError = err
	case "getuser":
		m.GetUserError = err
	case "globalsignout":
		m.GlobalSignOutError = err
	}
}

func (m *CognitoProviderMock) DescribeUserPoolClient(ctx context.Context, params *cognitoidentityprovider.DescribeUserPoolClientInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.DescribeUserPoolClientOutput, error) {
	if m.DescribeUserPoolClientError != nil {
		return nil, m.DescribeUserPoolClientError
	}

	pool, ok := m.Pools[*params.UserPoolId]
	if !ok {
		return nil, fmt.Errorf("user pool not found")
	}
	client, ok := pool[*params.ClientId]
	if !ok {
		return nil, fmt.Errorf("client not found")
	}
	return &cognitoidentityprovider.DescribeUserPoolClientOutput{
		UserPoolClient: &types.UserPoolClientType{
			AllowedOAuthFlows:               client.AllowedOAuthFlows,
			AllowedOAuthScopes:              client.AllowedOAuthScopes,
			AllowedOAuthFlowsUserPoolClient: &client.AllowedOAuthFlowsUserPoolClient,
			SupportedIdentityProviders:      client.SupportedIdentityProviders,
			CallbackURLs:                    client.CallbackURLs,
			LogoutURLs:                      client.LogoutURLs,
		},
	}, nil
}

func (m *CognitoProviderMock) UpdateUserPoolClient(ctx context.Context, params *cognitoidentityprovider.UpdateUserPoolClientInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.UpdateUserPoolClientOutput, error) {
	if m.UpdateUserPoolClientError != nil {
		return nil, m.UpdateUserPoolClientError
	}

	m.poolsMutex.Lock()
	defer m.poolsMutex.Unlock()

	pool, ok := m.Pools[*params.UserPoolId]
	if !ok {
		pool = make(map[string]cognitoidentityprovider.UpdateUserPoolClientInput)
		m.Pools[*params.UserPoolId] = pool
	}

	pool[*params.ClientId] = *params
	client := pool[*params.ClientId]

	return &cognitoidentityprovider.UpdateUserPoolClientOutput{
		UserPoolClient: &types.UserPoolClientType{
			AllowedOAuthFlows:               client.AllowedOAuthFlows,
			AllowedOAuthScopes:              client.AllowedOAuthScopes,
			AllowedOAuthFlowsUserPoolClient: &client.AllowedOAuthFlowsUserPoolClient,
			SupportedIdentityProviders:      client.SupportedIdentityProviders,
			CallbackURLs:                    client.CallbackURLs,
			LogoutURLs:                      client.LogoutURLs,
			ClientId:                        params.ClientId,
			UserPoolId:                      params.UserPoolId,
		},
	}, nil
}

// SignUp creates a new user in the mock with stateful tracking
func (m *CognitoProviderMock) SignUp(ctx context.Context, params *cognitoidentityprovider.SignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.SignUpOutput, error) {
	if m.SignUpError != nil {
		return nil, m.SignUpError
	}

	username := *params.Username
	userPoolID := m.getUserPoolIDFromClientID(*params.ClientId)

	// Initialize user pool map if it doesn't exist
	if m.Users[userPoolID] == nil {
		m.Users[userPoolID] = make(map[string]*UserState)
	}

	// Check if user already exists in this user pool
	if _, exists := m.Users[userPoolID][username]; exists {
		return nil, &types.UsernameExistsException{
			Message: aws.String("user already exists"),
		}
	}

	// Create new user state
	user := &UserState{
		Username:         username,
		Password:         *params.Password,
		UserSub:          m.generateUserSub(),
		UserPoolID:       userPoolID,
		IsConfirmed:      false,
		IsEnabled:        true,
		CreatedAt:        time.Now(),
		Attributes:       make(map[string]string),
		ConfirmationCode: m.DefaultConfirmationCode,
		ResetCode:        m.DefaultResetCode,
	}

	// Process user attributes
	for _, attr := range params.UserAttributes {
		if attr.Name != nil && attr.Value != nil {
			user.Attributes[*attr.Name] = *attr.Value

			// Set specific fields for easy access
			switch *attr.Name {
			case "email":
				user.Email = *attr.Value
			case "phone_number":
				user.PhoneNumber = *attr.Value
			}
		}
	}

	// Store user in primary map
	m.Users[userPoolID][username] = user

	// Store in alias index maps for O(1) lookups
	if user.Email != "" {
		if m.UsersByEmail[userPoolID] == nil {
			m.UsersByEmail[userPoolID] = make(map[string]*UserState)
		}
		m.UsersByEmail[userPoolID][user.Email] = user
	}
	if user.PhoneNumber != "" {
		if m.UsersByPhone[userPoolID] == nil {
			m.UsersByPhone[userPoolID] = make(map[string]*UserState)
		}
		m.UsersByPhone[userPoolID][user.PhoneNumber] = user
	}

	return &cognitoidentityprovider.SignUpOutput{
		UserSub:       &user.UserSub,
		UserConfirmed: user.IsConfirmed,
	}, nil
}

// ConfirmSignUp confirms user signup with stateful validation
// Supports username, email, or phone as identifier (alias support)
func (m *CognitoProviderMock) ConfirmSignUp(ctx context.Context, params *cognitoidentityprovider.ConfirmSignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
	if m.ConfirmSignUpError != nil {
		return nil, m.ConfirmSignUpError
	}

	identifier := *params.Username
	userPoolID := m.getUserPoolIDFromClientID(*params.ClientId)

	// Find user by username, email, or phone (alias support)
	user := m.findUserByIdentifier(userPoolID, identifier)
	if user == nil {
		return nil, &types.UserNotFoundException{
			Message: aws.String("user not found"),
		}
	}

	// Real Cognito validates the code BEFORE the account state (measured): a wrong code answers
	// CodeMismatchException even on a confirmed account, and a previously-valid code on a
	// confirmed account answers ExpiredCodeException — never NotAuthorizedException.
	if *params.ConfirmationCode != user.ConfirmationCode {
		return nil, &types.CodeMismatchException{
			Message: aws.String("Invalid verification code provided, please try again."),
		}
	}
	if user.IsConfirmed {
		return nil, &types.ExpiredCodeException{
			Message: aws.String("Invalid code provided, please request a code again."),
		}
	}

	// Confirm the user
	user.IsConfirmed = true

	return &cognitoidentityprovider.ConfirmSignUpOutput{}, nil
}

// InitiateAuth handles authentication with stateful session management
// Supports both USER_PASSWORD_AUTH and REFRESH_TOKEN_AUTH flows
func (m *CognitoProviderMock) InitiateAuth(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.InitiateAuthOutput, error) {
	if m.InitiateAuthError != nil {
		return nil, m.InitiateAuthError
	}

	userPoolID := m.getUserPoolIDFromClientID(*params.ClientId)

	switch params.AuthFlow {
	case types.AuthFlowTypeRefreshTokenAuth, types.AuthFlowTypeRefreshToken:
		return m.handleRefreshTokenAuth(userPoolID, params)
	case types.AuthFlowTypeUserPasswordAuth:
		return m.handleUserPasswordAuth(userPoolID, params)
	default:
		// Default to user password auth for backward compatibility
		return m.handleUserPasswordAuth(userPoolID, params)
	}
}

// handleUserPasswordAuth handles username/password authentication
// Supports username, email, or phone as identifier (alias support)
func (m *CognitoProviderMock) handleUserPasswordAuth(userPoolID string, params *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
	// Extract username and password from auth parameters
	identifier, ok := params.AuthParameters["USERNAME"]
	if !ok {
		return nil, fmt.Errorf("username required")
	}

	password, ok := params.AuthParameters["PASSWORD"]
	if !ok {
		return nil, fmt.Errorf("password required")
	}

	// Find user by username, email, or phone (alias support)
	user := m.findUserByIdentifier(userPoolID, identifier)
	if user == nil {
		return nil, &types.UserNotFoundException{
			Message: aws.String("user not found"),
		}
	}

	// Validate password
	if user.Password != password {
		return nil, &types.NotAuthorizedException{
			Message: aws.String("Incorrect username or password."),
		}
	}

	// Check if user is confirmed. Cognito raises this only once the password has matched, which is
	// why the password check above comes first.
	if !user.IsConfirmed {
		return nil, &types.UserNotConfirmedException{
			Message: aws.String("User is not confirmed."),
		}
	}

	// Check if user is enabled
	if !user.IsEnabled {
		return nil, fmt.Errorf("user disabled")
	}

	// Update user's last login
	m.usersMutex.Lock()
	now := time.Now()
	user.LastLoginAt = &now
	m.usersMutex.Unlock()

	accessToken := m.generateToken()
	refreshToken := m.generateToken()
	idToken := m.generateToken()
	if m.IDTokenMinter != nil {
		idToken = m.IDTokenMinter(user)
	}

	// Create session
	session := &AuthSession{
		Username:     user.Username,
		UserPoolID:   userPoolID,
		ClientID:     *params.ClientId,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		IDToken:      idToken,
		ExpiresAt:    time.Now().Add(time.Duration(m.TokenExpirationMinutes) * time.Minute),
	}

	// Store session with write lock
	m.sessionsMutex.Lock()
	defer m.sessionsMutex.Unlock()

	// Initialize nested maps if they don't exist
	if m.Sessions[userPoolID] == nil {
		m.Sessions[userPoolID] = make(map[string]map[string]*AuthSession)
	}
	if m.Sessions[userPoolID][*params.ClientId] == nil {
		m.Sessions[userPoolID][*params.ClientId] = make(map[string]*AuthSession)
	}

	m.Sessions[userPoolID][*params.ClientId][accessToken] = session

	return &cognitoidentityprovider.InitiateAuthOutput{
		AuthenticationResult: &types.AuthenticationResultType{
			AccessToken:  &accessToken,
			RefreshToken: &refreshToken,
			IdToken:      &idToken,
			ExpiresIn:    int32(m.TokenExpirationMinutes * 60),
			TokenType:    aws.String("Bearer"),
		},
	}, nil
}

// handleRefreshTokenAuth handles refresh token authentication
func (m *CognitoProviderMock) handleRefreshTokenAuth(userPoolID string, params *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
	// Extract refresh token from auth parameters
	refreshToken, ok := params.AuthParameters["REFRESH_TOKEN"]
	if !ok {
		return nil, fmt.Errorf("refresh token required")
	}

	// Find session by refresh token
	existingSession := m.GetSessionByRefreshToken(refreshToken)
	if existingSession == nil {
		return nil, fmt.Errorf("invalid refresh token")
	}

	// Validate user pool and client match
	if existingSession.UserPoolID != userPoolID || existingSession.ClientID != *params.ClientId {
		return nil, fmt.Errorf("invalid refresh token for this client")
	}

	// Find user to validate they still exist and are enabled
	m.usersMutex.RLock()
	userPool, poolExists := m.Users[userPoolID]
	if !poolExists {
		m.usersMutex.RUnlock()
		return nil, fmt.Errorf("user pool not found")
	}

	user, exists := userPool[existingSession.Username]
	if !exists {
		m.usersMutex.RUnlock()
		return nil, fmt.Errorf("user not found")
	}

	// Check if user is still enabled
	if !user.IsEnabled {
		m.usersMutex.RUnlock()
		return nil, fmt.Errorf("user disabled")
	}

	// Update user's last login (upgrade to write lock)
	m.usersMutex.RUnlock()
	m.usersMutex.Lock()
	now := time.Now()
	user.LastLoginAt = &now
	m.usersMutex.Unlock()

	// Generate new tokens (keep the same refresh token)
	newAccessToken := m.generateToken()
	newIDToken := m.generateToken()

	// Create new session with same refresh token
	newSession := &AuthSession{
		Username:     existingSession.Username,
		UserPoolID:   userPoolID,
		ClientID:     *params.ClientId,
		AccessToken:  newAccessToken,
		RefreshToken: refreshToken, // Keep the same refresh token
		IDToken:      newIDToken,
		ExpiresAt:    time.Now().Add(time.Duration(m.TokenExpirationMinutes) * time.Minute),
	}

	// Update sessions with write lock
	m.sessionsMutex.Lock()
	defer m.sessionsMutex.Unlock()

	// Remove old session
	if m.Sessions[userPoolID] != nil && m.Sessions[userPoolID][*params.ClientId] != nil {
		delete(m.Sessions[userPoolID][*params.ClientId], existingSession.AccessToken)
	}

	// Initialize nested maps if they don't exist
	if m.Sessions[userPoolID] == nil {
		m.Sessions[userPoolID] = make(map[string]map[string]*AuthSession)
	}
	if m.Sessions[userPoolID][*params.ClientId] == nil {
		m.Sessions[userPoolID][*params.ClientId] = make(map[string]*AuthSession)
	}

	// Store new session
	m.Sessions[userPoolID][*params.ClientId][newAccessToken] = newSession

	return &cognitoidentityprovider.InitiateAuthOutput{
		AuthenticationResult: &types.AuthenticationResultType{
			AccessToken:  &newAccessToken,
			RefreshToken: &refreshToken, // Return the same refresh token
			IdToken:      &newIDToken,
			ExpiresIn:    int32(m.TokenExpirationMinutes * 60),
			TokenType:    aws.String("Bearer"),
		},
	}, nil
}

// RespondToAuthChallenge handles authentication challenges with stateful tracking
func (m *CognitoProviderMock) RespondToAuthChallenge(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error) {
	if m.RespondToAuthChallengeError != nil {
		return nil, m.RespondToAuthChallengeError
	}

	// For simplicity, this mock assumes all challenges are successful
	// In a real implementation, you would handle different challenge types
	return &cognitoidentityprovider.RespondToAuthChallengeOutput{
		AuthenticationResult: &types.AuthenticationResultType{
			AccessToken:  aws.String(m.generateToken()),
			RefreshToken: aws.String(m.generateToken()),
			IdToken:      aws.String(m.generateToken()),
			ExpiresIn:    int32(m.TokenExpirationMinutes * 60),
			TokenType:    aws.String("Bearer"),
		},
	}, nil
}

// ForgotPassword initiates password reset with stateful code generation
func (m *CognitoProviderMock) ForgotPassword(ctx context.Context, params *cognitoidentityprovider.ForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgotPasswordOutput, error) {
	if m.ForgotPasswordError != nil {
		return nil, m.ForgotPasswordError
	}

	username := *params.Username
	userPoolID := m.getUserPoolIDFromClientID(*params.ClientId)

	userPool, poolExists := m.Users[userPoolID]
	if !poolExists {
		return nil, fmt.Errorf("user pool not found")
	}

	user, exists := userPool[username]
	if !exists {
		return nil, &types.UserNotFoundException{
			Message: aws.String("user not found"),
		}
	}

	// Generate new reset code
	user.ResetCode = m.DefaultResetCode

	return &cognitoidentityprovider.ForgotPasswordOutput{}, nil
}

// ConfirmForgotPassword completes password reset with stateful validation
func (m *CognitoProviderMock) ConfirmForgotPassword(ctx context.Context, params *cognitoidentityprovider.ConfirmForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmForgotPasswordOutput, error) {
	if m.ConfirmForgotPasswordError != nil {
		return nil, m.ConfirmForgotPasswordError
	}

	username := *params.Username
	userPoolID := m.getUserPoolIDFromClientID(*params.ClientId)

	userPool, poolExists := m.Users[userPoolID]
	if !poolExists {
		return nil, fmt.Errorf("user pool not found")
	}

	user, exists := userPool[username]
	if !exists {
		return nil, &types.UserNotFoundException{
			Message: aws.String("user not found"),
		}
	}

	// Validate confirmation code
	if *params.ConfirmationCode != user.ResetCode {
		return nil, fmt.Errorf("invalid confirmation code")
	}

	// Update password
	user.Password = *params.Password

	// Clear reset code
	user.ResetCode = ""

	// Delete all existing sessions for this user
	m.sessionsMutex.Lock()
	for userPoolID, poolSessions := range m.Sessions {
		for clientID, clientSessions := range poolSessions {
			for token, session := range clientSessions {
				if session.Username == username {
					delete(m.Sessions[userPoolID][clientID], token)
				}
			}
		}
	}
	m.sessionsMutex.Unlock()

	return &cognitoidentityprovider.ConfirmForgotPasswordOutput{}, nil
}

// ChangePassword changes user password with stateful validation
func (m *CognitoProviderMock) ChangePassword(ctx context.Context, params *cognitoidentityprovider.ChangePasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ChangePasswordOutput, error) {
	if m.ChangePasswordError != nil {
		return nil, m.ChangePasswordError
	}

	accessToken := *params.AccessToken

	// Find session
	session := m.GetSessionByToken(accessToken)
	if session == nil {
		return nil, fmt.Errorf("invalid access token")
	}

	// Check if token is expired
	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("access token expired")
	}

	// Find user
	m.usersMutex.RLock()
	userPool, poolExists := m.Users[session.UserPoolID]
	if !poolExists {
		m.usersMutex.RUnlock()
		return nil, fmt.Errorf("user pool not found")
	}

	user, exists := userPool[session.Username]
	if !exists {
		m.usersMutex.RUnlock()
		return nil, fmt.Errorf("user not found")
	}

	// Validate current password
	if user.Password != *params.PreviousPassword {
		m.usersMutex.RUnlock()
		return nil, fmt.Errorf("incorrect previous password")
	}

	// Update password (upgrade to write lock)
	m.usersMutex.RUnlock()
	m.usersMutex.Lock()
	user.Password = *params.ProposedPassword
	m.usersMutex.Unlock()

	return &cognitoidentityprovider.ChangePasswordOutput{}, nil
}

// RevokeToken revokes a refresh token by deleting the session
func (m *CognitoProviderMock) RevokeToken(ctx context.Context, params *cognitoidentityprovider.RevokeTokenInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.RevokeTokenOutput, error) {
	if m.RevokeTokenError != nil {
		return nil, m.RevokeTokenError
	}

	token := *params.Token

	// Find session by refresh token
	targetSession := m.GetSessionByRefreshToken(token)
	if targetSession == nil {
		return nil, fmt.Errorf("invalid refresh token")
	}

	// Delete the session completely
	m.sessionsMutex.Lock()
	defer m.sessionsMutex.Unlock()

	if m.Sessions[targetSession.UserPoolID] != nil &&
		m.Sessions[targetSession.UserPoolID][targetSession.ClientID] != nil {
		delete(m.Sessions[targetSession.UserPoolID][targetSession.ClientID], targetSession.AccessToken)
	}

	return &cognitoidentityprovider.RevokeTokenOutput{}, nil
}

// GetUser retrieves user information using access token with stateful validation
func (m *CognitoProviderMock) GetUser(ctx context.Context, params *cognitoidentityprovider.GetUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GetUserOutput, error) {
	if m.GetUserError != nil {
		return nil, m.GetUserError
	}

	accessToken := *params.AccessToken

	// Find session
	session := m.GetSessionByToken(accessToken)
	if session == nil {
		return nil, fmt.Errorf("invalid access token")
	}

	// Check if token is expired
	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("access token expired")
	}

	// Find user
	m.usersMutex.RLock()
	defer m.usersMutex.RUnlock()

	userPool, poolExists := m.Users[session.UserPoolID]
	if !poolExists {
		return nil, fmt.Errorf("user pool not found")
	}

	user, exists := userPool[session.Username]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	// Build user attributes
	var attributes []types.AttributeType
	for name, value := range user.Attributes {
		attributes = append(attributes, types.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(value),
		})
	}

	return &cognitoidentityprovider.GetUserOutput{
		Username:       &user.Username,
		UserAttributes: attributes,
	}, nil
}

// getUserFromJWTToken extracts user information from a JWT token
func (m *CognitoProviderMock) getUserFromJWTToken(tokenString string) (*cognitoidentityprovider.GetUserOutput, error) {
	// Parse token without verification (for test scenarios)
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("invalid access token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid access token")
	}

	// Check if token is expired
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().After(time.Unix(int64(exp), 0)) {
			return nil, fmt.Errorf("access token expired")
		}
	}

	// Extract user identifier from token (try sub or custom:user_id)
	var userID string
	if sub, ok := claims["sub"].(string); ok {
		userID = sub
	} else if customUserID, ok := claims["custom:user_id"].(string); ok {
		userID = customUserID
	} else {
		return nil, fmt.Errorf("invalid access token: missing user identifier")
	}

	// Extract user pool ID from issuer
	var userPoolID string
	if iss, ok := claims["iss"].(string); ok {
		// Extract user pool ID from issuer: https://cognito-idp.{region}.amazonaws.com/{userPoolID}
		parts := strings.Split(iss, "/")
		if len(parts) > 0 {
			userPoolID = parts[len(parts)-1]
		}
	}

	if userPoolID == "" {
		// Try to find user in any pool
		m.usersMutex.RLock()
		defer m.usersMutex.RUnlock()

		for _, pool := range m.Users {
			if user, exists := pool[userID]; exists {
				return m.buildGetUserOutput(user, claims)
			}
		}
		return nil, fmt.Errorf("user not found")
	}

	// Find user in specific pool
	m.usersMutex.RLock()
	defer m.usersMutex.RUnlock()

	userPool, poolExists := m.Users[userPoolID]
	if !poolExists {
		return nil, fmt.Errorf("user pool not found")
	}

	user, exists := userPool[userID]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	return m.buildGetUserOutput(user, claims)
}

// buildGetUserOutput builds GetUserOutput from user state and JWT claims
func (m *CognitoProviderMock) buildGetUserOutput(user *UserState, claims jwt.MapClaims) (*cognitoidentityprovider.GetUserOutput, error) {
	// Build user attributes, preferring values from JWT claims if available
	attributes := []types.AttributeType{}

	// Add all attributes from user state
	for name, value := range user.Attributes {
		attributes = append(attributes, types.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(value),
		})
	}

	// Override with JWT claim values if present
	attributeMap := make(map[string]string)
	for _, attr := range attributes {
		attributeMap[*attr.Name] = *attr.Value
	}

	if email, ok := claims["email"].(string); ok && email != "" {
		attributeMap["email"] = email
	}
	if phoneNumber, ok := claims["phone_number"].(string); ok && phoneNumber != "" {
		attributeMap["phone_number"] = phoneNumber
	}
	if customUserID, ok := claims["custom:user_id"].(string); ok && customUserID != "" {
		attributeMap["custom:user_id"] = customUserID
	}

	// Rebuild attributes list
	attributes = []types.AttributeType{}
	for name, value := range attributeMap {
		attributes = append(attributes, types.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(value),
		})
	}

	return &cognitoidentityprovider.GetUserOutput{
		Username:       &user.Username,
		UserAttributes: attributes,
	}, nil
}

// GlobalSignOut signs out user from all devices by deleting all their sessions
func (m *CognitoProviderMock) GlobalSignOut(ctx context.Context, params *cognitoidentityprovider.GlobalSignOutInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GlobalSignOutOutput, error) {
	if m.GlobalSignOutError != nil {
		return nil, m.GlobalSignOutError
	}

	accessToken := *params.AccessToken

	// Find session
	session := m.GetSessionByToken(accessToken)
	if session == nil {
		return nil, fmt.Errorf("invalid access token")
	}

	// Delete all sessions for this user across all pools and clients
	m.sessionsMutex.Lock()
	defer m.sessionsMutex.Unlock()

	for userPoolID, poolSessions := range m.Sessions {
		for clientID, clientSessions := range poolSessions {
			for token, sess := range clientSessions {
				if sess.Username == session.Username {
					delete(m.Sessions[userPoolID][clientID], token)
				}
			}
		}
	}

	return &cognitoidentityprovider.GlobalSignOutOutput{}, nil
}

// AdminGetUser retrieves user information using admin privileges
func (m *CognitoProviderMock) AdminGetUser(ctx context.Context, params *cognitoidentityprovider.AdminGetUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminGetUserOutput, error) {
	if m.AdminGetUserError != nil {
		return nil, m.AdminGetUserError
	}

	userPoolID := *params.UserPoolId
	identifier := *params.Username

	// Find user by username, email, or phone (alias support)
	user := m.findUserByIdentifier(userPoolID, identifier)
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// Build user attributes
	var attributes []types.AttributeType
	for name, value := range user.Attributes {
		attributes = append(attributes, types.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(value),
		})
	}

	// Determine user status
	var userStatus types.UserStatusType
	if user.IsConfirmed {
		userStatus = types.UserStatusTypeConfirmed
	} else {
		userStatus = types.UserStatusTypeUnconfirmed
	}

	return &cognitoidentityprovider.AdminGetUserOutput{
		Username:       &user.Username,
		UserAttributes: attributes,
		UserStatus:     userStatus,
		Enabled:        true,
	}, nil
}

// AdminCreateUser creates a new user in Cognito user pool using admin privileges
func (m *CognitoProviderMock) AdminCreateUser(ctx context.Context, params *cognitoidentityprovider.AdminCreateUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminCreateUserOutput, error) {
	if m.AdminCreateUserError != nil {
		return nil, m.AdminCreateUserError
	}

	userPoolID := *params.UserPoolId
	username := *params.Username

	m.usersMutex.Lock()
	defer m.usersMutex.Unlock()

	// Initialize user pool map if it doesn't exist
	if m.Users[userPoolID] == nil {
		m.Users[userPoolID] = make(map[string]*UserState)
	}

	// Check if user already exists in this user pool
	if _, exists := m.Users[userPoolID][username]; exists {
		return nil, fmt.Errorf("user already exists")
	}

	// Create new user state
	user := &UserState{
		Username:         username,
		UserSub:          m.generateUserSub(),
		UserPoolID:       userPoolID,
		IsConfirmed:      true,
		IsEnabled:        true,
		CreatedAt:        time.Now(),
		Attributes:       make(map[string]string),
		ConfirmationCode: m.DefaultConfirmationCode,
		ResetCode:        m.DefaultResetCode,
	}

	// Process user attributes
	for _, attr := range params.UserAttributes {
		if attr.Name != nil && attr.Value != nil {
			user.Attributes[*attr.Name] = *attr.Value

			// Set specific fields for easy access
			switch *attr.Name {
			case "email":
				user.Email = *attr.Value
			case "phone_number":
				user.PhoneNumber = *attr.Value
			}
		}
	}

	// Set temporary password if provided
	if params.TemporaryPassword != nil {
		user.Password = *params.TemporaryPassword
	}

	// Store user in primary map
	m.Users[userPoolID][username] = user

	// Store in alias index maps for O(1) lookups
	if user.Email != "" {
		if m.UsersByEmail[userPoolID] == nil {
			m.UsersByEmail[userPoolID] = make(map[string]*UserState)
		}
		m.UsersByEmail[userPoolID][user.Email] = user
	}
	if user.PhoneNumber != "" {
		if m.UsersByPhone[userPoolID] == nil {
			m.UsersByPhone[userPoolID] = make(map[string]*UserState)
		}
		m.UsersByPhone[userPoolID][user.PhoneNumber] = user
	}

	// Build user attributes for response
	var attributes []types.AttributeType
	for name, value := range user.Attributes {
		attributes = append(attributes, types.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(value),
		})
	}

	return &cognitoidentityprovider.AdminCreateUserOutput{
		User: &types.UserType{
			Username:       &user.Username,
			UserStatus:     types.UserStatusTypeConfirmed,
			UserCreateDate: &user.CreatedAt,
			Enabled:        true,
			Attributes:     attributes,
		},
	}, nil
}

// AdminSetUserPassword sets a permanent password for a user using admin privileges
func (m *CognitoProviderMock) AdminSetUserPassword(ctx context.Context, params *cognitoidentityprovider.AdminSetUserPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminSetUserPasswordOutput, error) {
	if m.AdminSetUserPasswordError != nil {
		return nil, m.AdminSetUserPasswordError
	}

	userPoolID := *params.UserPoolId
	username := *params.Username

	m.usersMutex.Lock()
	defer m.usersMutex.Unlock()

	// Check if user pool exists
	userPool, poolExists := m.Users[userPoolID]
	if !poolExists {
		return nil, fmt.Errorf("user pool not found")
	}

	// Find user by username
	user, exists := userPool[username]
	if !exists {
		return nil, &types.UserNotFoundException{
			Message: aws.String("user not found"),
		}
	}

	// Set password
	user.Password = *params.Password

	return &cognitoidentityprovider.AdminSetUserPasswordOutput{}, nil
}

// AdminUpdateUserAttributes updates user attributes using admin privileges
func (m *CognitoProviderMock) AdminUpdateUserAttributes(ctx context.Context, params *cognitoidentityprovider.AdminUpdateUserAttributesInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminUpdateUserAttributesOutput, error) {
	if m.AdminUpdateUserAttributesError != nil {
		return nil, m.AdminUpdateUserAttributesError
	}

	userPoolID := *params.UserPoolId
	identifier := *params.Username

	m.usersMutex.RLock()
	// Check if user pool exists
	_, poolExists := m.Users[userPoolID]
	if !poolExists {
		// Check if user pool exists in any of the alias maps
		_, poolExists = m.UsersByEmail[userPoolID]
		if !poolExists {
			_, poolExists = m.UsersByPhone[userPoolID]
		}
	}
	m.usersMutex.RUnlock()

	if !poolExists {
		return nil, fmt.Errorf("user pool not found")
	}

	// Find user by username, email, or phone (alias support)
	user := m.findUserByIdentifier(userPoolID, identifier)
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	m.usersMutex.Lock()
	defer m.usersMutex.Unlock()

	// Update user attributes
	for _, attr := range params.UserAttributes {
		if attr.Name != nil && attr.Value != nil {
			user.Attributes[*attr.Name] = *attr.Value
		}
	}

	return &cognitoidentityprovider.AdminUpdateUserAttributesOutput{}, nil
}

func (m *CognitoProviderMock) ResendConfirmationCode(ctx context.Context, params *cognitoidentityprovider.ResendConfirmationCodeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ResendConfirmationCodeOutput, error) {
	if m.ResendConfirmationCodeError != nil {
		return nil, m.ResendConfirmationCodeError
	}

	identifier := *params.Username
	userPoolID := m.getUserPoolIDFromClientID(*params.ClientId)

	user := m.findUserByIdentifier(userPoolID, identifier)
	if user == nil {
		return nil, &types.UserNotFoundException{
			Message: aws.String("user not found"),
		}
	}

	if user.IsConfirmed {
		return nil, fmt.Errorf("user already confirmed")
	}

	return &cognitoidentityprovider.ResendConfirmationCodeOutput{
		CodeDeliveryDetails: &types.CodeDeliveryDetailsType{
			Destination: aws.String(identifier),
		},
	}, nil
}
