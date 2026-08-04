// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity/types"
)

type CognitoIdentityMock struct {
	mu sync.Mutex
	// Map of Cognito tokens to Identity IDs
	TokenToIdentityMap map[string]string
	// Map of Identity IDs to Credentials
	IdentityToCredentialsMap map[string]*types.Credentials
	// Store the last GetId input for testing
	LastGetIdInput *cognitoidentity.GetIdInput
	// Store the last GetCredentialsForIdentity input for testing
	LastGetCredentialsInput *cognitoidentity.GetCredentialsForIdentityInput
}

func NewCognitoIdentityMock() *CognitoIdentityMock {
	return &CognitoIdentityMock{
		TokenToIdentityMap:       make(map[string]string),
		IdentityToCredentialsMap: make(map[string]*types.Credentials),
	}
}

func (m *CognitoIdentityMock) GetId(ctx context.Context, params *cognitoidentity.GetIdInput, optFns ...func(*cognitoidentity.Options)) (*cognitoidentity.GetIdOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store the input for testing
	m.LastGetIdInput = params

	// Extract the token from Logins map
	var token string
	for _, v := range params.Logins {
		token = v
		break
	}

	// Check if we have a mapping for this token
	if identityId, exists := m.TokenToIdentityMap[token]; exists {
		return &cognitoidentity.GetIdOutput{
			IdentityId: aws.String(identityId),
		}, nil
	}

	return nil, fmt.Errorf("no identity found for token")
}

func (m *CognitoIdentityMock) GetCredentialsForIdentity(ctx context.Context, params *cognitoidentity.GetCredentialsForIdentityInput, optFns ...func(*cognitoidentity.Options)) (*cognitoidentity.GetCredentialsForIdentityOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store the input for testing
	m.LastGetCredentialsInput = params

	if params.IdentityId == nil {
		return nil, fmt.Errorf("identity ID is required")
	}

	identityId := *params.IdentityId

	// Check if we have credentials for this identity
	if credentials, exists := m.IdentityToCredentialsMap[identityId]; exists {
		return &cognitoidentity.GetCredentialsForIdentityOutput{
			Credentials: credentials,
			IdentityId:  aws.String(identityId),
		}, nil
	}

	// Generate default credentials if not found
	defaultCredentials := &types.Credentials{
		AccessKeyId:  aws.String("MOCK_ACCESS_KEY_ID"),
		SecretKey:    aws.String("MOCK_SECRET_KEY"),
		SessionToken: aws.String("MOCK_SESSION_TOKEN"),
		Expiration:   aws.Time(time.Now().Add(1 * time.Hour)),
	}

	m.IdentityToCredentialsMap[identityId] = defaultCredentials

	return &cognitoidentity.GetCredentialsForIdentityOutput{
		Credentials: defaultCredentials,
		IdentityId:  aws.String(identityId),
	}, nil
}

// Helper method to set up token to identity mappings for testing
func (m *CognitoIdentityMock) AddTokenMapping(token, identityId string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TokenToIdentityMap[token] = identityId
}

// Helper method to set up identity to credentials mappings for testing
func (m *CognitoIdentityMock) AddCredentialsMapping(identityId string, credentials *types.Credentials) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IdentityToCredentialsMap[identityId] = credentials
}

// GetLastGetCredentialsInput returns the last GetCredentialsForIdentityInput used in a GetCredentialsForIdentity call
func (m *CognitoIdentityMock) GetLastGetCredentialsInput() *cognitoidentity.GetCredentialsForIdentityInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.LastGetCredentialsInput
}
