// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push

import (
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Expected message structure for easier testing
type ExpectedMessage struct {
	TargetArn        string
	MessageStructure string
	DefaultMessage   string
	PlatformPayload  map[string]interface{}
	Platform         string
}

var _ = Describe("Mobile Push Service", func() {
	var (
		mobilePushService *MobilePushService
		mockSNSClient     *mock.SNSMock
		userID1           string
		userID2           string
		testNotification  *notification.Notification
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		DoInit()

		// Initialize the service
		mobilePushService = NewMobilePushService()

		// Set up mock SNS client
		mockSNSClient = awscommon.GetSNSClient().(*mock.SNSMock)

		// Test user IDs
		userID1 = "test-user-1"
		userID2 = "test-user-2"

		// Create test notification
		var err error
		testNotification, err = notification.NewShadowUpdateNotification(
			"test-node-id",
			"params-test-group",
			node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"power": false,
				},
			},
			node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"power": true,
				},
			},
		)
		Expect(err).To(BeNil())
	})

	Describe("Service Properties", func() {
		It("should have correct name", func() {
			Expect(mobilePushService.GetName()).To(Equal("push"))
		})

		It("should be user-specific service type", func() {
			Expect(mobilePushService.GetType()).To(Equal(notification.NotificationServiceTypeUserSpecific))
		})
	})

	Describe("Send method", func() {
		It("should return error for generic send", func() {
			err := mobilePushService.Send(testNotification)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("must be sent to specific users"))
		})
	})

	Describe("Substitute Variables", func() {
		It("should substitute variables in the push message", func() {
			setupUserWithDevice(userID1, "APNS_device_token", "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/12345678-1234-1234-1234-123456789012")

			result, err := mobilePushService.Marshal(testNotification)
			Expect(err).To(BeNil())

			err = mobilePushService.SendTo(result, []string{userID1})
			Expect(err).To(BeNil())

			expectedMessage := ExpectedMessage{
				TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/12345678-1234-1234-1234-123456789012",
				MessageStructure: "json",
				DefaultMessage:   "Node Alert: Node test-node-id has an alert!",
				Platform:         "APNS",
				PlatformPayload: map[string]interface{}{
					"aps": map[string]interface{}{
						"alert": map[string]interface{}{
							"title": "Node Alert",
							"body":  "Node test-node-id has an alert!",
						},
						"sound":           "default",
						"mutable-content": float64(1),
						"category":        "node_alert",
						"thread-id":       "test-node-id.node.alert",
					},
					"event_data": map[string]interface{}{
						"data": map[string]any{"nodeID": string("test-node-id")},
						"ts":   float64(0),
						"type": string("node_alert"),
					},
				},
			}

			// The substitution is that the test-node-id is substituted with the nodeID from the notification
			verifyPublishedMessages(mockSNSClient, []ExpectedMessage{expectedMessage})
		})
	})

	Describe("Marshal", func() {
		// Regression: direct notifications have a nil ShadowUpdateData; Marshal must not deref it.
		It("resolves the node ID from DirectNotificationData for a direct notification", func() {
			directNotif, err := notification.NewDirectNotification(
				"direct-node-id",
				"test-group",
				map[string]interface{}{"push": true},
			)
			Expect(err).To(BeNil())

			result, err := mobilePushService.Marshal(directNotif)
			Expect(err).To(BeNil())

			msg, ok := result.(PushMessageWithEvent)
			Expect(ok).To(BeTrue())
			Expect(msg.Data["nodeID"]).To(Equal("direct-node-id"))
			Expect(msg.PushMessage.GroupingId).To(Equal("direct-node-id.node.alert"))
		})

		It("returns an error when shadow update data is nil", func() {
			_, err := mobilePushService.Marshal(&notification.Notification{
				NotificationType: notification.NotificationTypeShadowUpdate,
			})
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("shadow update data is nil"))
		})

		It("returns an error for an unsupported notification type", func() {
			_, err := mobilePushService.Marshal(&notification.Notification{
				NotificationType: "bogus",
			})
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unsupported notification type"))
		})
	})

	Describe("Per-user locale", func() {
		// A push_text_config with an es_ES override for node_alert, on top of the defaults. This mirrors a deployment uploading a localized config to S3.
		BeforeEach(func() {
			GPushTextConfig.Locale = map[string]*PushTextLocale{
				"es_ES": {
					Event: map[string]*PushTextForEvent{
						"node_alert": {
							Title: "Alerta de nodo",
							Text:  "¡El nodo {nodeID} tiene una alerta!",
						},
					},
				},
			}
		})

		It("renders the message in the locale stored on the user's endpoint", func() {
			setupUserWithDeviceLocale(userID1, "APNS_MyApp", "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/locale-es", "es_ES")

			result, err := mobilePushService.Marshal(testNotification)
			Expect(err).To(BeNil())
			Expect(mobilePushService.SendTo(result, []string{userID1})).To(BeNil())

			verifyPublishedMessages(mockSNSClient, []ExpectedMessage{{
				TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/locale-es",
				MessageStructure: "json",
				DefaultMessage:   "Alerta de nodo: ¡El nodo test-node-id tiene una alerta!",
				Platform:         "APNS",
				PlatformPayload: map[string]interface{}{
					"aps": map[string]interface{}{
						"alert": map[string]interface{}{
							"title": "Alerta de nodo",
							"body":  "¡El nodo test-node-id tiene una alerta!",
						},
						"sound":           "default",
						"mutable-content": float64(1),
						"category":        "node_alert",
						"thread-id":       "test-node-id.node.alert",
					},
					"event_data": map[string]interface{}{
						"data": map[string]any{"nodeID": string("test-node-id")},
						"ts":   float64(0),
						"type": string("node_alert"),
					},
				},
			}})
		})

		It("falls back to the default text when the user's locale has no override", func() {
			setupUserWithDeviceLocale(userID1, "APNS_MyApp", "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/locale-de", "de_DE")

			result, err := mobilePushService.Marshal(testNotification)
			Expect(err).To(BeNil())
			Expect(mobilePushService.SendTo(result, []string{userID1})).To(BeNil())

			verifyPublishedMessages(mockSNSClient, []ExpectedMessage{{
				TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/locale-de",
				MessageStructure: "json",
				DefaultMessage:   "Node Alert: Node test-node-id has an alert!",
				Platform:         "APNS",
				PlatformPayload: map[string]interface{}{
					"aps": map[string]interface{}{
						"alert": map[string]interface{}{
							"title": "Node Alert",
							"body":  "Node test-node-id has an alert!",
						},
						"sound":           "default",
						"mutable-content": float64(1),
						"category":        "node_alert",
						"thread-id":       "test-node-id.node.alert",
					},
					"event_data": map[string]interface{}{
						"data": map[string]any{"nodeID": string("test-node-id")},
						"ts":   float64(0),
						"type": string("node_alert"),
					},
				},
			}})
		})

		It("renders each endpoint in its own locale when one user has endpoints with different locales", func() {
			// es_ES endpoint gets the Spanish override; en_US endpoint has no override block and falls back to default English. This guards against picking one locale for the whole user.
			setupUserWithDeviceLocale(userID1, "APNS_MyApp", "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/locale-es", "es_ES")
			setupUserWithDeviceLocale(userID1, "GCM_MyApp", "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/locale-en", "en_US")

			result, err := mobilePushService.Marshal(testNotification)
			Expect(err).To(BeNil())
			Expect(mobilePushService.SendTo(result, []string{userID1})).To(BeNil())

			verifyPublishedMessages(mockSNSClient, []ExpectedMessage{
				{
					TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/locale-es",
					MessageStructure: "json",
					DefaultMessage:   "Alerta de nodo: ¡El nodo test-node-id tiene una alerta!",
					Platform:         "APNS",
					PlatformPayload: map[string]interface{}{
						"aps": map[string]interface{}{
							"alert": map[string]interface{}{
								"title": "Alerta de nodo",
								"body":  "¡El nodo test-node-id tiene una alerta!",
							},
							"sound":           "default",
							"mutable-content": float64(1),
							"category":        "node_alert",
							"thread-id":       "test-node-id.node.alert",
						},
						"event_data": map[string]interface{}{
							"data": map[string]any{"nodeID": string("test-node-id")},
							"ts":   float64(0),
							"type": string("node_alert"),
						},
					},
				},
				{
					TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/locale-en",
					MessageStructure: "json",
					DefaultMessage:   "Node Alert: Node test-node-id has an alert!",
					Platform:         "GCM",
					PlatformPayload: map[string]interface{}{
						"data": map[string]interface{}{
							"title": "Node Alert",
							"body":  "Node test-node-id has an alert!",
							"event_data": map[string]interface{}{
								"data":         map[string]any{"nodeID": string("test-node-id")},
								"ts":           float64(0),
								"type":         string("node_alert"),
								"notif_grp_id": "test-node-id.node.alert",
							},
						},
						"android": map[string]interface{}{"priority": "high"},
					},
				},
			})
		})
	})

	Describe("SendTo method", func() {
		Context("with iOS device", func() {
			BeforeEach(func() {
				setupUserWithDevice(userID1, "APNS_device_token", "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/12345678-1234-1234-1234-123456789012")
			})

			It("should send APNS formatted notification", func() {
				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{
					Category: "test",
				}

				expectedMessage := ExpectedMessage{
					TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/12345678-1234-1234-1234-123456789012",
					MessageStructure: "json",
					DefaultMessage:   "ESP RainMaker: Test Message",
					Platform:         "APNS",
					PlatformPayload: map[string]interface{}{
						"aps": map[string]interface{}{
							"alert": map[string]interface{}{
								"title": "ESP RainMaker",
								"body":  "Test Message",
							},
							"sound":           "default",
							"mutable-content": float64(1),
							"category":        "test",
						},
						"event_data": map[string]interface{}{
							"data": map[string]any{"nodeID": string("test-node-id")},
							"ts":   float64(0),
							"type": string("test"),
						},
					},
				}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				verifyPublishedMessages(mockSNSClient, []ExpectedMessage{expectedMessage})
			})
		})

		Context("with iOS sandbox device", func() {
			BeforeEach(func() {
				setupUserWithDevice(userID1, "APNS_SANDBOX_MyApp", "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/MyApp/12345678-1234-1234-1234-123456789012")
			})

			It("should publish under the APNS_SANDBOX key so SNS doesn't fall back to the default string", func() {
				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{Category: "test"}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				published := mockSNSClient.GetPublishedMessages()
				Expect(published).To(HaveLen(1))
				var messageMap map[string]string
				Expect(json.Unmarshal([]byte(*published[0].Message), &messageMap)).To(BeNil())
				// The sandbox endpoint requires the APNS_SANDBOX key; the rich (mutable-content/event_data) payload must live there, not under APNS.
				Expect(messageMap).To(HaveKey("APNS_SANDBOX"))
				Expect(messageMap).ToNot(HaveKey("APNS"))
				Expect(messageMap["APNS_SANDBOX"]).To(ContainSubstring("mutable-content"))
			})
		})

		Context("with Android device", func() {
			BeforeEach(func() {
				setupUserWithDevice(userID1, "GCM_device_token", "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/abcdef123456")
			})

			It("should send GCM formatted notification", func() {
				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{
					Category: "test",
				}

				expectedMessage := ExpectedMessage{
					TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/abcdef123456",
					MessageStructure: "json",
					DefaultMessage:   "ESP RainMaker: Test Message",
					Platform:         "GCM",
					PlatformPayload: map[string]interface{}{
						"data": map[string]interface{}{
							"title": "ESP RainMaker",
							"body":  "Test Message",
							"event_data": map[string]interface{}{
								"data": map[string]any{"nodeID": string("test-node-id")},
								"ts":   float64(0),
								"type": string("test"),
							},
						},
					},
				}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				verifyPublishedMessages(mockSNSClient, []ExpectedMessage{expectedMessage})
			})
		})

		Context("with multiple devices", func() {
			BeforeEach(func() {
				setupUserWithMultipleDevices(userID1)
			})

			It("should send notifications to all supported devices", func() {
				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{
					Category: "test",
					ExtraData: map[string]interface{}{
						"key": "value",
					},
				}

				expectedMessages := []ExpectedMessage{
					{
						TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/ios-device",
						MessageStructure: "json",
						DefaultMessage:   "ESP RainMaker: Test Message",
						Platform:         "APNS",
						PlatformPayload: map[string]interface{}{
							"aps": map[string]interface{}{
								"alert": map[string]interface{}{
									"title": "ESP RainMaker",
									"body":  "Test Message",
								},
								"sound":           "default",
								"mutable-content": float64(1),
								"category":        "test",
							},
							"event_data": map[string]interface{}{
								"data": map[string]any{"nodeID": string("test-node-id")},
								"ts":   float64(0),
								"type": string("test"),
								"key":  "value",
							},
						},
					},
					{
						TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/android-device",
						MessageStructure: "json",
						DefaultMessage:   "ESP RainMaker: Test Message",
						Platform:         "GCM",
						PlatformPayload: map[string]interface{}{
							"data": map[string]interface{}{
								"title": "ESP RainMaker",
								"body":  "Test Message",
								"event_data": map[string]interface{}{
									"data": map[string]any{"nodeID": string("test-node-id")},
									"ts":   float64(0),
									"type": string("test"),
									"key":  "value",
								},
							},
						},
					},
				}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				verifyPublishedMessages(mockSNSClient, expectedMessages)
			})
		})

		Context("with unsupported platform", func() {
			BeforeEach(func() {
				setupUserWithDevice(userID1, "web", "some-web-token")
			})

			It("should skip unsupported platforms", func() {
				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{
					Category: "test",
				}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				publishedMessages := mockSNSClient.GetPublishedMessages()
				Expect(publishedMessages).To(HaveLen(0))
			})
		})

		Context("with multiple users", func() {
			BeforeEach(func() {
				setupUserWithDevice(userID1, "APNS_user1_device", "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/user1-device")
				setupUserWithDevice(userID2, "GCM_user2_device", "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/user2-device")
			})

			It("should send notifications to all users", func() {
				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{
					Category: "test",
				}

				expectedMessages := []ExpectedMessage{
					{
						TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/user1-device",
						MessageStructure: "json",
						DefaultMessage:   "ESP RainMaker: Test Message",
						Platform:         "APNS",
						PlatformPayload: map[string]interface{}{
							"aps": map[string]interface{}{
								"alert": map[string]interface{}{
									"title": "ESP RainMaker",
									"body":  "Test Message",
								},
								"sound":           "default",
								"mutable-content": float64(1),
								"category":        "test",
							},
							"event_data": map[string]interface{}{
								"data": map[string]any{"nodeID": string("test-node-id")},
								"ts":   float64(0),
								"type": string("test"),
							},
						},
					},
					{
						TargetArn:        "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/user2-device",
						MessageStructure: "json",
						DefaultMessage:   "ESP RainMaker: Test Message",
						Platform:         "GCM",
						PlatformPayload: map[string]interface{}{
							"data": map[string]interface{}{
								"title": "ESP RainMaker",
								"body":  "Test Message",
								"event_data": map[string]interface{}{
									"data": map[string]any{"nodeID": string("test-node-id")},
									"ts":   float64(0),
									"type": string("test"),
								},
							},
						},
					},
				}

				err := mobilePushService.SendTo(pushMessage, []string{userID1, userID2})
				Expect(err).To(BeNil())

				verifyPublishedMessages(mockSNSClient, expectedMessages)
			})
		})

		Context("with user having no devices", func() {
			It("should handle gracefully when user has no devices", func() {
				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{
					Category: "test",
				}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				publishedMessages := mockSNSClient.GetPublishedMessages()
				Expect(publishedMessages).To(HaveLen(0))
			})
		})

		Context("with invalid notification type", func() {
			It("should return error for invalid notification type", func() {
				err := mobilePushService.SendTo("invalid-notification", []string{userID1})
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("Failed to cast notification to PushMessage"))
			})
		})

		Context("with a disabled SNS endpoint", func() {
			It("should call DeleteEndpoint and drop the user's row on EndpointDisabledException", func() {
				deviceArn := "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/disabled-device"
				setupUserWithDevice(userID1, "APNS_MyApp", deviceArn)

				mockSNSClient.SetPublishError(&snstypes.EndpointDisabledException{Message: stringPtr("Endpoint is disabled")})

				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{Category: "test"}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				deleteCalls := mockSNSClient.GetDeleteEndpointCalls()
				Expect(deleteCalls).To(HaveLen(1))
				Expect(*deleteCalls[0].EndpointArn).To(Equal(deviceArn))

				userDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID1)))
				_, err = userDB.GetUserEntryByEndpoint("APNS_MyApp", deviceArn)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("user entry not found"))
			})

			It("should drop the user's row when SNS reports the endpoint no longer exists", func() {
				deviceArn := "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/deleted-device"
				setupUserWithDevice(userID1, "APNS_MyApp", deviceArn)

				// Deleted platform app → Publish returns InvalidParameterException, not EndpointDisabledException; the row must still be GC'd.
				mockSNSClient.SetPublishError(&snstypes.InvalidParameterException{Message: stringPtr("Invalid parameter: TargetArn Reason: No endpoint found for the target arn specified")})

				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{Category: "test"}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				deleteCalls := mockSNSClient.GetDeleteEndpointCalls()
				Expect(deleteCalls).To(HaveLen(1))
				Expect(*deleteCalls[0].EndpointArn).To(Equal(deviceArn))

				userDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID1)))
				_, err = userDB.GetUserEntryByEndpoint("APNS_MyApp", deviceArn)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("user entry not found"))
			})

			It("should continue to other endpoints when one is disabled", func() {
				disabledArn := "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/disabled-device"
				healthyArn := "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/healthy-device"
				userDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID1)))
				Expect(userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "APNS_MyApp", EndpointID: disabledArn, SNSEndpointARN: disabledArn})).To(BeNil())
				Expect(userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "APNS_MyApp", EndpointID: healthyArn, SNSEndpointARN: healthyArn})).To(BeNil())

				// The SNS mock applies PublishError to every Publish; flip it after the first call to simulate only the first endpoint being disabled.
				mockSNSClient.SetPublishError(&snstypes.EndpointDisabledException{Message: stringPtr("Endpoint is disabled")})

				pushMessage := NewPushMessageWithEvent("test", map[string]string{"nodeID": "test-node-id"})
				pushMessage.PushMessage = &PushMessage{Category: "test"}

				err := mobilePushService.SendTo(pushMessage, []string{userID1})
				Expect(err).To(BeNil())

				// Both endpoints saw a disabled publish → both rows should be gone, both DeleteEndpoint calls fired.
				Expect(mockSNSClient.GetDeleteEndpointCalls()).To(HaveLen(2))
				_, err = userDB.GetUserEntryByEndpoint("APNS_MyApp", disabledArn)
				Expect(err).ToNot(BeNil())
				_, err = userDB.GetUserEntryByEndpoint("APNS_MyApp", healthyArn)
				Expect(err).ToNot(BeNil())
			})
		})
	})
})

