// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push

import (
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// deepCopyJSON creates a deep copy using JSON marshal/unmarshal (generic but slower)
func deepCopyJSON[T any](original T) (T, error) {
	var copy T
	data, err := json.Marshal(original)
	if err != nil {
		return copy, err
	}
	err = json.Unmarshal(data, &copy)
	return copy, err
}

// PushMessage represents a generic push notification message
type PushMessage struct {
	PushTextForEvent
	ExtraData map[string]interface{} `json:"data"`
	Category  string                 `json:"category"`
	// GroupingId allows grouping of notifications into a single thread
	GroupingId string
}

// MessageFormatter interface defines methods for formatting messages for different platforms
type MessageFormatter interface {
	FormatMessage(msg *PushMessage) (string, map[string]types.MessageAttributeValue, error)
}

// GCMFormatter implements MessageFormatter for Google Cloud Messaging
type GCMFormatter struct{}

// APNSFormatter implements MessageFormatter for Apple Push Notification Service
type APNSFormatter struct{}

// FormatMessage formats the message for GCM
func (f *GCMFormatter) FormatMessage(originalMsg *PushMessage) (string, map[string]types.MessageAttributeValue, error) {
	// Create a deep copy to avoid modifying the original
	msg, err := deepCopyJSON(originalMsg)
	if err != nil {
		return "", nil, err
	}

	gcmPayload := make(map[string]interface{})

	gcmPayload["data"] = map[string]interface{}{
		"title": msg.Title,
		"body":  msg.Text,
	}
	// We ignore the category field for GCM

	if msg.ExtraData != nil {
		gcmPayload["data"].(map[string]interface{})["event_data"] = msg.ExtraData
	}

	if msg.Priority != "" && msg.Priority != "low" {
		// GCM doesn't support 'low' priority
		// TODO: `android` is a native FCM v1 key; SNS ignores it in our legacy payload, so priority never reaches the device. To enable it, switch to native fcmV1Message.message{...}. See https://docs.aws.amazon.com/sns/latest/dg/sns-fcm-v1-payloads.html
		gcmPayload["android"] = map[string]interface{}{
			"priority": msg.Priority,
		}
	}

	// We map groupingId to an entry within event_data
	if msg.GroupingId != "" {
		gcmPayload["data"].(map[string]interface{})["event_data"].(map[string]interface{})["notif_grp_id"] = msg.GroupingId
	}

	jsonBytes, err := json.Marshal(gcmPayload)
	if err != nil {
		return "", nil, err
	}
	return string(jsonBytes), nil, nil
}

// FormatMessage formats the message for APNS
func (f *APNSFormatter) FormatMessage(originalMsg *PushMessage) (string, map[string]types.MessageAttributeValue, error) {
	// Create a deep copy to avoid modifying the original
	msg, err := deepCopyJSON(originalMsg)
	if err != nil {
		return "", nil, err
	}
	MsgAttrs := make(map[string]types.MessageAttributeValue)

	apsPayload := map[string]interface{}{
		"aps": make(map[string]interface{}),
	}

	apsPayload["aps"] = map[string]interface{}{
		"alert": map[string]interface{}{
			"title": msg.Title,
			"body":  msg.Text,
		},
		"sound":           "default",
		"mutable-content": 1,
	}
	if msg.ExtraData != nil {
		apsPayload["event_data"] = msg.ExtraData
	}

	if msg.Category != "" {
		apsPayload["aps"].(map[string]interface{})["category"] = msg.Category
	}

	if msg.Priority != "" {
		priority := "default"
		switch msg.Priority {
		case "low":
			priority = "1"
		case "normal":
			priority = "5"
		case "high":
			priority = "10"
		}

		MsgAttrs["AWS.SNS.MOBILE.APNS.PRIORITY"] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(priority),
		}
	}

	if msg.GroupingId != "" {
		apsPayload["aps"].(map[string]interface{})["thread-id"] = msg.GroupingId
	}

	jsonBytes, err := json.Marshal(apsPayload)
	if err != nil {
		return "", nil, err
	}
	return string(jsonBytes), MsgAttrs, nil
}

// NewGCMFormatter creates a new GCM formatter
func NewGCMFormatter() MessageFormatter {
	return &GCMFormatter{}
}

// NewAPNSFormatter creates a new APNS formatter
func NewAPNSFormatter() MessageFormatter {
	return &APNSFormatter{}
}
