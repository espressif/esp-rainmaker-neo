// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// SNSMock is a mock implementation of the SNS client
type SNSMock struct {
	PublishedMessages                     []*sns.PublishInput
	CreatePlatformApplicationCalls        []*sns.CreatePlatformApplicationInput
	CreatedPlatformEndpointCalls          []*sns.CreatePlatformEndpointInput
	DeleteEndpointCalls                   []*sns.DeleteEndpointInput
	ListPlatformApplicationsCalls         []*sns.ListPlatformApplicationsInput
	SetPlatformApplicationAttributesCalls []*sns.SetPlatformApplicationAttributesInput
	DeletePlatformApplicationCalls        []*sns.DeletePlatformApplicationInput
	GetPlatformApplicationAttributesCalls []*sns.GetPlatformApplicationAttributesInput
	mutex                                 sync.RWMutex
	PublishError                          error
	CreatePlatformApplicationError        error
	CreatePlatformEndpointError           error
	DeleteEndpointError                   error
	ListPlatformApplicationsError         error
	SetPlatformApplicationAttributesError error
	DeletePlatformApplicationError        error
	GetPlatformApplicationAttributesError error
	MockPlatformApplications              []types.PlatformApplication
	PlatformAttributes                    map[string]map[string]string
}

var (
	mockSNS *SNSMock
)

// NewSNSMock creates a new SNS mock
func NewSNSMock() *SNSMock {
	mockSNS = &SNSMock{
		PublishedMessages:              make([]*sns.PublishInput, 0),
		CreatePlatformApplicationCalls: make([]*sns.CreatePlatformApplicationInput, 0),
		CreatedPlatformEndpointCalls:   make([]*sns.CreatePlatformEndpointInput, 0),
		DeleteEndpointCalls:            make([]*sns.DeleteEndpointInput, 0),
		PlatformAttributes:             make(map[string]map[string]string),
	}
	return mockSNS
}

// Publish mocks the Publish method of the SNS client
func (m *SNSMock) Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error) {
	if m.PublishError != nil {
		return nil, m.PublishError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.PublishedMessages = append(m.PublishedMessages, params)

	return &sns.PublishOutput{
		MessageId: new(string),
	}, nil
}

// GetPublishedMessages returns all published messages
func (m *SNSMock) GetPublishedMessages() []*sns.PublishInput {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.PublishedMessages
}

// ClearPublishedMessages clears all published messages
func (m *SNSMock) ClearPublishedMessages() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.PublishedMessages = make([]*sns.PublishInput, 0)
}

// SetPublishError sets an error to be returned by Publish
func (m *SNSMock) SetPublishError(err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.PublishError = err
}

