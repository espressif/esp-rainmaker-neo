// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/testmock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

/* The itest webhook mock, located through testmock, exposes:
* - POST /v1/token which accepts a json body with field refresh_token, and returns a refresh_token and access_token
* - POST /v1/data which accepts the POST data of the webhook and returns a 200 status code. A field "uuid" is mandatory
    in the body of the request
*
* For validating that the data was correctly received in /v1/data, there is a GET /v1/validate?uuid={uuid} which returns the data
* that was received in /v1/data.
*/

// WebhookMockService delivers the webhook_mock integration, which only itests register, to the itest webhook mock.
type WebhookMockService struct {
	*WebhookService
}

func NewWebhookMockService() *WebhookMockService {
	return &WebhookMockService{
		WebhookService: NewWebhookService("webhook_mock", "", "", InjectUserCode),
	}
}

// GetName returns the name of the notification service
func (s *WebhookMockService) GetName() string {
	return "webhook_mock"
}

// SendTo resolves the mock per send, so an account that deploys it after a cold start still delivers.
func (s *WebhookMockService) SendTo(ctx context.Context, notif interface{}, userIDs []string) error {
	cfg, ok := testmock.Get(ctx)
	if !ok {
		rlog.Warn(ctx).Msg("dropping a webhook_mock notification: no itest webhook mock is deployed in this account")
		return nil
	}
	return NewWebhookService("webhook_mock", cfg.URL("/v1/data"), cfg.URL("/v1/token"), InjectUserCode).SendTo(ctx, notif, userIDs)
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
