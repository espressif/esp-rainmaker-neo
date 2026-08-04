// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	kvs_types "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"
	"github.com/aws/smithy-go"
)

type SignalingChannel struct {
	ChannelName string
	ChannelARN  string
	ChannelType kvs_types.ChannelType
}

type KVSClientMock struct {
	// Key is the channel name
	Channels map[string]SignalingChannel

	mutex sync.RWMutex

	// Error forcing flags
	ForceCreateError   bool
	ForceDescribeError bool
}

func NewKVSClientMock() *KVSClientMock {
	return &KVSClientMock{
		Channels: make(map[string]SignalingChannel),
	}
}

func (m *KVSClientMock) CreateSignalingChannel(ctx context.Context, params *kinesisvideo.CreateSignalingChannelInput, optFns ...func(*kinesisvideo.Options)) (*kinesisvideo.CreateSignalingChannelOutput, error) {
	if m.ForceCreateError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced create signaling channel error",
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	channelName := *params.ChannelName

	// Check if channel already exists — return ResourceInUseException
	if _, exists := m.Channels[channelName]; exists {
		return nil, &kvs_types.ResourceInUseException{
			Message: aws.String(fmt.Sprintf("Channel %s already exists", channelName)),
		}
	}

	channelARN := fmt.Sprintf("arn:aws:kinesisvideo:us-west-2:123456789012:channel/%s/1234567890", channelName)

	m.Channels[channelName] = SignalingChannel{
		ChannelName: channelName,
		ChannelARN:  channelARN,
		ChannelType: params.ChannelType,
	}

	return &kinesisvideo.CreateSignalingChannelOutput{
		ChannelARN: aws.String(channelARN),
	}, nil
}

func (m *KVSClientMock) DescribeSignalingChannel(ctx context.Context, params *kinesisvideo.DescribeSignalingChannelInput, optFns ...func(*kinesisvideo.Options)) (*kinesisvideo.DescribeSignalingChannelOutput, error) {
	if m.ForceDescribeError {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalFailure",
			Message: "Forced describe signaling channel error",
		}
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	channelName := *params.ChannelName

	channel, exists := m.Channels[channelName]
	if !exists {
		return nil, &kvs_types.ResourceNotFoundException{
			Message: aws.String(fmt.Sprintf("Channel %s not found", channelName)),
		}
	}

	return &kinesisvideo.DescribeSignalingChannelOutput{
		ChannelInfo: &kvs_types.ChannelInfo{
			ChannelARN:  aws.String(channel.ChannelARN),
			ChannelName: aws.String(channel.ChannelName),
			ChannelType: channel.ChannelType,
		},
	}, nil
}

// GetChannelDirect retrieves a channel directly from the mock for testing
func (m *KVSClientMock) GetChannelDirect(channelName string) (*SignalingChannel, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	channel, exists := m.Channels[channelName]
	if !exists {
		return nil, false
	}
	return &channel, true
}
