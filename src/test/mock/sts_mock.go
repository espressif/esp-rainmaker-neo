// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"
)

type STSMock struct {
	mu                  sync.Mutex
	AccountID           string
	lastAssumeRoleInput *sts.AssumeRoleInput
	// AssumeRoleError, when set, is returned by AssumeRole instead of creds —
	// used to exercise a handler's AssumeRole error path.
	AssumeRoleError error
}

func NewSTSMock() *STSMock {
	return &STSMock{
		AccountID: "00112233445566778899",
	}
}

func (m *STSMock) GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{
		Account: &m.AccountID,
	}, nil
}

func (m *STSMock) AssumeRole(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store the input for later retrieval
	m.lastAssumeRoleInput = params

	if m.AssumeRoleError != nil {
		return nil, m.AssumeRoleError
	}

	var expiration *time.Time
	if params.DurationSeconds != nil {
		exp := time.Now().Add(time.Duration(*params.DurationSeconds) * time.Second)
		expiration = &exp
	} else {
		exp := time.Now().Add(3600 * time.Second)
		expiration = &exp
	}

	output := &sts.AssumeRoleOutput{
		Credentials: &types.Credentials{
			AccessKeyId:     aws.String("new-access-key"),
			SecretAccessKey: aws.String("new-secret-key"),
			SessionToken:    aws.String("new-session-token"),
			Expiration:      expiration,
		},
	}
	return output, nil
}

// GetLastAssumeRoleInput returns the last AssumeRoleInput used in an AssumeRole call
func (m *STSMock) GetLastAssumeRoleInput() *sts.AssumeRoleInput {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.lastAssumeRoleInput
}
