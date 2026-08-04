// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// MobilePushService implements the NotificationService interface for mobile push notifications
type MobilePushService struct{}

// NewMobilePushService creates a new MobilePushService
func NewMobilePushService() *MobilePushService {
	return &MobilePushService{}
}

// GetName returns the name of the notification service
func (s *MobilePushService) GetName() string {
	return "push"
}

// GetType returns the type of the notification service
func (s *MobilePushService) GetType() notification.NotificationServiceType {
	return notification.NotificationServiceTypeUserSpecific
}

// Send sends a mobile push notification
func (s *MobilePushService) Send(notification interface{}) error {
	rlog.Error(context.TODO()).Msg("Mobile push notifications must be sent to specific users")
	return rmerror.NewRMError(nil, "Mobile push notifications must be sent to specific users")
}

// SendTo sends mobile push notifications to specific users
func (s *MobilePushService) SendTo(notif interface{}, userIDs []string) error {
	rlog.Info(context.TODO()).Msgf("Sending mobile push notification to users: %v", userIDs)

	// Test-only: reload the push text config from S3 per invocation so itests see config changes without a cold start. Never set in production.
	if os.Getenv("test_push_text_config_no_cache") != "" {
		DoInit()
	}

	pushMessageForEvent, ok := notif.(PushMessageWithEvent)
	if !ok {
		return rmerror.NewRMError(nil, "Failed to cast notification to PushMessageForEvent")
	}
	if pushMessageForEvent.PushMessage.ExtraData == nil {
		pushMessageForEvent.PushMessage.ExtraData = make(map[string]interface{})
	}
	pushMessageForEvent.PushMessage.ExtraData["type"] = pushMessageForEvent.Name
	pushMessageForEvent.PushMessage.ExtraData["ts"] = time.Now().Unix()
	pushMessageForEvent.PushMessage.ExtraData["data"] = pushMessageForEvent.Data

	snsClient := awscommon.GetSNSClient()

	for _, userID := range userIDs {
		// Get user entries for the user
		err := s.sendToUser(snsClient, pushMessageForEvent, userID)
		if err != nil {
			rlog.Error(context.TODO()).Err(err).Msgf("Failed to send push notification to user %s", userID)
			continue
		}

		rlog.Info(context.TODO()).Msgf("Successfully sent push notification to user %s", userID)
	}

	return nil
}

// sendToUser sends push notifications to a specific user's devices
func (s *MobilePushService) sendToUser(snsClient awscommon.SNSClientInterface, pushMessageForEvent PushMessageWithEvent, userID string) error {
	// Create user context for database operations
	user := user.NewUser(userID)
	ctx := rmngctx.NewRmngContext(user)
	userDB := user_integration_db.NewUserDB(ctx)

	// Get all user entries for the user
	userEntries, err := userDB.GetUserEntries()
	if err != nil {
		return rmerror.NewRMError(err, "Failed to get user entries")
	}

	if len(userEntries) == 0 {
		rlog.Info(ctx).Msgf("No user entries found for user %s", userID)
		return nil
	}

	for _, userEntry := range userEntries { // TODO: Use SNS publish batch API
		pushMessageForEvent.LoadMessage(userEntry.Locale)
		err := s.sendToDevice(snsClient, pushMessageForEvent.PushMessage, &userEntry)
		if err != nil {
			if isEndpointUnusableErr(err) {
				cleanupUnusableEndpoint(ctx, snsClient, userDB, &userEntry)
				continue
			}
			rlog.Error(ctx).Err(err).Msgf("Failed to send push notification to integration %s for user %s", userEntry.IntegrationID, userID)
			continue
		}
		rlog.Info(ctx).Msgf("Successfully sent push notification to %s integration for user %s", userEntry.IntegrationID, userID)
	}

	return nil
}

// isEndpointUnusableErr reports whether an SNS Publish error means the endpoint should be GC'd: provider-disabled, or no longer existing (e.g. its platform application was deleted).
func isEndpointUnusableErr(err error) bool {
	if err == nil {
		return false
	}
	var disabled *snstypes.EndpointDisabledException
	if errors.As(err, &disabled) {
		return true
	}
	var notFound *snstypes.NotFoundException
	if errors.As(err, &notFound) {
		return true
	}
	var invalid *snstypes.InvalidParameterException
	if errors.As(err, &invalid) {
		msg := strings.ToLower(invalid.ErrorMessage())
		if strings.Contains(msg, "disabled") || strings.Contains(msg, "no endpoint found") {
			return true
		}
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "endpoint is disabled") ||
		strings.Contains(lower, "no endpoint found for the target arn") ||
		strings.Contains(lower, "endpoint does not exist")
}

// cleanupUnusableEndpoint deletes the SNS endpoint (idempotent) and its DynamoDB row so the next fanout doesn't retry it. The client re-registers on next launch.
func cleanupUnusableEndpoint(ctx *rmngctx.RmngContext, snsClient awscommon.SNSClientInterface, userDB *user_integration_db.UserDB, entry *user_integration_db.UserIntegrationEntry) {
	rlog.Info(ctx).Msgf("Cleaning up unusable endpoint %s (integration=%s) for user %s", entry.EndpointID, entry.IntegrationID, ctx.Accessor.GetID())
	if entry.SNSEndpointARN != "" {
		if _, err := snsClient.DeleteEndpoint(ctx.Context, &sns.DeleteEndpointInput{EndpointArn: aws.String(entry.SNSEndpointARN)}); err != nil {
			rlog.Error(ctx).Err(err).Msgf("Failed to delete SNS endpoint %s", entry.SNSEndpointARN)
		}
	}
	if err := userDB.UnregisterClient(entry.IntegrationID, entry.EndpointID); err != nil {
		rlog.Error(ctx).Err(err).Msgf("Failed to unregister unusable endpoint row (integration=%s, endpoint=%s)", entry.IntegrationID, entry.EndpointID)
	}
}

