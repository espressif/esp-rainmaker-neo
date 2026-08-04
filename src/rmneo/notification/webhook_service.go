// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"net/http"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/integrationauth"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// WebhookService implements the NotificationService interface for webhook notifications
type WebhookService struct {
	platform        string
	uri             string
	refreshTokenURI string
	perUserMarshal  func(notification map[string]interface{}, userID string, endpointID string) (map[string]interface{}, error)
}

// NewWebhookService creates a new WebhookService
// platform is the platform of the webhook service, this is used to identify the service for triggering a notification
// uri is the URI of the webhook service
// refreshTokenURI is the URI of the webhook service to refresh the token
// perUserMarshal is an extra Marshalling function that webhook services may define that wishes to marshal the notification specific to each user
func NewWebhookService(platform, uri, refreshTokenURI string, perUserMarshal func(notification map[string]interface{}, userID string, endpointID string) (map[string]interface{}, error)) *WebhookService {
	return &WebhookService{
		platform:        platform,
		uri:             uri,
		refreshTokenURI: refreshTokenURI,
		perUserMarshal:  perUserMarshal,
	}
}

// GetName returns the name of the notification service
func (s *WebhookService) GetName() string {
	return fmt.Sprintf("webhook_%s", s.platform)
}

// GetType returns the type of the notification service
func (s *WebhookService) GetType() NotificationServiceType {
	return NotificationServiceTypeUserSpecific
}

// Send sends a webhook notification
func (s *WebhookService) Send(notif interface{}) error {

	// Webhook notifications are user-specific, so this should not be called directly
	return rmerror.NewRMError(nil, "Webhook notifications must be sent to specific users")
}

// MakeHTTPPostRequest makes an HTTP POST request with the given body, URI, and header closure
func MakeHTTPPostRequest(body []byte, uri string, addHeaders func(*http.Request) error) ([]byte, error) {
	req, err := http.NewRequest("POST", uri, bytes.NewBuffer(body))
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to create HTTP request")
	}

	// Add headers using the closure
	if err := addHeaders(req); err != nil {
		return nil, rmerror.NewRMError(err, "Failed to add headers to request")
	}

	// In mock mode the test-infra API Gateway requires an API key. webhook_mock_api_key is set only during integration tests (by the notification itest fixture), so this is a no-op in production, where these requests hit the real Alexa/Google endpoints.
	if apiKey := os.Getenv("webhook_mock_api_key"); apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	client := httpclient.Get()
	resp, err := client.Do(req)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to send HTTP request")
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to read response body")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("Received non-2XX status code: %d, body: %s", resp.StatusCode, string(respBody)))
	}

	return respBody, nil
}

// refreshToken attempts to refresh an expired token
func (s *WebhookService) refreshToken(refreshToken string) (*integrationauth.TokenResponse, error) {
	rlog.Info(context.TODO()).Msgf("Refreshing token (refresh token fingerprint: %s)", jwtutil.TokenFingerprint(refreshToken))
	body := []byte(fmt.Sprintf(`{"refresh_token": "%s"}`, refreshToken))
	respBody, err := MakeHTTPPostRequest(body, s.refreshTokenURI, func(req *http.Request) error {
		req.Header.Set("Content-Type", "application/json")
		return nil
	})
	if err != nil {
		return nil, err
	}

	var tokenResp integrationauth.TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, rmerror.NewRMError(err, "Failed to decode refresh token response")
	}

	return &tokenResp, nil
}

// SendTo sends a webhook notification to specific users
func (s *WebhookService) SendTo(notif interface{}, userIDs []string) error {
	rlog.Info(context.TODO()).Msgf("Sending webhook notification to users: %v\n", userIDs)
	notification, ok := notif.(map[string]interface{})
	if !ok {
		return rmerror.NewRMError(nil, "Failed to cast notification to map[string]interface{}")
	}

	for _, userID := range userIDs {
		// Multi-endpoint fan-out: a user may have multiple linked accounts on the same integration (e.g. two Amazon accounts both linked to the same Rainmaker user). Send the notification to each linked endpoint.
		endpoints, err := integrationauth.GetAllOAuthEndpoints(userID, s.platform)
		if err != nil {
			rlog.Error(context.TODO()).Err(err).Msgf("Failed to list endpoints for user %s on platform %s", userID, s.platform)
			continue
		}

		for _, endpoint := range endpoints {
			tokenData, err := integrationauth.UpdateAndGetLatestToken(userID, s.platform, endpoint.EndpointID, s.refreshToken)
			if err != nil {
				rlog.Error(context.TODO()).Err(err).Msgf("Failed to get token data for user %s endpoint %s", userID, endpoint.EndpointID)
				continue
			}

			new_notification, err := s.perUserMarshal(notification, userID, endpoint.EndpointID)
			if err != nil {
				rlog.Error(context.TODO()).Err(err).Msgf("Failed to inject user code for user %s", userID)
				continue
			}

			jsonData, err := json.Marshal(new_notification)
			if err != nil {
				rlog.Error(context.TODO()).Err(err).Msg("Failed to marshal notification data")
				continue
			}

			_, err = MakeHTTPPostRequest(jsonData, s.uri, func(req *http.Request) error {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenData.AccessToken))
				return nil
			})

			if err != nil {
				rlog.Error(context.TODO()).Err(err).Msgf("Failed to send webhook notification to user %s endpoint %s", userID, endpoint.EndpointID)
				continue
			}

			rlog.Info(context.TODO()).Msgf("Successfully sent webhook notification to user %s endpoint %s on platform %s\n", userID, endpoint.EndpointID, s.platform)
		}
	}

	return nil
}

// Marshal marshals the notification
func (s *WebhookService) Marshal(notification *Notification) (interface{}, error) {
	body := make(map[string]interface{})

	switch notification.NotificationType {
	case NotificationTypeShadowUpdate:
		if notification.ShadowUpdateData == nil {
			return nil, rmerror.NewRMError(nil, "shadow update data is nil")
		}
		// Format state in traditional IoT shadow format with "reported" wrapper
		body["state"] = notification.ShadowUpdateData.State
		body["node_id"] = notification.ShadowUpdateData.NodeID
		body["topic_name"] = notification.TopicName
		body["notification_type"] = "shadow_update"

	case NotificationTypeDirect:
		if notification.DirectNotificationData == nil {
			return nil, rmerror.NewRMError(nil, "direct notification data is nil")
		}
		body["notify_data"] = notification.DirectNotificationData.NotifyData
		body["node_id"] = notification.DirectNotificationData.NodeID
		body["topic_name"] = notification.TopicName
		body["notification_type"] = "direct"

	default:
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("unsupported notification type: %s", notification.NotificationType))
	}

	return body, nil
}
