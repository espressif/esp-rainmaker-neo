// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/sharing_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/gva"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/push"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// alexaEUGateway is the event gateway for the eu-west-1 region used by the Alexa
// test fixtures below (their endpoints are registered with Region "eu-west-1").
const alexaEUGateway = "https://api.eu.amazonalexa.com/v3/events"

func resetPushNotificationTimestamp(payload map[string]interface{}) {
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

// MockNotificationService implements the NotificationService interface for testing
type MockNotificationService struct {
	Name              string
	Type              notification.NotificationServiceType
	SentNotifications []*notification.Notification
	SentToUsers       map[string][]string
}

func NewMockNotificationService(name string, serviceType notification.NotificationServiceType) *MockNotificationService {
	return &MockNotificationService{
		Name:              name,
		Type:              serviceType,
		SentNotifications: make([]*notification.Notification, 0),
		SentToUsers:       make(map[string][]string),
	}
}

func (m *MockNotificationService) GetName() string {
	return m.Name
}

func (m *MockNotificationService) GetType() notification.NotificationServiceType {
	return m.Type
}

func (m *MockNotificationService) Send(notif interface{}) error {
	notifData, ok := notif.(*notification.Notification)
	if !ok {
		return fmt.Errorf("failed to cast notification to Notification")
	}
	m.SentNotifications = append(m.SentNotifications, notifData)
	return nil
}

func (m *MockNotificationService) SendTo(notif interface{}, userIDs []string) error {
	notifData, ok := notif.(*notification.Notification)
	if !ok {
		return fmt.Errorf("failed to cast notification to Notification")
	}
	m.SentNotifications = append(m.SentNotifications, notifData)

	// Use appropriate nodeID based on notification type
	var nodeID string
	switch notifData.NotificationType {
	case notification.NotificationTypeShadowUpdate:
		if notifData.ShadowUpdateData != nil {
			nodeID = notifData.ShadowUpdateData.NodeID
		}
	case notification.NotificationTypeDirect:
		if notifData.DirectNotificationData != nil {
			nodeID = notifData.DirectNotificationData.NodeID
		}
	}

	if nodeID != "" {
		m.SentToUsers[nodeID] = userIDs
	}
	return nil
}

func (m *MockNotificationService) Marshal(notification *notification.Notification) (interface{}, error) {
	return notification, nil
}

var profiles = map[string]*mock.Profile{}

var _ = Describe("Notification Constructors", func() {
	Describe("NewDirectNotification", func() {
		It("should parse group information from notify topic name", func() {
			notif, err := notification.NewDirectNotification("test-node", "group123-sub1-sub2", map[string]interface{}{})
			Expect(err).To(BeNil())
			Expect(notif.GroupID).To(Equal("group123"))
			Expect(notif.SubGroupIDs).To(Equal([]string{"sub1", "sub2"}))
			Expect(notif.TopicName).To(Equal("group123-sub1-sub2"))
		})

		It("should handle notify topic without subgroups", func() {
			notif, err := notification.NewDirectNotification("test-node", "group123", map[string]interface{}{})
			Expect(err).To(BeNil())
			Expect(notif.GroupID).To(Equal("group123"))
			Expect(notif.SubGroupIDs).To(BeEmpty())
		})

		It("should return error for empty notify topic", func() {
			_, err := notification.NewDirectNotification("test-node", "", map[string]interface{}{})
			Expect(err).ToNot(BeNil())
		})
	})

	Describe("NewShadowUpdateNotification", func() {
		It("should parse group information from shadow name", func() {
			emptyState := node.ReportedOrDesiredShadow{}
			notif, err := notification.NewShadowUpdateNotification("test-node", "params-group123-sub1-sub2", emptyState, emptyState)
			Expect(err).To(BeNil())
			Expect(notif.GroupID).To(Equal("group123"))
			Expect(notif.SubGroupIDs).To(Equal([]string{"sub1", "sub2"}))
			Expect(notif.TopicName).To(Equal("params-group123-sub1-sub2"))
		})

		It("should handle shadow name without subgroups", func() {
			emptyState := node.ReportedOrDesiredShadow{}
			notif, err := notification.NewShadowUpdateNotification("test-node", "params-group123", emptyState, emptyState)
			Expect(err).To(BeNil())
			Expect(notif.GroupID).To(Equal("group123"))
			Expect(notif.SubGroupIDs).To(BeEmpty())
		})

		It("should return error for invalid shadow name format", func() {
			emptyState := node.ReportedOrDesiredShadow{}
			_, err := notification.NewShadowUpdateNotification("test-node", "invalid-format", emptyState, emptyState)
			Expect(err).ToNot(BeNil())
		})
	})
})

var _ = Describe("resolveMockBaseURL", func() {
	// webhook_mock_base_url is read from the real process environment, so each
	// spec snapshots and restores it to avoid leaking state into unrelated specs.
	var originalBase string
	var baseWasSet bool

	BeforeEach(func() {
		originalBase, baseWasSet = os.LookupEnv("webhook_mock_base_url")
	})

	AfterEach(func() {
		if baseWasSet {
			os.Setenv("webhook_mock_base_url", originalBase)
		} else {
			os.Unsetenv("webhook_mock_base_url")
		}
	})

	It("resolves to the base URL when webhook_mock_base_url is set", func() {
		os.Setenv("webhook_mock_base_url", "https://webhook-mock.test")
		Expect(resolveMockBaseURL()).To(Equal("https://webhook-mock.test"))
	})

	It("resolves to empty (production) when webhook_mock_base_url is unset", func() {
		os.Unsetenv("webhook_mock_base_url")
		Expect(resolveMockBaseURL()).To(Equal(""))
	})
})

var _ = Describe("Notifications Handler", func() {
	var (
		ctx                 context.Context
		nodeID              string
		groupID             string
		userID              string
		userSpecificService *MockNotificationService
		genericService      *MockNotificationService
		webhookService      *notification.WebhookService
		mockHTTPClient      *mock.MockHTTPClient
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		ctx = context.Background()
		nodeID = "test-node-id"
		groupName := "test-group-id"
		userID = "test-user-id"

		// Create a test user and context
		testUser := user.NewUser(userID)
		rmngCtx := rmngctx.NewRmngContext(testUser)

		grp, err := group.CreateGroupForUser(rmngCtx, groupName)
		Expect(err).To(BeNil())
		groupID = grp.GroupID

		// Initialize notification services
		notification.Initialize()
		userSpecificService = NewMockNotificationService("user_specific", notification.NotificationServiceTypeUserSpecific)
		genericService = NewMockNotificationService("generic", notification.NotificationServiceTypeGeneric)

		// Set up mobile push service
		mobilePushService := push.NewMobilePushService()

		// Set up webhook service with mock HTTP client
		mockHTTPClient = httpclient.Get().(*mock.MockHTTPClient)
		webhookService = notification.NewWebhookService("test", "https://api.test.com/webhook", "https://api.test.com/refresh",
			func(notification map[string]interface{}, userID string, endpointID string) (map[string]interface{}, error) {
				notification["uuid"] = userID + "#" + endpointID
				return notification, nil
			})

		// Configure mock response for webhook
		successResponse := `{"status":"success"}`
		err = mockHTTPClient.RegisterResponse("https://api.test.com/webhook", "POST", http.StatusOK, successResponse)
		Expect(err).To(BeNil())

		// Configure mock response for token refresh
		tokenRefreshResponse := `{
			"access_token": "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in": 3600,
			"token_type": "Bearer"
		}`
		err = mockHTTPClient.RegisterResponse("https://api.test.com/refresh", "POST", http.StatusOK, tokenRefreshResponse)
		Expect(err).To(BeNil())

		notification.Registry().Register(userSpecificService)
		notification.Registry().Register(genericService)
		notification.Registry().Register(webhookService)
		notification.Registry().Register(mobilePushService)

		// Register automation service for direct notification tests
		notification.Registry().Register(notification.NewAutomationService())

		// Add node to group
		test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

		// Create some node config
		testNode := node.NewNode(nodeID)
		nodeCtx := rmngctx.NewRmngContext(testNode)
		nodeConfigDB := node_details_db.NewNodeDetailsDB(nodeCtx)
		err = nodeConfigDB.UpdateServiceData("config", node_cfg_simple_light_test_data)
		Expect(err).To(BeNil())

		// Set up user's mobile device token
		userDB := user_integration_db.NewUserDB(rmngCtx)
		expiresAt := time.Now().Add(24 * time.Hour).Unix()
		err = userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "test", EndpointID: "test", IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "test-token", RefreshToken: "test-refresh-token", ExpiresAt: expiresAt, TokenType: "Bearer"}})
		Expect(err).To(BeNil())

		// Set up mock Alexa responses
		err = mockHTTPClient.RegisterResponse(alexaEUGateway, "POST", http.StatusOK, successResponse)
		Expect(err).To(BeNil())

		err = mockHTTPClient.RegisterResponse(alexa_skill.AlexaRefreshURI, "POST", http.StatusOK, tokenRefreshResponse)
		Expect(err).To(BeNil())

		ssmMock := awscommon.GetSSMClient()
		ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
			Name:  aws.String(alexa_skill.AlexaSSMClientIDParam),
			Value: aws.String("alexa-test-client-id"),
		})
		ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
			Name:  aws.String(alexa_skill.AlexaSSMClientSecretParam),
			Value: aws.String("alexa-test-client-secret"),
		})

	})

	Describe("Generic Notification Tests", func() {
		It("should handle user-specific notifications", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"user_specific": true,
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify notification was sent to users
			Expect(userSpecificService.SentNotifications).To(HaveLen(1))
			Expect(userSpecificService.SentNotifications[0].ShadowUpdateData.NodeID).To(Equal(nodeID))
			Expect(userSpecificService.SentToUsers[nodeID]).To(ContainElement(userID))
		})

		It("should handle generic notifications", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"generic": true,
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify notification was sent
			Expect(genericService.SentNotifications).To(HaveLen(1))
			Expect(genericService.SentNotifications[0].ShadowUpdateData.NodeID).To(Equal(nodeID))
			Expect(genericService.SentToUsers).To(BeEmpty())
		})

		It("should handle multiple notification services", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"user_specific": true,
					"generic":       true,
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify both services received notifications
			Expect(userSpecificService.SentNotifications).To(HaveLen(1))
			Expect(userSpecificService.SentNotifications[0].ShadowUpdateData.NodeID).To(Equal(nodeID))
			Expect(genericService.SentNotifications).To(HaveLen(1))
			Expect(genericService.SentNotifications[0].ShadowUpdateData.NodeID).To(Equal(nodeID))
			Expect(userSpecificService.SentToUsers[nodeID]).To(ContainElement(userID))
		})

		It("should handle invalid notification service", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"invalid_service": true,
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify no notifications were sent
			Expect(userSpecificService.SentNotifications).To(BeEmpty())
			Expect(genericService.SentNotifications).To(BeEmpty())
		})

		It("should handle node not in any group", func() {
			nodeIDWithoutGroup := "node-without-group"
			event := NotificationEvent{
				NodeID:           nodeIDWithoutGroup,
				TopicName:        "params-nonexistent-group",
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"user_specific": true,
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify no notifications were sent
			Expect(userSpecificService.SentNotifications).To(BeEmpty())
		})

		It("should handle Matter data model shadow update with nested endpoints, clusters, and attributes", func() {
			matterNodeID := "62356B4758474E74"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, matterNodeID)

			matterNode := node.NewNode(matterNodeID)
			matterNodeCtx := rmngctx.NewRmngContext(matterNode)
			nodeConfigDB := node_details_db.NewNodeDetailsDB(matterNodeCtx)

			matterConfig := map[string]interface{}{
				"data_model": "matter",
				"endpoints": map[string]interface{}{
					"0x1": map[string]interface{}{
						"c": map[string]interface{}{
							// "a" lists plain attributes only; "i"/"ts" are independent and
							// may overlap; "v" holds config-only values (never in state).
							"s": map[string]interface{}{
								"0x3": map[string]interface{}{
									"a":  []interface{}{"0xfffc", "0xfffd"},
									"i":  []interface{}{"0x0"},
									"ts": []interface{}{"0x0"},
								},
								"0x4": map[string]interface{}{
									"a":  []interface{}{"0xfffc", "0xfffd"},
									"i":  []interface{}{"0x0"},
									"ts": []interface{}{"0x0"},
								},
								"0x6": map[string]interface{}{
									"a":  []interface{}{"0x4001", "0x4002", "0x4003", "0xfffc", "0xfffd"},
									"i":  []interface{}{"0x0", "0x4000"},
									"ts": []interface{}{"0x0"},
								},
								"0x8": map[string]interface{}{
									"a":  []interface{}{"0x1", "0x2", "0x3", "0xf", "0x11", "0x12", "0x13", "0x14", "0x4000", "0xfffc", "0xfffd"},
									"i":  []interface{}{"0x0", "0x10"},
									"ts": []interface{}{"0x0", "0x10"},
								},
								"0x300": map[string]interface{}{
									"a":  []interface{}{"0x2", "0x3", "0x4", "0xf", "0x15", "0x16", "0x17", "0x19", "0x1a", "0x1b", "0x20", "0x21", "0x22", "0x24", "0x25", "0x26", "0xfffc", "0xfffd"},
									"i":  []interface{}{"0x0", "0x1", "0x7", "0x8", "0x11", "0x12", "0x13"},
									"ts": []interface{}{"0x0", "0x1", "0x7"},
								},
								// config-only cluster: values live in node config "v", never reported in params/state
								"0x1d": map[string]interface{}{
									"v": map[string]interface{}{"0x0": float64(7)},
								},
							},
						},
					},
				},
				"info": map[string]interface{}{
					"fw_version": "1.0.0",
					"name":       "smartlight-mtr-app",
					"type":       "smartlight-mtr-app",
				},
			}

			err := nodeConfigDB.UpdateServiceData("config", matterConfig)
			Expect(err).To(BeNil())

			event := NotificationEvent{
				NodeID:           matterNodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"0x1": map[string]interface{}{
							"c": map[string]interface{}{
								"s": map[string]interface{}{
									"0x300": map[string]interface{}{
										"a": map[string]interface{}{
											"0x0": 100,
										},
									},
								},
							},
						},
						"online": false,
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"0x1": map[string]interface{}{
							"c": map[string]interface{}{
								"s": map[string]interface{}{
									"0x300": map[string]interface{}{
										"a": map[string]interface{}{
											"0xfffd": 5,
											"0xfffc": 31,
											"0x4010": 65535,
											"0x400d": 0,
											"0x400c": 455,
											"0x400b": 142,
											"0x400a": 31,
											"0x4001": 1,
											"0x4000": 32804,
											"0x2a":   0,
											"0x29":   0,
											"0x28":   0,
											"0x26":   0,
											"0x25":   0,
											"0x24":   0,
											"0x22":   0,
											"0x21":   0,
											"0x20":   0,
											"0x1b":   0,
											"0x1a":   0,
											"0x19":   0,
											"0x17":   0,
											"0x16":   0,
											"0x15":   0,
											"0x13":   0,
											"0x12":   0,
											"0x11":   0,
											"0x10":   0,
											"0xf":    0,
											"0x8":    1,
											"0x7":    250,
											"0x4":    24701,
											"0x3":    24939,
											"0x2":    0,
											"0x1":    128,
											"0x0":    130,
										},
									},
									"0x8": map[string]interface{}{
										"a": map[string]interface{}{
											"0xfffd": 5,
											"0xfffc": 3,
											"0x4000": 255,
											"0x14":   50,
											"0x13":   0,
											"0x12":   0,
											"0x11":   255,
											"0x10":   0,
											"0xf":    0,
											"0x3":    254,
											"0x2":    1,
											"0x1":    0,
											"0x0":    45,
										},
									},
									"0x6": map[string]interface{}{
										"a": map[string]interface{}{
											"0xfffd": 6,
											"0xfffc": 7,
											"0x4003": 0,
											"0x4002": 0,
											"0x4001": 0,
											"0x4000": true,
											"0x0":    true,
										},
									},
									"0x4": map[string]interface{}{
										"a": map[string]interface{}{
											"0xfffd": 4,
											"0xfffc": 1,
											"0x0":    128,
										},
									},
									"0x3": map[string]interface{}{
										"a": map[string]interface{}{
											"0xfffd": 4,
											"0xfffc": 0,
											"0x0":    0,
										},
									},
								},
							},
						},
						"online": true,
					},
				},
				Notify: map[string]interface{}{
					"user_specific": true,
				},
			}

			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			Expect(userSpecificService.SentNotifications).To(HaveLen(1))
			sentNotif := userSpecificService.SentNotifications[0]
			Expect(sentNotif.ShadowUpdateData.NodeID).To(Equal(matterNodeID))
			Expect(sentNotif.TopicName).To(Equal(fmt.Sprintf("params-%s", groupID)))
			Expect(userSpecificService.SentToUsers[matterNodeID]).To(ContainElement(userID))

			currState := sentNotif.ShadowUpdateData.State
			Expect(currState.Params).To(HaveKey("0x1"))
			Expect(currState.Params).To(HaveKey("online"))
			Expect(currState.Params["online"]).To(Equal(true))

			endpoint1, ok := currState.Params["0x1"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(endpoint1).To(HaveKey("c"))

			clusters, ok := endpoint1["c"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(clusters).To(HaveKey("s"))

			serverClusters, ok := clusters["s"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(serverClusters).To(HaveKey("0x300"))
			Expect(serverClusters).To(HaveKey("0x8"))
			Expect(serverClusters).To(HaveKey("0x6"))
			Expect(serverClusters).To(HaveKey("0x4"))
			Expect(serverClusters).To(HaveKey("0x3"))
			// Config-only cluster 0x1d is declared in node config but excluded from reported params/state
			Expect(serverClusters).ToNot(HaveKey("0x1d"))

			cluster300, ok := serverClusters["0x300"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(cluster300).To(HaveKey("a"))

			attributes300, ok := cluster300["a"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(attributes300["0x0"]).To(Equal(130))
			Expect(attributes300["0x1"]).To(Equal(128))
			Expect(attributes300["0x4000"]).To(Equal(32804))
			Expect(attributes300["0x4001"]).To(Equal(1))

			cluster6, ok := serverClusters["0x6"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			attributes6, ok := cluster6["a"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(attributes6["0x0"]).To(Equal(true))
			Expect(attributes6["0x4000"]).To(Equal(true))

			cluster8, ok := serverClusters["0x8"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			attributes8, ok := cluster8["a"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(attributes8["0x0"]).To(Equal(45))
			Expect(attributes8["0x4000"]).To(Equal(255))
		})
	})

	Describe("Mobile Push Notifications", func() {
		It("should send notifications to both iOS and Android platforms for users with multiple device tokens", func() {
			// Setup additional user with both iOS and Android platforms
			additionalUserID := "user-with-both-platforms"
			additionalUser := user.NewUser(additionalUserID)
			additionalCtx := rmngctx.NewRmngContext(additionalUser)

			// Setup user entries for both platforms for original user
			originalUserDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID)))

			// Register iOS platform entry
			err := originalUserDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "APNS_ios_device-token", EndpointID: "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/ios-app/ios-device-token", SNSEndpointARN: "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/ios-app/ios-device-token"})
			Expect(err).To(BeNil())

			// Register Android platform entry
			err = originalUserDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "GCM_android_device-token", EndpointID: "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/android-app/android-device-token", SNSEndpointARN: "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/android-app/android-device-token"})
			Expect(err).To(BeNil())

			// Setup user entries for additional user
			additionalUserDB := user_integration_db.NewUserDB(additionalCtx)

			// Register iOS platform entry for additional user
			err = additionalUserDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "APNS_ios_additional-ios-token", EndpointID: "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/ios-app/additional-ios-token", SNSEndpointARN: "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/ios-app/additional-ios-token"})
			Expect(err).To(BeNil())

			// Register Android platform entry for additional user
			err = additionalUserDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "GCM_android_additional-android-token", EndpointID: "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/android-app/additional-android-token", SNSEndpointARN: "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/android-app/additional-android-token"})
			Expect(err).To(BeNil())

			// Share group with additional user
			_, err = group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, additionalUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			// Approve the sharing request
			sharingRequestDB := sharing_request_db.NewSharingRequestDB(additionalCtx)
			sharingRequests, err := sharingRequestDB.GetMySharingRequests()
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))

			err = group.ApproveSharingRequest(additionalCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			// Reset SNS mock to capture calls
			mockSNSClient := awscommon.GetSNSClient().(*mock.SNSMock)
			mockSNSClient.ClearPublishedMessages()

			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"push": true,
				},
			}

			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			// Define expected push messages
			type expectedPushMessage struct {
				TargetArn       string
				Platform        string
				DefaultMessage  string
				PlatformMessage map[string]interface{}
			}

			expectedMessages := []expectedPushMessage{
				{
					TargetArn:      "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/ios-app/ios-device-token",
					Platform:       "APNS",
					DefaultMessage: fmt.Sprintf("Node Alert: Node %s has an alert!", nodeID),
					PlatformMessage: map[string]interface{}{
						"aps": map[string]interface{}{
							"alert": map[string]interface{}{
								"title": "Node Alert",
								"body":  fmt.Sprintf("Node %s has an alert!", nodeID),
							},
							"sound":           "default",
							"category":        "node_alert",
							"mutable-content": float64(1),
							"thread-id":       fmt.Sprintf("%s.node.alert", nodeID),
						},
						"event_data": map[string]interface{}{
							"data": map[string]interface{}{
								"nodeID": nodeID,
							},
							"ts":   int(0),
							"type": "node_alert",
						},
					},
				},
				{
					TargetArn:      "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/android-app/android-device-token",
					Platform:       "GCM",
					DefaultMessage: fmt.Sprintf("Node Alert: Node %s has an alert!", nodeID),
					PlatformMessage: map[string]interface{}{
						"data": map[string]interface{}{
							"title": "Node Alert",
							"body":  fmt.Sprintf("Node %s has an alert!", nodeID),
							"event_data": map[string]interface{}{
								"data": map[string]interface{}{
									"nodeID": nodeID,
								},
								"ts":           int(0),
								"type":         "node_alert",
								"notif_grp_id": fmt.Sprintf("%s.node.alert", nodeID),
							},
						},
						"android": map[string]interface{}{
							"priority": "high",
						},
					},
				},
				{
					TargetArn:      "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/ios-app/additional-ios-token",
					Platform:       "APNS",
					DefaultMessage: fmt.Sprintf("Node Alert: Node %s has an alert!", nodeID),
					PlatformMessage: map[string]interface{}{
						"aps": map[string]interface{}{
							"alert": map[string]interface{}{
								"title": "Node Alert",
								"body":  fmt.Sprintf("Node %s has an alert!", nodeID),
							},
							"sound":           "default",
							"category":        "node_alert",
							"mutable-content": float64(1),
							"thread-id":       fmt.Sprintf("%s.node.alert", nodeID),
						},
						"event_data": map[string]interface{}{
							"data": map[string]interface{}{
								"nodeID": nodeID,
							},
							"ts":   int(0),
							"type": "node_alert",
						},
					},
				},
				{
					TargetArn:      "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/android-app/additional-android-token",
					Platform:       "GCM",
					DefaultMessage: fmt.Sprintf("Node Alert: Node %s has an alert!", nodeID),
					PlatformMessage: map[string]interface{}{
						"data": map[string]interface{}{
							"title": "Node Alert",
							"body":  fmt.Sprintf("Node %s has an alert!", nodeID),
							"event_data": map[string]interface{}{
								"data": map[string]interface{}{
									"nodeID": nodeID,
								},
								"ts":           int(0),
								"type":         "node_alert",
								"notif_grp_id": fmt.Sprintf("%s.node.alert", nodeID),
							},
						},
						"android": map[string]interface{}{
							"priority": "high",
						},
					},
				},
			}

			// Verify SNS Publish was called for all 4 platforms (2 users × 2 platforms each)
			publishCalls := mockSNSClient.GetPublishedMessages()
			Expect(publishCalls).To(HaveLen(len(expectedMessages)))

			// Create a map of actual calls by target ARN for easier comparison
			actualCallsByARN := make(map[string]*sns.PublishInput)
			for _, call := range publishCalls {
				actualCallsByARN[*call.TargetArn] = call
			}

			// Verify each expected message
			for _, expected := range expectedMessages {
				actualCall, exists := actualCallsByARN[expected.TargetArn]
				Expect(exists).To(BeTrue(), "Expected call to ARN %s not found", expected.TargetArn)

				// Verify message structure
				Expect(actualCall.MessageStructure).To(Equal(aws.String("json")))

				// Parse the actual message
				var actualMessageMap map[string]string
				err := json.Unmarshal([]byte(*actualCall.Message), &actualMessageMap)
				Expect(err).To(BeNil())

				// Verify DefaultMessage
				Expect(actualMessageMap).To(HaveKey("default"))
				Expect(actualMessageMap["default"]).To(Equal(expected.DefaultMessage))

				// Verify Platform
				Expect(actualMessageMap).To(HaveKey(expected.Platform))

				// Verify PlatformMessage
				var actualPlatformMessage map[string]interface{}
				err = json.Unmarshal([]byte(actualMessageMap[expected.Platform]), &actualPlatformMessage)
				Expect(err).To(BeNil())
				resetPushNotificationTimestamp(actualPlatformMessage)
				Expect(actualPlatformMessage).To(Equal(expected.PlatformMessage))
			}
		})

		It("should publish a push for a direct notification", func() {
			// Regression: direct notifications leave ShadowUpdateData nil; the push service used to deref it and panic.
			userDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID)))
			iosARN := "arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/ios-app/direct-push-token"
			err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "APNS_ios_direct-push-token", EndpointID: iosARN, SNSEndpointARN: iosARN})
			Expect(err).To(BeNil())

			mockSNSClient := awscommon.GetSNSClient().(*mock.SNSMock)
			mockSNSClient.ClearPublishedMessages()

			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        groupID,
				NotificationType: "direct_notification",
				Notify: map[string]interface{}{
					"push": true,
				},
			}

			Expect(Handler(ctx, event)).To(BeNil())

			publishCalls := mockSNSClient.GetPublishedMessages()
			Expect(publishCalls).To(HaveLen(1))
			Expect(*publishCalls[0].TargetArn).To(Equal(iosARN))

			var messageMap map[string]string
			Expect(json.Unmarshal([]byte(*publishCalls[0].Message), &messageMap)).To(BeNil())
			Expect(messageMap["default"]).To(Equal(fmt.Sprintf("Node Alert: Node %s has an alert!", nodeID)))

			var apns map[string]interface{}
			Expect(json.Unmarshal([]byte(messageMap["APNS"]), &apns)).To(BeNil())
			eventData := apns["event_data"].(map[string]interface{})
			Expect(eventData["data"].(map[string]interface{})["nodeID"]).To(Equal(nodeID))
		})
	})

	Describe("Webhook Notifications", func() {
		It("should handle webhook notifications", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"webhook_test": true,
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify webhook request was made
			Expect(mockHTTPClient.Requests).To(HaveLen(1))
			req := mockHTTPClient.Requests[0]

			// Verify request URL
			Expect(req.URL.String()).To(Equal("https://api.test.com/webhook"))

			// Verify request method
			Expect(req.Method).To(Equal("POST"))

			// Verify authorization header
			Expect(req.Header.Get("Authorization")).To(Equal("Bearer test-token"))

			// Verify request body
			body, err := io.ReadAll(req.Body)
			Expect(err).To(BeNil())
			Expect(string(body)).To(Equal(`{"node_id":"test-node-id","notification_type":"shadow_update","state":{"params":{"status":"online"}},"topic_name":"params-` + groupID + `","uuid":"test-user-id#test"}`))
		})

		It("should handle webhook notifications with multiple users", func() {
			// Create additional test user
			additionalUserID := "test-user-id-2"
			additionalUser := user.NewUser(additionalUserID)
			additionalCtx := rmngctx.NewRmngContext(additionalUser)

			// Set up additional user's mobile device token
			additionalUserDB := user_integration_db.NewUserDB(additionalCtx)
			expiresAt := time.Now().Add(24 * time.Hour).Unix()
			err := additionalUserDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "test", EndpointID: "test", IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "test-token-2", RefreshToken: "test-refresh-token-2", ExpiresAt: expiresAt, TokenType: "Bearer"}})
			Expect(err).To(BeNil())

			// Share group with additional user
			_, err = group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, additionalUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			// Approve the sharing request
			sharingRequestDB := sharing_request_db.NewSharingRequestDB(additionalCtx)
			sharingRequests, err := sharingRequestDB.GetMySharingRequests()
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))

			err = group.ApproveSharingRequest(additionalCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"webhook_test": true,
				},
			}

			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify webhook requests were made for both users
			Expect(mockHTTPClient.Requests).To(HaveLen(2))

			// Verify requests for both users
			tokens := []string{"Bearer test-token", "Bearer test-token-2"}
			expectedBody := []string{
				`{"node_id":"test-node-id","notification_type":"shadow_update","state":{"params":{"status":"online"}},"topic_name":"params-` + groupID + `","uuid":"test-user-id#test"}`,
				`{"node_id":"test-node-id","notification_type":"shadow_update","state":{"params":{"status":"online"}},"topic_name":"params-` + groupID + `","uuid":"test-user-id-2#test"}`,
			}
			for _, req := range mockHTTPClient.Requests {
				Expect(req.URL.String()).To(Equal("https://api.test.com/webhook"))
				Expect(req.Method).To(Equal("POST"))
				Expect(req.Header.Get("Authorization")).To(BeElementOf(tokens))

				body, err := io.ReadAll(req.Body)
				Expect(err).To(BeNil())
				Expect(string(body)).To(BeElementOf(expectedBody))
			}
		})

		It("should handle webhook notifications with missing user tokens", func() {
			// Create user without mobile device token
			userWithoutToken := user.NewUser("user-without-token")
			userWithoutTokenCtx := rmngctx.NewRmngContext(userWithoutToken)

			// Share group with user without token
			_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, "user-without-token", utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			// Approve the sharing request
			sharingRequestDB := sharing_request_db.NewSharingRequestDB(userWithoutTokenCtx)
			sharingRequests, err := sharingRequestDB.GetMySharingRequests()
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))

			err = group.ApproveSharingRequest(userWithoutTokenCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"webhook_test": true,
				},
			}

			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify webhook request was only made for user with token
			Expect(mockHTTPClient.Requests).To(HaveLen(1))
			req := mockHTTPClient.Requests[0]

			// Verify request was made with the correct token
			Expect(req.Header.Get("Authorization")).To(Equal("Bearer test-token"))
		})

		It("should handle webhook notifications with invalid platform", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"webhook_invalid": true,
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify no webhook requests were made
			Expect(mockHTTPClient.Requests).To(BeEmpty())
		})

		It("should handle webhook notifications with token refresh", func() {
			// Create test user and context
			testUser := user.NewUser(userID)
			rmngCtx := rmngctx.NewRmngContext(testUser)

			// Set up user with expired token
			userDB := user_integration_db.NewUserDB(rmngCtx)
			expiresAt := time.Now().Add(-1 * time.Hour).Unix() // Expired 1 hour ago
			err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "test", EndpointID: "test", IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "expired-token", RefreshToken: "valid-refresh-token", ExpiresAt: expiresAt, TokenType: "Bearer"}})
			Expect(err).To(BeNil())

			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"webhook_test": true,
				},
			}

			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify token refresh request was made
			Expect(mockHTTPClient.Requests).To(HaveLen(2)) // One for refresh, one for webhook

			// Verify refresh token request
			refreshReq := mockHTTPClient.Requests[0]
			Expect(refreshReq.URL.String()).To(Equal("https://api.test.com/refresh"))
			Expect(refreshReq.Method).To(Equal("POST"))
			Expect(refreshReq.Header.Get("Content-Type")).To(Equal("application/json"))

			// Verify webhook request was made with new token
			webhookReq := mockHTTPClient.Requests[1]
			Expect(webhookReq.URL.String()).To(Equal("https://api.test.com/webhook"))
			Expect(webhookReq.Header.Get("Authorization")).To(Equal("Bearer new-access-token"))

			// Verify token was updated in database
			userEntry, err := userDB.GetUserEntry()
			Expect(err).To(BeNil())
			Expect(userEntry.IntegrationToken.AccessToken).To(Equal("new-access-token"))
		})

		It("should handle webhook notifications with failed token refresh", func() {
			// Create test user and context
			testUser := user.NewUser(userID)
			rmngCtx := rmngctx.NewRmngContext(testUser)

			// Set up user with expired token
			userDB := user_integration_db.NewUserDB(rmngCtx)
			expiresAt := time.Now().Add(-1 * time.Hour).Unix() // Expired 1 hour ago
			err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: "test", EndpointID: "test", IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "expired-token", RefreshToken: "invalid-refresh-token", ExpiresAt: expiresAt, TokenType: "Bearer"}})
			Expect(err).To(BeNil())

			// Configure mock response for failed token refresh
			failedRefreshResponse := `{"error": "invalid_refresh_token"}`
			err = mockHTTPClient.RegisterResponse("https://api.test.com/refresh", "POST", http.StatusUnauthorized, failedRefreshResponse)
			Expect(err).To(BeNil())

			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        fmt.Sprintf("params-%s", groupID),
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "offline",
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"status": "online",
					},
				},
				Notify: map[string]interface{}{
					"webhook_test": true,
				},
			}

			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify only refresh token request was made (no webhook request due to failed refresh)
			Expect(mockHTTPClient.Requests).To(HaveLen(1))
			refreshReq := mockHTTPClient.Requests[0]
			Expect(refreshReq.URL.String()).To(Equal("https://api.test.com/refresh"))
		})
	})

	Describe("Alexa Notifications", func() {
		// Add helper functions just before the first Alexa test
		// Helper function to setup Alexa test environment and register user token
		setupAlexaTest := func() string {
			// Register Alexa token for user
			userDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID)))
			expiresAt := time.Now().Add(-1 * time.Hour).Unix() // Expired 1 hour ago
			err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: alexa_skill.AlexaPlatform, EndpointID: alexa_skill.AlexaPlatform, IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "alexa-test-token", RefreshToken: "alexa-test-refresh", ExpiresAt: expiresAt, TokenType: "Bearer", Region: "eu-west-1"}})
			Expect(err).To(BeNil())

			// Register Alexa notification service and reset HTTP requests
			alexaNotification := alexa_skill.NewAlexaNotification(ctx, "")
			notification.Registry().Register(alexaNotification)
			mockHTTPClient.Requests = []*http.Request{}

			return "new-access-token"
		}

		// Helper function to create a notification event for a device state
		createAlexaNotificationEvent := func(prevDeviceState, currDeviceState map[string]interface{}) NotificationEvent {
			return NotificationEvent{
				NodeID:           nodeID,
				TopicName:        "params-" + groupID,
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"Light": prevDeviceState,
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Online: aws.Bool(true),
					Params: map[string]interface{}{
						"Light": currDeviceState,
					},
				},
				Notify: map[string]interface{}{
					alexa_skill.AlexaPlatform: true,
				},
			}
		}

		// Helper function to get the Alexa request body from mock requests
		getAlexaRequestBody := func() map[string]interface{} {
			Expect(mockHTTPClient.Requests).To(HaveLen(2), "Should have exactly 2 requests")

			req := mockHTTPClient.Requests[0]
			Expect(req.URL.String()).To(Equal(alexa_skill.AlexaRefreshURI), "Request should be sent to Alexa Refresh URI")
			Expect(req.Method).To(Equal("POST"))
			Expect(req.Header.Get("Content-Type")).To(Equal("application/x-www-form-urlencoded;charset=UTF-8"))
			body, err := io.ReadAll(req.Body)
			Expect(err).To(BeNil())
			Expect(string(body)).To(ContainSubstring("grant_type=refresh_token"))
			Expect(string(body)).To(ContainSubstring("refresh_token=alexa-test-refresh"))
			Expect(string(body)).To(ContainSubstring("client_id=alexa-test-client-id"))
			Expect(string(body)).To(ContainSubstring("client_secret=alexa-test-client-secret"))

			req = mockHTTPClient.Requests[1]
			Expect(req.URL.String()).To(Equal(alexaEUGateway), "Request should be sent to Alexa URI")

			// Verify request headers
			Expect(req.Method).To(Equal("POST"))
			Expect(req.Header.Get("Content-Type")).To(Equal("application/json"))

			// Parse the request body
			body, err = io.ReadAll(req.Body)
			Expect(err).To(BeNil())

			var requestBody map[string]interface{}
			err = json.Unmarshal(body, &requestBody)
			Expect(err).To(BeNil())

			return requestBody
		}

		It("should format Alexa notifications correctly with state properties", func() {
			// Setup
			token := setupAlexaTest()

			// Create test event with power and brightness state
			event := createAlexaNotificationEvent(map[string]interface{}{
				"power":      false,
				"brightness": 100,
			}, map[string]interface{}{
				"power":      true,
				"brightness": 80,
			})

			// Start profiling before execution
			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.ProfileReset()

			// Execute - Process the notification
			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Capture profile data
			profile := dbMock.ProfileGet()
			readCount, writeCount := profile.TotalCounts()
			Expect(readCount).To(BeEquivalentTo(6)) // Multi-endpoint: Alexa send path does 1 Query (list endpoints for integration) + 1 GetItem per endpoint (token refresh read-modify-write), so 2 reads here vs the previous single Query.
			Expect(writeCount).To(BeEquivalentTo(1))
			profiles["Alexa Notification (Device Param Change)"] = &profile

			// Get request body
			changeReport := getAlexaRequestBody()

			// Verify headers
			req := mockHTTPClient.Requests[1]
			Expect(req.Header.Get("Authorization")).To(Equal("Bearer " + token))

			// Normalize message ID for consistent testing
			changeReport["event"].(map[string]interface{})["header"].(map[string]interface{})["messageId"] = "test_overwritten"

			// Define expected response structure
			var expectedPayload interface{} = alexa_skill.ChangeReportPayload{
				Change: alexa_skill.ChangeReportChange{
					Cause: alexa_skill.ChangeReportCause{
						Type: "PHYSICAL_INTERACTION",
					},
					Properties: []alexa_skill.ContextProperty{
						{
							NameSpace:                 "Alexa.PowerController",
							Name:                      "powerState",
							Value:                     "ON",
							TimeOfSample:              time.Now().Format(time.RFC3339),
							UncertaintyInMilliseconds: 0,
						},
						{
							NameSpace:                 "Alexa.BrightnessController",
							Name:                      "brightness",
							Value:                     80,
							TimeOfSample:              time.Now().Format(time.RFC3339),
							UncertaintyInMilliseconds: 0,
						},
					},
				},
			}

			expectedChangeReport := alexa_skill.AlexaResponse{
				Event: alexa_skill.Event{
					Header: alexa_skill.Header{
						Namespace:      "Alexa",
						Name:           "ChangeReport",
						MessageID:      "test_overwritten",
						PayloadVersion: "3",
					},
					Endpoint: &alexa_skill.Endpoint{
						EndpointID: fmt.Sprintf("%s#%s", nodeID, "Light"),
						Scope: &alexa_skill.Scope{
							Type:  "BearerToken",
							Token: token,
						},
					},
					Payload: &expectedPayload,
				},
				Context: &alexa_skill.Context{
					Properties: []alexa_skill.ContextProperty{
						{
							NameSpace: "Alexa.EndpointHealth",
							Name:      "connectivity",
							Value: struct {
								Value string `json:"value"`
							}{Value: "OK"},
							TimeOfSample:              time.Now().Format(time.RFC3339),
							UncertaintyInMilliseconds: 0,
						},
					},
				},
			}
			test_utils.AssertNormalizedEqual(changeReport, expectedChangeReport)
		})

		It("should send multiple changeReport notifications for multiple devices", func() {
			// Setup
			token := setupAlexaTest()

			// Update the node config to include two devices
			testNode := node.NewNode(nodeID)
			nodeCtx := rmngctx.NewRmngContext(testNode)
			nodeConfigDB := node_details_db.NewNodeDetailsDB(nodeCtx)

			// Node config with two devices: Light and Switch
			multiDeviceNodeConfig := map[string]interface{}{
				"devices": []map[string]interface{}{
					{
						"id":      "Light",
						"type":    "esp.device.lightbulb",
						"primary": "power",
						"params": []map[string]interface{}{
							{
								"id":        "power",
								"data_type": "bool",
								"ui_type":   "esp.ui.toggle",
								"type":      "esp.param.power",
							},
							{
								"id":        "brightness",
								"data_type": "int",
								"ui_type":   "esp.ui.slider",
								"type":      "esp.param.brightness",
								"bounds":    map[string]interface{}{"min": 0, "max": 100},
							},
						},
					},
					{
						"id":      "Switch",
						"type":    "esp.device.switch",
						"primary": "power",
						"params": []map[string]interface{}{
							{
								"id":        "power",
								"data_type": "bool",
								"ui_type":   "esp.ui.toggle",
								"type":      "esp.param.power",
							},
						},
					},
				},
				"info": map[string]interface{}{
					"fw_version": "1.0",
				},
			}

			err := nodeConfigDB.UpdateServiceData("config", multiDeviceNodeConfig)
			Expect(err).To(BeNil())

			// Create test event with changes to both devices
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        "params-" + groupID,
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"Light": map[string]interface{}{
							"power":      false,
							"brightness": 50,
						},
						"Switch": map[string]interface{}{
							"power": false,
						},
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Online: aws.Bool(true),
					Params: map[string]interface{}{
						"Light": map[string]interface{}{
							"power":      true,
							"brightness": 80,
						},
						"Switch": map[string]interface{}{
							"power": true,
						},
					},
				},
				Notify: map[string]interface{}{
					alexa_skill.AlexaPlatform: true,
				},
			}

			// Reset HTTP client requests
			mockHTTPClient.Requests = []*http.Request{}

			// Execute - Process the notification
			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify we have one token refresh request and two notification requests (one for each device)
			Expect(mockHTTPClient.Requests).To(HaveLen(3), "Should have one token refresh and two notification requests")

			refreshReq := mockHTTPClient.Requests[0]
			Expect(refreshReq.URL.String()).To(Equal(alexa_skill.AlexaRefreshURI), "First request should be token refresh")

			// Verify both notification requests
			firstNotifyReq := mockHTTPClient.Requests[1]
			secondNotifyReq := mockHTTPClient.Requests[2]

			Expect(firstNotifyReq.URL.String()).To(Equal(alexaEUGateway), "Second request should be Alexa notification")
			Expect(secondNotifyReq.URL.String()).To(Equal(alexaEUGateway), "Third request should be Alexa notification")

			// Parse both notification bodies
			firstNotifyBody, err := io.ReadAll(firstNotifyReq.Body)
			Expect(err).To(BeNil())

			secondNotifyBody, err := io.ReadAll(secondNotifyReq.Body)
			Expect(err).To(BeNil())

			// Unmarshal first notification
			var firstChangeReport alexa_skill.AlexaResponse
			err = json.Unmarshal(firstNotifyBody, &firstChangeReport)
			Expect(err).To(BeNil())

			// Unmarshal second notification
			var secondChangeReport alexa_skill.AlexaResponse
			err = json.Unmarshal(secondNotifyBody, &secondChangeReport)
			Expect(err).To(BeNil())

			// Sort notifications by EndpointID to ensure consistent testing
			notifications := []alexa_skill.AlexaResponse{firstChangeReport, secondChangeReport}
			sort.Slice(notifications, func(i, j int) bool {
				return notifications[i].Event.Endpoint.EndpointID < notifications[j].Event.Endpoint.EndpointID
			})

			rlog.Info(ctx).Interface("notifications", notifications).Send()

			// Define expected notification structure
			expectedNotifications := []map[string]interface{}{
				{
					"context": map[string]interface{}{
						"properties": []map[string]interface{}{
							{
								"name":                      "connectivity",
								"namespace":                 "Alexa.EndpointHealth",
								"timeOfSample":              notifications[0].Context.Properties[0].TimeOfSample, // Use actual timestamp for comparison
								"uncertaintyInMilliseconds": float64(0),
								"value": map[string]interface{}{
									"value": "OK",
								},
							},
						},
					},
					"event": map[string]interface{}{
						"endpoint": map[string]interface{}{
							"endpointId": fmt.Sprintf("%s#%s", nodeID, "Light"),
							"scope": map[string]interface{}{
								"token": token,
								"type":  "BearerToken",
							},
						},
						"header": map[string]interface{}{
							"messageId":      notifications[0].Event.Header.MessageID, // Use actual message ID for comparison
							"name":           "ChangeReport",
							"namespace":      "Alexa",
							"payloadVersion": "3",
						},
						"payload": map[string]interface{}{
							"change": map[string]interface{}{
								"cause": map[string]interface{}{
									"type": "PHYSICAL_INTERACTION",
								},
								"properties": []map[string]interface{}{
									{
										"name":                      "powerState",
										"namespace":                 "Alexa.PowerController",
										"timeOfSample":              notifications[0].Context.Properties[0].TimeOfSample, // Use actual timestamp for comparison
										"uncertaintyInMilliseconds": float64(0),
										"value":                     "ON",
									},
									{
										"name":                      "brightness",
										"namespace":                 "Alexa.BrightnessController",
										"timeOfSample":              notifications[0].Context.Properties[0].TimeOfSample, // Use actual timestamp for comparison
										"uncertaintyInMilliseconds": float64(0),
										"value":                     float64(80), // JSON numbers are parsed as float64
									},
								},
							},
						},
					},
				},
				{
					"context": map[string]interface{}{
						"properties": []map[string]interface{}{
							{
								"name":                      "connectivity",
								"namespace":                 "Alexa.EndpointHealth",
								"timeOfSample":              notifications[1].Context.Properties[0].TimeOfSample, // Use actual timestamp for comparison
								"uncertaintyInMilliseconds": float64(0),
								"value": map[string]interface{}{
									"value": "OK",
								},
							},
						},
					},
					"event": map[string]interface{}{
						"endpoint": map[string]interface{}{
							"endpointId": fmt.Sprintf("%s#%s", nodeID, "Switch"),
							"scope": map[string]interface{}{
								"token": token,
								"type":  "BearerToken",
							},
						},
						"header": map[string]interface{}{
							"messageId":      notifications[1].Event.Header.MessageID, // Use actual message ID for comparison
							"name":           "ChangeReport",
							"namespace":      "Alexa",
							"payloadVersion": "3",
						},
						"payload": map[string]interface{}{
							"change": map[string]interface{}{
								"cause": map[string]interface{}{
									"type": "PHYSICAL_INTERACTION",
								},
								"properties": []map[string]interface{}{
									{
										"name":                      "powerState",
										"namespace":                 "Alexa.PowerController",
										"timeOfSample":              notifications[1].Context.Properties[0].TimeOfSample, // Use actual timestamp for comparison
										"uncertaintyInMilliseconds": float64(0),
										"value":                     "ON",
									},
								},
							},
						},
					},
				},
			}

			// Convert notifications to map for comparison
			var actualNotifications []map[string]interface{}
			notificationsJSON, err := json.Marshal(notifications)
			Expect(err).To(BeNil())
			err = json.Unmarshal(notificationsJSON, &actualNotifications)
			Expect(err).To(BeNil())

			// Compare actual and expected notifications
			test_utils.AssertNormalizedEqual(actualNotifications, expectedNotifications)
		})

		It("should not duplicate properties between Change.Properties and Context.Properties", func() {
			// Setup
			token := setupAlexaTest()

			// Create test event with power and brightness state
			event := createAlexaNotificationEvent(map[string]interface{}{
				"power":      false,
				"brightness": 100,
			}, map[string]interface{}{
				"power":      true,
				"brightness": 100,
			})

			// Execute - Process the notification
			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Get response and extract properties
			changeReport := getAlexaRequestBody()

			// Verify headers
			req := mockHTTPClient.Requests[1]
			Expect(req.Header.Get("Authorization")).To(Equal("Bearer " + token))

			// Normalize message ID for consistent testing
			changeReport["event"].(map[string]interface{})["header"].(map[string]interface{})["messageId"] = "test_overwritten"

			// Define expected response structure
			var expectedPayload interface{} = alexa_skill.ChangeReportPayload{
				Change: alexa_skill.ChangeReportChange{
					Cause: alexa_skill.ChangeReportCause{
						Type: "PHYSICAL_INTERACTION",
					},
					Properties: []alexa_skill.ContextProperty{
						{
							NameSpace:                 "Alexa.PowerController",
							Name:                      "powerState",
							Value:                     "ON",
							TimeOfSample:              time.Now().Format(time.RFC3339),
							UncertaintyInMilliseconds: 0,
						},
					},
				},
			}

			expectedChangeReport := alexa_skill.AlexaResponse{
				Event: alexa_skill.Event{
					Header: alexa_skill.Header{
						Namespace:      "Alexa",
						Name:           "ChangeReport",
						MessageID:      "test_overwritten",
						PayloadVersion: "3",
					},
					Endpoint: &alexa_skill.Endpoint{
						EndpointID: fmt.Sprintf("%s#%s", nodeID, "Light"),
						Scope: &alexa_skill.Scope{
							Type:  "BearerToken",
							Token: token,
						},
					},
					Payload: &expectedPayload,
				},
				Context: &alexa_skill.Context{
					Properties: []alexa_skill.ContextProperty{
						{
							NameSpace: "Alexa.EndpointHealth",
							Name:      "connectivity",
							Value: struct {
								Value string `json:"value"`
							}{Value: "OK"},
							TimeOfSample:              time.Now().Format(time.RFC3339),
							UncertaintyInMilliseconds: 0,
						},
						{
							NameSpace:                 "Alexa.BrightnessController",
							Name:                      "brightness",
							Value:                     100,
							TimeOfSample:              time.Now().Format(time.RFC3339),
							UncertaintyInMilliseconds: 0,
						},
					},
				},
			}
			test_utils.AssertNormalizedEqual(changeReport, expectedChangeReport)
		})

		It("should handle direct notifications", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        groupID,
				NotificationType: "direct_notification",
				Notify: map[string]interface{}{
					"user_specific": true,
					"generic":       true,
					"automation": map[string]interface{}{
						"id": []interface{}{"automation-1", "automation-2"},
					},
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify both services received direct notifications
			Expect(userSpecificService.SentNotifications).To(HaveLen(1))
			Expect(userSpecificService.SentNotifications[0].NotificationType).To(Equal(notification.NotificationTypeDirect))
			Expect(userSpecificService.SentNotifications[0].DirectNotificationData.NodeID).To(Equal(nodeID))
			Expect(userSpecificService.SentNotifications[0].DirectNotificationData.NotifyData).To(Equal(event.Notify))

			Expect(genericService.SentNotifications).To(HaveLen(1))
			Expect(genericService.SentNotifications[0].NotificationType).To(Equal(notification.NotificationTypeDirect))
			Expect(genericService.SentNotifications[0].DirectNotificationData.NodeID).To(Equal(nodeID))

			// Verify user-specific service was called with correct users
			Expect(userSpecificService.SentToUsers[nodeID]).To(ContainElement(userID))
		})

		It("should handle webhook direct notifications", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        groupID,
				NotificationType: "direct_notification",
				Notify: map[string]interface{}{
					"webhook_test": map[string]interface{}{
						"test_field":  true,
						"custom_data": "test_value",
					},
				},
			}

			err := Handler(ctx, event)
			Expect(err).To(BeNil())

			// Verify webhook request was made
			Expect(mockHTTPClient.Requests).To(HaveLen(1))
			req := mockHTTPClient.Requests[0]

			// Verify request URL
			Expect(req.URL.String()).To(Equal("https://api.test.com/webhook"))

			// Verify request method
			Expect(req.Method).To(Equal("POST"))

			// Verify authorization header
			Expect(req.Header.Get("Authorization")).To(Equal("Bearer test-token"))

			// Verify request body contains direct notification data
			body, err := io.ReadAll(req.Body)
			Expect(err).To(BeNil())

			var bodyData map[string]interface{}
			err = json.Unmarshal(body, &bodyData)
			Expect(err).To(BeNil())

			Expect(bodyData["notification_type"]).To(Equal("direct"))
			Expect(bodyData["node_id"]).To(Equal(nodeID))
			Expect(bodyData["notify_data"]).To(HaveKey("webhook_test"))
			notifyData := bodyData["notify_data"].(map[string]interface{})
			webhookData := notifyData["webhook_test"].(map[string]interface{})
			Expect(webhookData).To(HaveKey("test_field"))
			Expect(webhookData).To(HaveKey("custom_data"))
		})

		It("should fan out to every linked Amazon account when the user has multiple Alexa endpoints", func() {
			expiresAt := time.Now().Add(-1 * time.Hour).Unix()
			userDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID)))
			err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: alexa_skill.AlexaPlatform, EndpointID: "amazon-user-A", IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "token-A", RefreshToken: "refresh-A", ExpiresAt: expiresAt, TokenType: "Bearer", Region: "eu-west-1"}})
			Expect(err).To(BeNil())
			err = userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: alexa_skill.AlexaPlatform, EndpointID: "amazon-user-B", IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "token-B", RefreshToken: "refresh-B", ExpiresAt: expiresAt, TokenType: "Bearer", Region: "eu-west-1"}})
			Expect(err).To(BeNil())

			alexaNotification := alexa_skill.NewAlexaNotification(ctx, "")
			notification.Registry().Register(alexaNotification)
			mockHTTPClient.Requests = []*http.Request{}

			event := createAlexaNotificationEvent(
				map[string]interface{}{"power": false},
				map[string]interface{}{"power": true},
			)

			err = Handler(ctx, event)
			Expect(err).To(BeNil())

			// Two endpoints → two refresh POSTs + two ChangeReport POSTs = 4 requests.
			Expect(mockHTTPClient.Requests).To(HaveLen(4))

			refreshTokensSeen := map[string]bool{}
			changeReportAuthsSeen := map[string]bool{}
			for _, req := range mockHTTPClient.Requests {
				if req.URL.String() == alexa_skill.AlexaRefreshURI {
					body, err := io.ReadAll(req.Body)
					Expect(err).To(BeNil())
					if strings.Contains(string(body), "refresh_token=refresh-A") {
						refreshTokensSeen["A"] = true
					}
					if strings.Contains(string(body), "refresh_token=refresh-B") {
						refreshTokensSeen["B"] = true
					}
				} else if req.URL.String() == alexaEUGateway {
					changeReportAuthsSeen[req.Header.Get("Authorization")] = true
				}
			}
			Expect(refreshTokensSeen).To(HaveLen(2), "both refresh tokens should have been used")
			Expect(changeReportAuthsSeen).To(HaveLen(1), "both endpoints get the same refreshed bearer because the mock returns one canned response")
		})

	})

	Describe("GVA Notifications", func() {
		// Helper function to setup GVA test environment and register user token
		setupGVATest := func() {
			// Register GVA token for user
			userDB := user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID)))
			expiresAt := time.Now().Add(24 * time.Hour).Unix()
			err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{IntegrationID: gva.GVAPlatform, EndpointID: gva.GVAPlatform, IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: "gva-test-token", RefreshToken: "gva-test-refresh", ExpiresAt: expiresAt, TokenType: "Bearer"}})
			Expect(err).To(BeNil())

			// Register GVA notification service in mock mode and reset HTTP requests
			gvaNotification := gva.NewGVANotification(ctx, "https://webhook-mock.test")
			notification.Registry().Register(gvaNotification)

			// Register mock response for GVA mock endpoint
			successResponse := `{"requestId":"test","payload":{}}`
			err = mockHTTPClient.RegisterResponse("https://webhook-mock.test/v1/gva/data", "POST", http.StatusOK, successResponse)
			Expect(err).To(BeNil())
			err = mockHTTPClient.RegisterResponse("https://webhook-mock.test/v1/gva/token", "POST", http.StatusOK, `{"access_token":"mock-gva-token"}`)
			Expect(err).To(BeNil())

			// Setup SSM mock parameter for GVA service account JSON
			ssmMock := awscommon.GetSSMClient()
			ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
				Name:  aws.String(gva.GVASSMServiceAccountJSONParam),
				Value: aws.String(`{"type":"service_account","project_id":"test-project-id","private_key_id":"test-key-id","private_key":"-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n","client_email":"test@test.iam.gserviceaccount.com","client_id":"123456789","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","auth_provider_x509_cert_url":"https://www.googleapis.com/oauth2/v1/certs","client_x509_cert_url":"https://www.googleapis.com/robot/v1/metadata/x509/test","universe_domain":"googleapis.com"}`),
			})

			mockHTTPClient.Requests = []*http.Request{}
		}

		// Helper function to create a GVA notification event for a device state
		createGVANotificationEvent := func(prevDeviceState, currDeviceState map[string]interface{}) NotificationEvent {
			return NotificationEvent{
				NodeID:           nodeID,
				TopicName:        "params-" + groupID,
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"Light": prevDeviceState,
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"Light": currDeviceState,
					},
				},
				Notify: map[string]interface{}{
					gva.GVAPlatform: true,
				},
			}
		}

		It("should marshal GVA report state for multiple devices", func() {
			setupGVATest()

			// Update node config to include two devices
			testNode := node.NewNode(nodeID)
			nodeCtx := rmngctx.NewRmngContext(testNode)
			nodeConfigDB := node_details_db.NewNodeDetailsDB(nodeCtx)

			multiDeviceNodeConfig := map[string]interface{}{
				"devices": []map[string]interface{}{
					{
						"id":      "Light",
						"type":    "esp.device.lightbulb",
						"primary": "power",
						"params": []map[string]interface{}{
							{
								"id":        "power",
								"data_type": "bool",
								"ui_type":   "esp.ui.toggle",
								"type":      "esp.param.power",
							},
							{
								"id":        "brightness",
								"data_type": "int",
								"ui_type":   "esp.ui.slider",
								"type":      "esp.param.brightness",
								"bounds":    map[string]interface{}{"min": 0, "max": 100},
							},
						},
					},
					{
						"id":      "Switch",
						"type":    "esp.device.switch",
						"primary": "power",
						"params": []map[string]interface{}{
							{
								"id":        "power",
								"data_type": "bool",
								"ui_type":   "esp.ui.toggle",
								"type":      "esp.param.power",
							},
						},
					},
				},
				"info": map[string]interface{}{
					"fw_version": "1.0",
				},
			}
			err := nodeConfigDB.UpdateServiceData("config", multiDeviceNodeConfig)
			Expect(err).To(BeNil())

			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        "params-" + groupID,
				NotificationType: "shadow_update",
				PrevState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"Light":  map[string]interface{}{"power": false, "brightness": 50},
						"Switch": map[string]interface{}{"power": false},
					},
				},
				CurrState: node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"Light":  map[string]interface{}{"power": true, "brightness": 75},
						"Switch": map[string]interface{}{"power": true},
					},
				},
				Notify: map[string]interface{}{
					gva.GVAPlatform: true,
				},
			}

			notif, err := notification.NewNotificationFromEvent(
				event.NodeID, event.TopicName, event.NotificationType,
				event.CurrState, event.PrevState, event.Notify,
			)
			Expect(err).To(BeNil())

			gvaService := gva.NewGVANotification(ctx, "https://webhook-mock.test")
			result, err := gvaService.Marshal(notif)
			Expect(err).To(BeNil())

			batch, ok := result.(gva.GVANotificationBatch)
			Expect(ok).To(BeTrue())
			Expect(batch.Reports).To(HaveLen(1))
			Expect(batch.RequestSync).To(BeFalse())

			request := batch.Reports[0]
			request.RequestID = "test_overwritten"

			lightID := fmt.Sprintf("%s.%s", nodeID, "Light")
			switchID := fmt.Sprintf("%s.%s", nodeID, "Switch")

			expectedRequest := gva.GVAReportStateRequest{
				RequestID: "test_overwritten",
				Payload: gva.GVAReportStatePayload{
					Devices: gva.GVADeviceStates{
						States: map[string]interface{}{
							lightID: map[string]interface{}{
								"online":     true,
								"on":         true,
								"brightness": 75,
							},
							switchID: map[string]interface{}{
								"online": true,
								"on":     true,
							},
						},
					},
				},
			}
			test_utils.AssertNormalizedEqual(request, expectedRequest)
		})

		// Recipient filtering: SendTo only targets users with a recorded GVA
		// account link (the row setupGVATest registers), so group members who
		// never linked Google cost no HomeGraph call.
		gvaDataRequests := func() []string {
			var agentUserIDs []string
			for _, req := range mockHTTPClient.Requests {
				if !strings.Contains(req.URL.String(), "/v1/gva/data") {
					continue
				}
				body, err := io.ReadAll(req.Body)
				Expect(err).To(BeNil())
				// Report state and request sync bodies both carry agentUserId.
				var sent struct {
					AgentUserID string `json:"agentUserId"`
				}
				Expect(json.Unmarshal(body, &sent)).To(BeNil())
				agentUserIDs = append(agentUserIDs, sent.AgentUserID)
			}
			return agentUserIDs
		}

		It("sends report state only to users with a recorded GVA link", func() {
			setupGVATest()

			gvaService := gva.NewGVANotification(ctx, "https://webhook-mock.test")
			batch := gva.GVANotificationBatch{Reports: []gva.GVAReportStateRequest{{
				RequestID: "filter-test",
			}}}

			mockHTTPClient.Requests = nil
			err := gvaService.SendTo(batch, []string{"user-without-gva-link", userID})
			Expect(err).To(BeNil())
			Expect(gvaDataRequests()).To(Equal([]string{userID}))
		})

		It("sends request sync only to users with a recorded GVA link", func() {
			setupGVATest()

			gvaService := gva.NewGVANotification(ctx, "https://webhook-mock.test")

			mockHTTPClient.Requests = nil
			err := gvaService.SendTo([]gva.GVARequestSyncRequest{{Async: false}}, []string{"user-without-gva-link", userID})
			Expect(err).To(BeNil())
			Expect(gvaDataRequests()).To(Equal([]string{userID}))
		})

		It("makes no HomeGraph calls when no recipient has a GVA link", func() {
			setupGVATest()

			gvaService := gva.NewGVANotification(ctx, "https://webhook-mock.test")
			batch := gva.GVANotificationBatch{Reports: []gva.GVAReportStateRequest{{
				RequestID: "filter-test",
			}}}

			mockHTTPClient.Requests = nil
			err := gvaService.SendTo(batch, []string{"unlinked-1", "unlinked-2"})
			Expect(err).To(BeNil())
			Expect(mockHTTPClient.Requests).To(BeEmpty(), "no token fetch or report for zero linked recipients")
		})

		It("should return empty report state when no devices changed", func() {
			setupGVATest()

			// Same state - no changes
			event := createGVANotificationEvent(map[string]interface{}{
				"power":      true,
				"brightness": 80,
			}, map[string]interface{}{
				"power":      true,
				"brightness": 80,
			})

			notif, err := notification.NewNotificationFromEvent(
				event.NodeID, event.TopicName, event.NotificationType,
				event.CurrState, event.PrevState, event.Notify,
			)
			Expect(err).To(BeNil())

			gvaService := gva.NewGVANotification(ctx, "https://webhook-mock.test")
			result, err := gvaService.Marshal(notif)
			Expect(err).To(BeNil())

			batch, ok := result.(gva.GVANotificationBatch)
			Expect(ok).To(BeTrue())
			Expect(batch.Reports).To(HaveLen(0))
		})

		It("should report offline for every device on an online-only delta", func() {
			setupGVATest()

			// A disconnect writes Online=false with no param changes (node.BuildDisconnectShadow); the device state must still be reported so HomeGraph learns the device went offline.
			params := map[string]interface{}{"power": true, "brightness": 80}
			event := createGVANotificationEvent(params, params)
			event.CurrState.Online = aws.Bool(false)

			notif, err := notification.NewNotificationFromEvent(
				event.NodeID, event.TopicName, event.NotificationType,
				event.CurrState, event.PrevState, event.Notify,
			)
			Expect(err).To(BeNil())

			gvaService := gva.NewGVANotification(ctx, "https://webhook-mock.test")
			result, err := gvaService.Marshal(notif)
			Expect(err).To(BeNil())

			batch, ok := result.(gva.GVANotificationBatch)
			Expect(ok).To(BeTrue())
			Expect(batch.Reports).To(HaveLen(1))
			Expect(batch.RequestSync).To(BeFalse())

			lightID := fmt.Sprintf("%s.%s", nodeID, "Light")
			states := batch.Reports[0].Payload.Devices.States
			lightState, ok := states[lightID].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(lightState["online"]).To(Equal(false))
		})

		It("should flag a request sync when a device name param changes", func() {
			setupGVATest()

			// Config with an esp.param.name param so the rename is detectable in the delta.
			testNode := node.NewNode(nodeID)
			nodeCtx := rmngctx.NewRmngContext(testNode)
			nodeConfigDB := node_details_db.NewNodeDetailsDB(nodeCtx)
			namedNodeConfig := map[string]interface{}{
				"devices": []map[string]interface{}{
					{
						"id":      "Light",
						"type":    "esp.device.lightbulb",
						"primary": "power",
						"params": []map[string]interface{}{
							{"id": "power", "data_type": "bool", "ui_type": "esp.ui.toggle", "type": "esp.param.power"},
							{"id": "name", "data_type": "string", "ui_type": "esp.ui.text", "type": "esp.param.name"},
						},
					},
				},
				"info": map[string]interface{}{"fw_version": "1.0"},
			}
			Expect(nodeConfigDB.UpdateServiceData("config", namedNodeConfig)).To(BeNil())

			event := createGVANotificationEvent(map[string]interface{}{
				"power": true,
				"name":  "Light",
			}, map[string]interface{}{
				"power": true,
				"name":  "Bedroom Light",
			})

			notif, err := notification.NewNotificationFromEvent(
				event.NodeID, event.TopicName, event.NotificationType,
				event.CurrState, event.PrevState, event.Notify,
			)
			Expect(err).To(BeNil())

			gvaService := gva.NewGVANotification(ctx, "https://webhook-mock.test")
			result, err := gvaService.Marshal(notif)
			Expect(err).To(BeNil())

			batch, ok := result.(gva.GVANotificationBatch)
			Expect(ok).To(BeTrue())
			Expect(batch.Reports).To(HaveLen(1))
			Expect(batch.RequestSync).To(BeTrue())
		})

		It("should handle GVA notification through Handler without error", func() {
			setupGVATest()

			event := createGVANotificationEvent(map[string]interface{}{
				"power":      false,
				"brightness": 100,
			}, map[string]interface{}{
				"power":      true,
				"brightness": 80,
			})

			// Handler should complete without error even if SendTo fails
			// (e.g., due to OAuth token fetch failure in test environment)
			err := Handler(ctx, event)
			Expect(err).To(BeNil())
		})
	})

	// Guard for connectivity-only shadow events: shadow_notify_rule also fires
	// on reported.online transitions, where notify.version has not moved and
	// the event still carries the node's lingering notify map. Only services
	// that opt in (notification.ConnectivityNotifier) may be dispatched for
	// those, or every online flip would re-deliver the last notification.
	Describe("Connectivity-only dispatch guard", func() {
		var (
			plain *recordingService
			conn  *connectivityRecordingService
		)

		BeforeEach(func() {
			plain = &recordingService{name: "recording_plain"}
			conn = &connectivityRecordingService{recordingService{name: "recording_conn"}}
			notification.Registry().Register(plain)
			notification.Registry().Register(conn)
		})

		shadowEvent := func(prevNotify, currNotify map[string]interface{}, prevOnline, currOnline *bool) NotificationEvent {
			prevParams := map[string]interface{}{}
			if prevNotify != nil {
				prevParams["notify"] = prevNotify
			}
			currParams := map[string]interface{}{}
			if currNotify != nil {
				currParams["notify"] = currNotify
			}
			return NotificationEvent{
				NodeID:           nodeID,
				TopicName:        "params-" + groupID,
				NotificationType: "shadow_update",
				PrevState:        node.ReportedOrDesiredShadow{Params: prevParams, Online: prevOnline},
				CurrState:        node.ReportedOrDesiredShadow{Params: currParams, Online: currOnline},
				Notify: map[string]interface{}{
					"recording_plain": true,
					"recording_conn":  true,
					"version":         float64(5),
				},
			}
		}

		It("dispatches every service on a notify.version bump", func() {
			event := shadowEvent(
				map[string]interface{}{"version": float64(4)},
				map[string]interface{}{"version": float64(5)},
				nil, nil)
			Expect(Handler(ctx, event)).To(BeNil())
			Expect(plain.sendTos).To(Equal(1))
			Expect(conn.sendTos).To(Equal(1))
		})

		It("dispatches on the first-ever notification with no previous version", func() {
			event := shadowEvent(
				nil,
				map[string]interface{}{"version": float64(1)},
				nil, nil)
			Expect(Handler(ctx, event)).To(BeNil())
			Expect(plain.sendTos).To(Equal(1))
			Expect(conn.sendTos).To(Equal(1))
		})

		It("skips non-connectivity services on an online flip with a lingering version", func() {
			wasOnline, nowOnline := true, false
			event := shadowEvent(
				map[string]interface{}{"version": float64(5)},
				map[string]interface{}{"version": float64(5)},
				&wasOnline, &nowOnline)
			Expect(Handler(ctx, event)).To(BeNil())
			Expect(plain.sendTos).To(Equal(0), "the lingering notify map must not be re-delivered")
			Expect(conn.sendTos).To(Equal(1), "connectivity-aware services still report the transition")
		})

		It("always dispatches direct notifications regardless of shadow state", func() {
			event := NotificationEvent{
				NodeID:           nodeID,
				TopicName:        groupID,
				NotificationType: "direct_notification",
				Notify:           map[string]interface{}{"recording_plain": true},
			}
			Expect(Handler(ctx, event)).To(BeNil())
			Expect(plain.sendTos).To(Equal(1))
		})
	})

})

