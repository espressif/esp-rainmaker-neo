// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"fmt"
)

/* A mock webhook service is hosted at the configured mock base URL, which exposes the following APIs
* - POST /v1/token which accepts a json body with field refresh_token, and returns a refresh_token and access_token
* - POST /v1/data which accepts the POST data of the webhook and returns a 200 status code. A field "uuid" is mandatory
    in the body of the request
*
* For validating that the data was correctly received in /v1/data, there is a GET /v1/validate?uuid={uuid} which returns the data
* that was received in /v1/data.
*/

// WebhookMockService implements the NotificationService interface for mock webhook notifications
type WebhookMockService struct {
	*WebhookService
}

// NewWebhookMockService creates a new WebhookMockService pointed at baseURL.
func NewWebhookMockService(baseURL string) *WebhookMockService {
	return &WebhookMockService{
		WebhookService: NewWebhookService("webhook_mock", fmt.Sprintf("%s/v1/data", baseURL), fmt.Sprintf("%s/v1/token", baseURL), InjectUserCode),
	}
}

// GetName returns the name of the notification service
func (s *WebhookMockService) GetName() string {
	return "webhook_mock"
}

// Marshal marshals the notification
func (s *WebhookMockService) Marshal(notification *Notification) (interface{}, error) {
	// Use the base webhook service to get the formatted data
	baseData, err := s.WebhookService.Marshal(notification)
	if err != nil {
		return nil, err
	}

	// Wrap in the mock service format
	reqBody := map[string]interface{}{
		"data": baseData,
	}

	return reqBody, nil
}

func InjectUserCode(notification map[string]interface{}, userID string, endpointID string) (map[string]interface{}, error) {
	// uuid keys the mock's per-delivery store. Include endpointID so a user with
	// multiple endpoints on this integration produces a distinct uuid per
	// endpoint instead of overwriting on a shared userID key.
	notification["uuid"] = userID + "#" + endpointID
	return notification, nil
}