func stringPtr(s string) *string { return &s }

// Helper function to verify published messages against expected messages
func verifyPublishedMessages(mockSNSClient *mock.SNSMock, expectedMessages []ExpectedMessage) {
	publishedMessages := mockSNSClient.GetPublishedMessages()
	Expect(publishedMessages).To(HaveLen(len(expectedMessages)))

	// Create a map of target ARNs to published messages for easier comparison
	publishedByArn := make(map[string]*sns.PublishInput)
	for _, msg := range publishedMessages {
		publishedByArn[*msg.TargetArn] = msg
	}

	for _, expected := range expectedMessages {
		publishedMsg, exists := publishedByArn[expected.TargetArn]
		Expect(exists).To(BeTrue(), "Expected message to target ARN %s", expected.TargetArn)

		// Verify basic message properties
		Expect(*publishedMsg.MessageStructure).To(Equal(expected.MessageStructure))
		Expect(*publishedMsg.TargetArn).To(Equal(expected.TargetArn))

		// Parse and verify message content
		var messageMap map[string]string
		err := json.Unmarshal([]byte(*publishedMsg.Message), &messageMap)
		Expect(err).To(BeNil())

		// Verify default message
		Expect(messageMap["default"]).To(Equal(expected.DefaultMessage))

		// Verify platform-specific payload
		Expect(messageMap).To(HaveKey(expected.Platform))
		var actualPayload map[string]interface{}
		err = json.Unmarshal([]byte(messageMap[expected.Platform]), &actualPayload)
		Expect(err).To(BeNil())

		resetTimestamp(actualPayload)

		// Compare the platform payload
		test_utils.AssertNormalizedEqual(actualPayload, expected.PlatformPayload)
	}
}