// recordingService is a minimal notification service that counts dispatches;
// connectivityRecordingService additionally opts into connectivity-only events.
type recordingService struct {
	name    string
	sendTos int
}

func (r *recordingService) GetName() string { return r.name }
func (r *recordingService) GetType() notification.NotificationServiceType {
	return notification.NotificationServiceTypeUserSpecific
}
func (r *recordingService) Send(interface{}) error { return nil }
func (r *recordingService) SendTo(interface{}, []string) error {
	r.sendTos++
	return nil
}
func (r *recordingService) Marshal(*notification.Notification) (interface{}, error) {
	return "recorded", nil
}

type connectivityRecordingService struct{ recordingService }

func (c *connectivityRecordingService) NotifyOnConnectivityChange() bool { return true }

var node_cfg_simple_light_test_data = map[string]interface{}{
	"devices": []map[string]interface{}{
		{
			"id":      "Light",
			"type":    "esp.device.lightbulb",
			"primary": "power",
			"params": []map[string]interface{}{
				{
					"id":        "power",
					"data_type": "bool",
					"ui_type":   "esp.ui.toggle",
					"type":      "esp.param.power",
				},
				{
					"id":        "name",
					"data_type": "string",
					"type":      "esp.param.name",
				},
				{
					"id":        "brightness",
					"data_type": "int",
					"ui_type":   "esp.ui.slider",
					"type":      "esp.param.brightness",
					"bounds":    map[string]interface{}{"min": 0, "max": 100},
				},
			},
		},
	},
	"info": map[string]interface{}{
		"fw_version": "1.0",
	},
}

var _ = AfterSuite(func() {
	fmt.Printf("notifications profiles: %v\n", profiles)
	for key, profile := range profiles {
		if profile != nil {
			var timingFile *os.File
			timingFile, _ = test_utils.CreateCommonSummaryFile("notifications_main_" + uuid.New().String() + ".txt")
			fmt.Fprintf(timingFile, "\n--- %s ---\n", key)
			profile.Print(timingFile)
			fmt.Fprintf(timingFile, "-----------------------------\n\n")
			timingFile.Close()
		}
	}
})

func TestGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Notifications Handler Tests")
}