// CreatePlatformApplication mocks the CreatePlatformApplication method of the SNS client
func (m *SNSMock) CreatePlatformApplication(ctx context.Context, params *sns.CreatePlatformApplicationInput, optFns ...func(*sns.Options)) (*sns.CreatePlatformApplicationOutput, error) {
	if m.CreatePlatformApplicationError != nil {
		return nil, m.CreatePlatformApplicationError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.CreatePlatformApplicationCalls = append(m.CreatePlatformApplicationCalls, params)

	// Generate a mock ARN
	platformAppArn := fmt.Sprintf("arn:aws:sns:us-east-1:123456789012:app/%s/%s", *params.Platform, *params.Name)

	// Record attributes for later GET.
	if m.PlatformAttributes == nil {
		m.PlatformAttributes = make(map[string]map[string]string)
	}
	attrs := make(map[string]string, len(params.Attributes))
	for k, v := range params.Attributes {
		attrs[k] = v
	}
	m.PlatformAttributes[platformAppArn] = attrs

	return &sns.CreatePlatformApplicationOutput{
		PlatformApplicationArn: aws.String(platformAppArn),
	}, nil
}

// CreatePlatformEndpoint mocks the CreatePlatformEndpoint method of the SNS client
func (m *SNSMock) CreatePlatformEndpoint(ctx context.Context, params *sns.CreatePlatformEndpointInput, optFns ...func(*sns.Options)) (*sns.CreatePlatformEndpointOutput, error) {
	if m.CreatePlatformEndpointError != nil {
		return nil, m.CreatePlatformEndpointError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.CreatedPlatformEndpointCalls = append(m.CreatedPlatformEndpointCalls, params)

	// Generate a mock endpoint ARN
	endpointArn := fmt.Sprintf("%s/%s", *params.PlatformApplicationArn, "mock-endpoint-id")

	return &sns.CreatePlatformEndpointOutput{
		EndpointArn: aws.String(endpointArn),
	}, nil
}

// DeleteEndpoint mocks the DeleteEndpoint method of the SNS client
func (m *SNSMock) DeleteEndpoint(ctx context.Context, params *sns.DeleteEndpointInput, optFns ...func(*sns.Options)) (*sns.DeleteEndpointOutput, error) {
	if m.DeleteEndpointError != nil {
		return nil, m.DeleteEndpointError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.DeleteEndpointCalls = append(m.DeleteEndpointCalls, params)

	return &sns.DeleteEndpointOutput{}, nil
}

// GetCreatePlatformApplicationCalls returns all CreatePlatformApplication calls
func (m *SNSMock) GetCreatePlatformApplicationCalls() []*sns.CreatePlatformApplicationInput {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.CreatePlatformApplicationCalls
}

// GetCreatePlatformEndpointCalls returns all CreatePlatformEndpoint calls
func (m *SNSMock) GetCreatePlatformEndpointCalls() []*sns.CreatePlatformEndpointInput {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.CreatedPlatformEndpointCalls
}

// GetDeleteEndpointCalls returns all DeleteEndpoint calls
func (m *SNSMock) GetDeleteEndpointCalls() []*sns.DeleteEndpointInput {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.DeleteEndpointCalls
}

// ListPlatformApplications mocks the ListPlatformApplications method of the SNS client
func (m *SNSMock) ListPlatformApplications(ctx context.Context, params *sns.ListPlatformApplicationsInput, optFns ...func(*sns.Options)) (*sns.ListPlatformApplicationsOutput, error) {
	if m.ListPlatformApplicationsError != nil {
		return nil, m.ListPlatformApplicationsError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.ListPlatformApplicationsCalls = append(m.ListPlatformApplicationsCalls, params)

	return &sns.ListPlatformApplicationsOutput{
		PlatformApplications: m.MockPlatformApplications,
		NextToken:            nil, // For simplicity, we don't mock pagination
	}, nil
}

// GetListPlatformApplicationsCalls returns all ListPlatformApplications calls
func (m *SNSMock) GetListPlatformApplicationsCalls() []*sns.ListPlatformApplicationsInput {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.ListPlatformApplicationsCalls
}

// SetMockPlatformApplications sets the mock platform applications to be returned
func (m *SNSMock) SetMockPlatformApplications(apps []types.PlatformApplication) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.MockPlatformApplications = apps
}

// SetListPlatformApplicationsError sets an error to be returned by ListPlatformApplications
func (m *SNSMock) SetListPlatformApplicationsError(err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.ListPlatformApplicationsError = err
}

// SetPlatformApplicationAttributes mocks the SetPlatformApplicationAttributes method of the SNS client
func (m *SNSMock) SetPlatformApplicationAttributes(ctx context.Context, params *sns.SetPlatformApplicationAttributesInput, optFns ...func(*sns.Options)) (*sns.SetPlatformApplicationAttributesOutput, error) {
	if m.SetPlatformApplicationAttributesError != nil {
		return nil, m.SetPlatformApplicationAttributesError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.SetPlatformApplicationAttributesCalls = append(m.SetPlatformApplicationAttributesCalls, params)

	if m.PlatformAttributes == nil {
		m.PlatformAttributes = make(map[string]map[string]string)
	}
	if _, ok := m.PlatformAttributes[*params.PlatformApplicationArn]; !ok {
		m.PlatformAttributes[*params.PlatformApplicationArn] = make(map[string]string)
	}
	for k, v := range params.Attributes {
		m.PlatformAttributes[*params.PlatformApplicationArn][k] = v
	}

	return &sns.SetPlatformApplicationAttributesOutput{}, nil
}

// GetPlatformApplicationAttributes mocks the GetPlatformApplicationAttributes method of the SNS client
func (m *SNSMock) GetPlatformApplicationAttributes(ctx context.Context, params *sns.GetPlatformApplicationAttributesInput, optFns ...func(*sns.Options)) (*sns.GetPlatformApplicationAttributesOutput, error) {
	if m.GetPlatformApplicationAttributesError != nil {
		return nil, m.GetPlatformApplicationAttributesError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.GetPlatformApplicationAttributesCalls = append(m.GetPlatformApplicationAttributesCalls, params)

	attrs := map[string]string{}
	if stored, ok := m.PlatformAttributes[*params.PlatformApplicationArn]; ok {
		for k, v := range stored {
			attrs[k] = v
		}
	}

	return &sns.GetPlatformApplicationAttributesOutput{Attributes: attrs}, nil
}

// DeletePlatformApplication mocks the DeletePlatformApplication method of the SNS client
func (m *SNSMock) DeletePlatformApplication(ctx context.Context, params *sns.DeletePlatformApplicationInput, optFns ...func(*sns.Options)) (*sns.DeletePlatformApplicationOutput, error) {
	if m.DeletePlatformApplicationError != nil {
		return nil, m.DeletePlatformApplicationError
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.DeletePlatformApplicationCalls = append(m.DeletePlatformApplicationCalls, params)

	return &sns.DeletePlatformApplicationOutput{}, nil
}

// GetSetPlatformApplicationAttributesCalls returns all SetPlatformApplicationAttributes calls
func (m *SNSMock) GetSetPlatformApplicationAttributesCalls() []*sns.SetPlatformApplicationAttributesInput {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.SetPlatformApplicationAttributesCalls
}

// GetDeletePlatformApplicationCalls returns all DeletePlatformApplication calls
func (m *SNSMock) GetDeletePlatformApplicationCalls() []*sns.DeletePlatformApplicationInput {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.DeletePlatformApplicationCalls
}