// sendToDevice sends a push notification to a specific device
func (s *MobilePushService) sendToDevice(snsClient awscommon.SNSClientInterface, pushMessage *PushMessage, userEntry *user_integration_db.UserIntegrationEntry) error {
	var formatter MessageFormatter
	var targetPlatform string // logical platform, used for the test uuid suffix
	var snsMessageKey string  // SNS message-structure key; must match the endpoint's platform or SNS silently falls back to the "default" string

	// Select the formatter and SNS platform key from the integration_id prefix.
	// APNS_SANDBOX_ must be checked before APNS_ since it also starts with "APNS_".
	switch {
	case strings.HasPrefix(userEntry.IntegrationID, "APNS_SANDBOX_"):
		formatter = NewAPNSFormatter()
		targetPlatform = "APNS"
		snsMessageKey = "APNS_SANDBOX"
	case strings.HasPrefix(userEntry.IntegrationID, "APNS_"), strings.HasPrefix(userEntry.IntegrationID, "MOCK_APNS_"):
		formatter = NewAPNSFormatter()
		targetPlatform = "APNS"
		snsMessageKey = "APNS"
	case strings.HasPrefix(userEntry.IntegrationID, "GCM_"), strings.HasPrefix(userEntry.IntegrationID, "MOCK_GCM_"):
		formatter = NewGCMFormatter()
		targetPlatform = "GCM"
		snsMessageKey = "GCM"
	default:
		return rmerror.NewRMError(nil, "Unsupported integration_id for push: "+userEntry.IntegrationID)
	}

	mockRandom := os.Getenv("mock_random")
	if mockRandom != "" {
		pushMessage.ExtraData["uuid"] = mockRandom + "_" + targetPlatform
	}

	formattedMessage, msgAttrs, err := formatter.FormatMessage(pushMessage)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to format message for integration "+userEntry.IntegrationID)
	}

	message := map[string]string{
		"default":     pushMessage.Title + ": " + pushMessage.Text,
		snsMessageKey: formattedMessage,
	}

	rlog.Info(context.TODO()).Interface("message", message).Send()
	messageJSON, err := json.Marshal(message)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to marshal SNS message")
	}

	pushMockSqsUrl := os.Getenv("push_mock_sqs_url")
	if pushMockSqsUrl != "" {
		ForTestingPushToSQS(messageJSON, userEntry.SNSEndpointARN, pushMockSqsUrl)
	} else {
		publishInput := &sns.PublishInput{
			Message:          aws.String(string(messageJSON)),
			MessageStructure: aws.String("json"),
			TargetArn:        aws.String(userEntry.SNSEndpointARN),
		}

		if len(msgAttrs) > 0 {
			publishInput.MessageAttributes = msgAttrs
		}

		_, err = snsClient.Publish(context.Background(), publishInput)
		if err != nil {
			return rmerror.NewRMError(err, "Failed to publish to SNS")
		}
	}

	return nil
}

// Marshal resolves the node ID by notification type; direct notifications have a nil ShadowUpdateData, so reading it blindly panics.
func (s *MobilePushService) Marshal(notif *notification.Notification) (interface{}, error) {
	var nodeID string
	switch notif.NotificationType {
	case notification.NotificationTypeShadowUpdate:
		if notif.ShadowUpdateData == nil {
			return nil, rmerror.NewRMError(nil, "shadow update data is nil")
		}
		nodeID = notif.ShadowUpdateData.NodeID
	case notification.NotificationTypeDirect:
		if notif.DirectNotificationData == nil {
			return nil, rmerror.NewRMError(nil, "direct notification data is nil")
		}
		nodeID = notif.DirectNotificationData.NodeID
	default:
		return nil, rmerror.NewRMError(nil, "unsupported notification type: "+string(notif.NotificationType))
	}

	return PushMessageWithEvent{
		Name: "node_alert",
		Data: map[string]string{
			"nodeID": nodeID,
		},
		PushMessage: &PushMessage{
			Category:   "node_alert",
			GroupingId: nodeID + ".node.alert",
		},
	}, nil
}

// ForTestingPushToSQS is a test function to push a message to an SQS queue - so that a test utility can read and validate it
func ForTestingPushToSQS(messageJSON []byte, deviceToken string, pushMockSqsUrl string) {
	// Test code path
	msg := map[string]interface{}{
		"Message":          string(messageJSON),
		"MessageStructure": "json",
		"TargetArn":        deviceToken,
	}
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		rlog.Error(context.TODO()).Err(err).Msg("Failed to marshal SNS message")
		return
	}
	// Write to the queue
	queue := awscommon.GetSQSClient()
	_, err = queue.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    aws.String(pushMockSqsUrl),
		MessageBody: aws.String(string(msgJSON)),
	})
	if err != nil {
		// This is necessary here - as it is test code path
		rlog.Error(context.TODO()).Err(err).Msg("Failed to send message to SQS")
		return
	}
}