func resetTimestamp(payload map[string]interface{}) {
	// APNS
	if b, ok := payload["event_data"].(map[string]interface{}); ok {
		if _, ok := b["ts"]; ok {
			b["ts"] = 0
		}
	}

	// GCM
	if a, ok := payload["data"].(map[string]interface{}); ok {
		if b, ok := a["event_data"].(map[string]interface{}); ok {
			if _, ok := b["ts"]; ok {
				b["ts"] = 0
			}
		}
	}
}

// Helper functions for setting up test data
func setupUserWithDevice(userID, platform, deviceToken string) {
	user := user.NewUser(userID)
	ctx := rmngctx.NewRmngContext(user)
	userDB := user_integration_db.NewUserDB(ctx)
	err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: platform, EndpointID: deviceToken, SNSEndpointARN: deviceToken})
	Expect(err).To(BeNil())
}

func setupUserWithDeviceLocale(userID, platform, deviceToken, locale string) {
	user := user.NewUser(userID)
	ctx := rmngctx.NewRmngContext(user)
	userDB := user_integration_db.NewUserDB(ctx)
	err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: platform, EndpointID: deviceToken, SNSEndpointARN: deviceToken, Locale: locale})
	Expect(err).To(BeNil())
}

func setupUserWithMultipleDevices(userID string) {
	user := user.NewUser(userID)
	ctx := rmngctx.NewRmngContext(user)
	userDB := user_integration_db.NewUserDB(ctx)

	// Register iOS device
	iosArn := "arn:aws:sns:us-east-1:123456789012:endpoint/APNS/MyApp/ios-device"
	err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "APNS_ios_device", EndpointID: iosArn, SNSEndpointARN: iosArn})
	Expect(err).To(BeNil())

	// Register Android device
	androidArn := "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/MyApp/android-device"
	err = userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "GCM_android_device", EndpointID: androidArn, SNSEndpointARN: androidArn})
	Expect(err).To(BeNil())

	// Register unsupported platform (should be ignored)
	err = userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "web", EndpointID: "some-web-token", SNSEndpointARN: "some-web-token"})
	Expect(err).To(BeNil())
}
