// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type MockSSM struct {
	Parameters        map[string]*types.Parameter
	DeletedParameters []string
	mutex             sync.RWMutex
	PutParameterError error
	// GetParameterError fails every read; GetParameterErrors fails only the named parameters, for
	// tests that must break one read while leaving the rest of a handler's SSM reads working.
	GetParameterError        error
	GetParameterErrors       map[string]error
	GetParametersByPathError error
	DeleteParameterError     error
}

var (
	mockSSM *MockSSM
)

func NewMockSSM() *MockSSM {
	mockSSM = &MockSSM{
		Parameters:         make(map[string]*types.Parameter),
		DeletedParameters:  make([]string, 0),
		GetParameterErrors: make(map[string]error),
	}
	return mockSSM
}

func (m *MockSSM) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	if m.PutParameterError != nil {
		return nil, m.PutParameterError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Mirror the real service: without Overwrite, writing over an existing
	// parameter is an error rather than a silent replace. Callers rely on this
	// as the atomic guard for write-once material.
	if params.Overwrite == nil || !*params.Overwrite {
		if _, exists := m.Parameters[*params.Name]; exists {
			return nil, &types.ParameterAlreadyExists{}
		}
	}

	m.Parameters[*params.Name] = &types.Parameter{
		Name:  params.Name,
		Type:  params.Type,
		Value: params.Value,
	}

	return &ssm.PutParameterOutput{}, nil
}

func (m *MockSSM) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	if m.GetParameterError != nil {
		return nil, m.GetParameterError
	}
	if err, exists := m.GetParameterErrors[*params.Name]; exists {
		return nil, err
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if param, exists := m.Parameters[*params.Name]; exists {
		return &ssm.GetParameterOutput{
			Parameter: &types.Parameter{
				Name:  param.Name,
				Type:  param.Type,
				Value: param.Value,
			},
		}, nil
	}

	return nil, &types.ParameterNotFound{}
}

func (m *MockSSM) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	if m.GetParametersByPathError != nil {
		return nil, m.GetParametersByPathError
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var parameters []types.Parameter
	path := *params.Path

	for paramName, param := range m.Parameters {
		if strings.HasPrefix(paramName, path) {
			parameters = append(parameters, types.Parameter{
				Name:  param.Name,
				Type:  param.Type,
				Value: param.Value,
			})
		}
	}

	return &ssm.GetParametersByPathOutput{
		Parameters: parameters,
	}, nil
}

func (m *MockSSM) DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	if m.DeleteParameterError != nil {
		return nil, m.DeleteParameterError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	paramName := *params.Name
	if _, exists := m.Parameters[paramName]; exists {
		delete(m.Parameters, paramName)
		m.DeletedParameters = append(m.DeletedParameters, paramName)
	}

	return &ssm.DeleteParameterOutput{}, nil
}
