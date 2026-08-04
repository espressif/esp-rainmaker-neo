// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// MockSES is an in-memory SESClientInterface for tests; SendEmail is a no-op recorder.
type MockSES struct {
	Identities              map[string]types.IdentityInfo
	SentEmails              []*sesv2.SendEmailInput
	ProductionAccessEnabled bool
	ReviewPending           bool
	mutex                   sync.RWMutex

	SendEmailError           error
	CreateEmailIdentityError error
	GetEmailIdentityError    error
	ListEmailIdentitiesError error
	DeleteEmailIdentityError error
	GetAccountError          error
	PutAccountDetailsError   error
}

func NewMockSES() *MockSES {
	return &MockSES{Identities: make(map[string]types.IdentityInfo)}
}

func (m *MockSES) SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	if m.SendEmailError != nil {
		return nil, m.SendEmailError
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.SentEmails = append(m.SentEmails, params)
	return &sesv2.SendEmailOutput{}, nil
}

func (m *MockSES) CreateEmailIdentity(ctx context.Context, params *sesv2.CreateEmailIdentityInput, optFns ...func(*sesv2.Options)) (*sesv2.CreateEmailIdentityOutput, error) {
	if m.CreateEmailIdentityError != nil {
		return nil, m.CreateEmailIdentityError
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	name := aws.ToString(params.EmailIdentity)
	// SES refuses to re-create an identity it already holds, and callers have to cope
	// with that; overwriting instead would hide their handling of it.
	if _, exists := m.Identities[name]; exists {
		return nil, &types.AlreadyExistsException{Message: aws.String("Email identity " + name + " already exist.")}
	}
	m.Identities[name] = types.IdentityInfo{
		IdentityName:       params.EmailIdentity,
		IdentityType:       types.IdentityTypeEmailAddress,
		VerificationStatus: types.VerificationStatusPending,
		SendingEnabled:     false,
	}
	return &sesv2.CreateEmailIdentityOutput{
		IdentityType:             types.IdentityTypeEmailAddress,
		VerifiedForSendingStatus: false,
	}, nil
}

func (m *MockSES) GetEmailIdentity(ctx context.Context, params *sesv2.GetEmailIdentityInput, optFns ...func(*sesv2.Options)) (*sesv2.GetEmailIdentityOutput, error) {
	if m.GetEmailIdentityError != nil {
		return nil, m.GetEmailIdentityError
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	info, ok := m.Identities[aws.ToString(params.EmailIdentity)]
	if !ok {
		return nil, &types.NotFoundException{}
	}
	return &sesv2.GetEmailIdentityOutput{
		IdentityType:             info.IdentityType,
		VerificationStatus:       info.VerificationStatus,
		VerifiedForSendingStatus: info.SendingEnabled,
	}, nil
}

func (m *MockSES) ListEmailIdentities(ctx context.Context, params *sesv2.ListEmailIdentitiesInput, optFns ...func(*sesv2.Options)) (*sesv2.ListEmailIdentitiesOutput, error) {
	if m.ListEmailIdentitiesError != nil {
		return nil, m.ListEmailIdentitiesError
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	infos := make([]types.IdentityInfo, 0, len(m.Identities))
	for _, info := range m.Identities {
		infos = append(infos, info)
	}
	return &sesv2.ListEmailIdentitiesOutput{EmailIdentities: infos}, nil
}

func (m *MockSES) DeleteEmailIdentity(ctx context.Context, params *sesv2.DeleteEmailIdentityInput, optFns ...func(*sesv2.Options)) (*sesv2.DeleteEmailIdentityOutput, error) {
	if m.DeleteEmailIdentityError != nil {
		return nil, m.DeleteEmailIdentityError
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	// Mirror SES: deleting an absent identity is a NotFoundException, not a no-op (the service swallows it).
	name := aws.ToString(params.EmailIdentity)
	if _, ok := m.Identities[name]; !ok {
		return nil, &types.NotFoundException{}
	}
	delete(m.Identities, name)
	return &sesv2.DeleteEmailIdentityOutput{}, nil
}

func (m *MockSES) GetAccount(ctx context.Context, params *sesv2.GetAccountInput, optFns ...func(*sesv2.Options)) (*sesv2.GetAccountOutput, error) {
	if m.GetAccountError != nil {
		return nil, m.GetAccountError
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	out := &sesv2.GetAccountOutput{ProductionAccessEnabled: m.ProductionAccessEnabled}
	if m.ReviewPending {
		out.Details = &types.AccountDetails{ReviewDetails: &types.ReviewDetails{Status: types.ReviewStatusPending}}
	}
	return out, nil
}

func (m *MockSES) PutAccountDetails(ctx context.Context, params *sesv2.PutAccountDetailsInput, optFns ...func(*sesv2.Options)) (*sesv2.PutAccountDetailsOutput, error) {
	if m.PutAccountDetailsError != nil {
		return nil, m.PutAccountDetailsError
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.ReviewPending = true
	return &sesv2.PutAccountDetailsOutput{}, nil
}

// SetVerified simulates the owner following SES's verification link.
func (m *MockSES) SetVerified(identity string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if info, ok := m.Identities[identity]; ok {
		info.VerificationStatus = types.VerificationStatusSuccess
		info.SendingEnabled = true
		m.Identities[identity] = info
	}
}
