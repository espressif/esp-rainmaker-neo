// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/nodes_online_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
)

var (
	profile    *mock.Profile
	timingFile *os.File
)

var _ = BeforeSuite(func() {
	timingFile, _ = test_utils.CreateCommonSummaryFile("presence_event_handler.txt")
})

var _ = AfterSuite(func() {
	if profile != nil {
		fmt.Fprintf(timingFile, "\n--- Presence Event (Disconnect Event) ---\n")
		profile.Print(timingFile)
		fmt.Fprintf(timingFile, "-----------------------------\n\n")
	}
	timingFile.Close()
})

func AddNodeSessionToDB(ctx context.Context, nodeID string, sessionID string) {
	AddNodeSessionWithVersionToDB(ctx, nodeID, sessionID, 0)
}

func AddNodeSessionWithVersionToDB(ctx context.Context, nodeID string, sessionID string, versionNumber int) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	item := map[string]types.AttributeValue{
		"clientId":          &types.AttributeValueMemberS{Value: nodeID},
		"sessionIdentifier": &types.AttributeValueMemberS{Value: sessionID},
	}
	if versionNumber != 0 {
		item["versionNumber"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", versionNumber)}
	}
	dbMock.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(nodes_online_db.NodesOnlineTable),
		Item:      item,
	})
}

var _ = Describe("awscommon.IsSQSEvent", func() {
	It("returns true for an SQSEvent payload (even when empty)", func() {
		raw, _ := json.Marshal(events.SQSEvent{Records: []events.SQSMessage{}})
		Expect(awscommon.IsSQSEvent(raw)).To(BeTrue())
	})

	It("returns true for an SQSEvent payload with records", func() {
		raw, _ := json.Marshal(events.SQSEvent{
			Records: []events.SQSMessage{
				{MessageId: "m1", Body: `{}`, EventSource: "aws:sqs"},
			},
		})
		Expect(awscommon.IsSQSEvent(raw)).To(BeTrue())
	})

	It("returns false for a direct PresenceEvent payload", func() {
		raw, _ := json.Marshal(node.PresenceEvent{
			ClientID:  "test-node",
			EventType: "disconnected",
		})
		Expect(awscommon.IsSQSEvent(raw)).To(BeFalse())
	})

	It("returns false for malformed JSON", func() {
		Expect(awscommon.IsSQSEvent([]byte(`not valid json`))).To(BeFalse())
	})
})

var _ = Describe("Presence Event Handler", func() {
	var (
		ctx       context.Context
		nodeID    string
		groupID   string
		sessionID string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		nodeID = "test-node-id"
		groupID = "test-group-id"
		sessionID = "test-session-id"
		test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
		AddNodeSessionToDB(ctx, nodeID, sessionID)

		// Skip the 10s production wait in tests; tests that need to simulate
		// DB churn during the wait override this further.
		presenceOfflineSleep = func(time.Duration) {}
	})

	AfterEach(func() {
		presenceOfflineSleep = time.Sleep
	})

	Describe("handlePresenceEvent (direct invocation)", func() {
		It("should handle a disconnected event", func() {
			event := node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "disconnected",
				IPAddress:   "192.168.1.1",
				PrincipalID: "test-principal-id",
				SessionID:   sessionID,
			}

			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.ProfileReset()

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			p := dbMock.ProfileGet()
			profile = &p

			// 1 GetNodeSessionInfo (after the wait), 1 group lookup for the first
			// shadow write (cached for the second). The node-left-group/offline
			// lifecycle hook is a fire-and-forget Lambda invoke with no DB read.
			readCount, writeCount := profile.TotalCounts()
			Expect(readCount).To(Equal(2))
			Expect(writeCount).To(Equal(0))

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadow, err := iotMock.GetDirect(nodeID, fmt.Sprintf("params-%s", groupID))
			Expect(err).To(BeNil())
			Expect(shadow).To(MatchJSON(`{"state":{"reported":{"online":false}}}`))
			shadow, err = iotMock.GetDirect(nodeID, "iparams")
			Expect(err).To(BeNil())
			Expect(shadow).To(MatchJSON(`{"state":{"reported":{"online":false}}}`))
		})

		It("should drop a stale disconnect when the FW reconnects during the delay", func() {
			// Duplicate-clientID race: while the lambda waits (direct path), the
			// connect IoT-rule's PutItem updates the row to a new session. The
			// post-wait check detects the change and skips the offline write.
			// Route through handle() so the direct-path sleep fires.
			event := node.PresenceEvent{
				ClientID:  nodeID,
				EventType: "disconnected",
				SessionID: sessionID,
			}
			raw, _ := json.Marshal(event)

			presenceOfflineSleep = func(time.Duration) {
				AddNodeSessionToDB(ctx, nodeID, "new-session-id-after-reconnect")
			}

			_, err := handle(ctx, raw)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotMock.Shadows).To(BeEmpty())
		})

		It("should write DisconnectInfo on ungraceful disconnect (KEEPALIVE_TIMEOUT)", func() {
			disconnectTimestamp := int64(1773234367770)
			event := node.PresenceEvent{
				ClientID:         nodeID,
				EventType:        "disconnected",
				SessionID:        sessionID,
				DisconnectReason: "KEEPALIVE_TIMEOUT",
				Timestamp:        disconnectTimestamp,
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadow, err := iotMock.GetDirect(nodeID, fmt.Sprintf("params-%s", groupID))
			Expect(err).To(BeNil())
			Expect(shadow).To(MatchJSON(fmt.Sprintf(
				`{"state":{"reported":{"online":false,"disconnect_info":{"last_disconnect_reason":"KEEPALIVE_TIMEOUT","last_disconnect_ts":%d}}}}`,
				disconnectTimestamp,
			)))
		})

		It("should drop disconnect when no entry exists in nodes_online (cold node)", func() {
			// The sleep must fire on the direct path even when the node isn't found —
			// the guard runs before the DDB read. Route through handle() to verify.
			coldNodeID := "test-cold-node"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, coldNodeID)

			event := node.PresenceEvent{
				ClientID:  coldNodeID,
				EventType: "disconnected",
				SessionID: "any-session",
			}
			raw, _ := json.Marshal(event)

			sleepCalled := false
			presenceOfflineSleep = func(time.Duration) { sleepCalled = true }

			_, err := handle(ctx, raw)
			Expect(err).To(BeNil())
			Expect(sleepCalled).To(BeTrue())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotMock.Shadows).NotTo(HaveKey(coldNodeID))
		})

		It("should drop disconnect when versionNumber differs (persistent session)", func() {
			// MQTT persistent session: SessionID is stable across reconnects but
			// VersionNumber increments. A stale disconnect for an older version
			// must not mark the node offline.
			persistentSessionID := "persistent-session"
			AddNodeSessionWithVersionToDB(ctx, nodeID, persistentSessionID, 5)

			event := node.PresenceEvent{
				ClientID:      nodeID,
				EventType:     "disconnected",
				SessionID:     persistentSessionID,
				VersionNumber: 4,
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotMock.Shadows).To(BeEmpty())
		})

		It("should handle a node not in any group", func() {
			nodeIDWithoutGroup := "test-node-without-group"
			sessionIDWithoutGroup := "test-session-without-group"
			AddNodeSessionToDB(ctx, nodeIDWithoutGroup, sessionIDWithoutGroup)

			event := node.PresenceEvent{
				ClientID:    nodeIDWithoutGroup,
				EventType:   "disconnected",
				IPAddress:   "192.168.1.1",
				PrincipalID: "test-principal-id",
				SessionID:   sessionIDWithoutGroup,
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)

			Expect(iotMock.Shadows).To(HaveLen(1))
			Expect(iotMock.Shadows).To(HaveKey("test-node-without-group"))
			Expect(iotMock.Shadows["test-node-without-group"]).To(And(
				HaveLen(1),
				HaveKey("iparams"),
			))

			shadowData := iotMock.Shadows["test-node-without-group"]["iparams"]
			Expect(string(shadowData)).To(MatchJSON(`{"state":{"reported":{"online":false}}}`))
		})

		It("should handle a session mismatch", func() {
			event := node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "disconnected",
				IPAddress:   "192.168.1.1",
				PrincipalID: "test-principal-id",
				SessionID:   "different-session-id",
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotMock.Shadows).To(BeEmpty())
		})

		It("should handle invalid event type", func() {
			event := node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "invalid_event",
				IPAddress:   "192.168.1.1",
				PrincipalID: "test-principal-id",
				SessionID:   sessionID,
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotMock.Shadows).To(BeEmpty())
		})

		It("should handle missing client ID", func() {
			event := node.PresenceEvent{
				EventType:   "disconnected",
				IPAddress:   "192.168.1.1",
				PrincipalID: "test-principal-id",
				SessionID:   sessionID,
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotMock.Shadows).To(BeEmpty())
		})

		It("should handle missing session ID", func() {
			event := node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "disconnected",
				IPAddress:   "192.168.1.1",
				PrincipalID: "test-principal-id",
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotMock.Shadows).To(BeEmpty())
		})

		It("should handle a connected event", func() {
			newNodeID := "test-node-connected"
			newSessionID := "test-session-connected"
			testTimestamp := int64(1773234367770)
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, newNodeID)

			event := node.PresenceEvent{
				ClientID:      newNodeID,
				EventType:     "connected",
				IPAddress:     "192.168.1.1",
				PrincipalID:   "test-principal-id",
				SessionID:     newSessionID,
				Timestamp:     testTimestamp,
				VersionNumber: 12,
			}

			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.ProfileReset()

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			p := dbMock.ProfileGet()
			profile = &p

			readCount, writeCount := profile.TotalCounts()
			Expect(readCount).To(Equal(0))
			Expect(writeCount).To(Equal(1))

			result, err := dbMock.GetItem(ctx, &dynamodb.GetItemInput{
				TableName: aws.String(nodes_online_db.NodesOnlineTable),
				Key: map[string]types.AttributeValue{
					"clientId": &types.AttributeValueMemberS{Value: newNodeID},
				},
			})
			Expect(err).To(BeNil())
			Expect(result.Item).ToNot(BeNil())

			sessionAttr, ok := result.Item["sessionIdentifier"]
			Expect(ok).To(BeTrue())
			sessionValue := sessionAttr.(*types.AttributeValueMemberS).Value
			Expect(sessionValue).To(Equal(newSessionID))

			ipAttr, ok := result.Item["ipAddress"]
			Expect(ok).To(BeTrue())
			ipValue := ipAttr.(*types.AttributeValueMemberS).Value
			Expect(ipValue).To(Equal("192.168.1.1"))

			principalAttr, ok := result.Item["principalIdentifier"]
			Expect(ok).To(BeTrue())
			principalValue := principalAttr.(*types.AttributeValueMemberS).Value
			Expect(principalValue).To(Equal("test-principal-id"))

			timestampAttr, ok := result.Item["timestamp"]
			Expect(ok).To(BeTrue())
			timestampValue := timestampAttr.(*types.AttributeValueMemberN).Value
			Expect(timestampValue).To(Equal("1773234367770"))

			eventTypeAttr, ok := result.Item["eventType"]
			Expect(ok).To(BeTrue())
			eventTypeValue := eventTypeAttr.(*types.AttributeValueMemberS).Value
			Expect(eventTypeValue).To(Equal("connected"))

			versionAttr, ok := result.Item["versionNumber"]
			Expect(ok).To(BeTrue())
			versionValue := versionAttr.(*types.AttributeValueMemberN).Value
			Expect(versionValue).To(Equal("12"))
		})

		It("should handle connected event for existing node with different session", func() {
			newSessionID := "new-session-id"
			testTimestamp := int64(1773234367770)

			event := node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "connected",
				IPAddress:   "192.168.1.2",
				PrincipalID: "test-principal-id-2",
				SessionID:   newSessionID,
				Timestamp:   testTimestamp,
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			result, err := dbMock.GetItem(ctx, &dynamodb.GetItemInput{
				TableName: aws.String(nodes_online_db.NodesOnlineTable),
				Key: map[string]types.AttributeValue{
					"clientId": &types.AttributeValueMemberS{Value: nodeID},
				},
			})
			Expect(err).To(BeNil())
			Expect(result.Item).ToNot(BeNil())

			sessionAttr, ok := result.Item["sessionIdentifier"]
			Expect(ok).To(BeTrue())
			sessionValue := sessionAttr.(*types.AttributeValueMemberS).Value
			Expect(sessionValue).To(Equal(newSessionID))

			ipAttr, ok := result.Item["ipAddress"]
			Expect(ok).To(BeTrue())
			ipValue := ipAttr.(*types.AttributeValueMemberS).Value
			Expect(ipValue).To(Equal("192.168.1.2"))

			principalAttr, ok := result.Item["principalIdentifier"]
			Expect(ok).To(BeTrue())
			principalValue := principalAttr.(*types.AttributeValueMemberS).Value
			Expect(principalValue).To(Equal("test-principal-id-2"))

			eventTypeAttr, ok := result.Item["eventType"]
			Expect(ok).To(BeTrue())
			eventTypeValue := eventTypeAttr.(*types.AttributeValueMemberS).Value
			Expect(eventTypeValue).To(Equal("connected"))
		})

		It("should handle connected event with minimal fields", func() {
			minimalNodeID := "test-node-minimal"
			minimalSessionID := "test-session-minimal"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, minimalNodeID)

			event := node.PresenceEvent{
				ClientID:  minimalNodeID,
				EventType: "connected",
				SessionID: minimalSessionID,
			}

			err := handlePresenceEvent(ctx, event)
			Expect(err).To(BeNil())

			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			result, err := dbMock.GetItem(ctx, &dynamodb.GetItemInput{
				TableName: aws.String(nodes_online_db.NodesOnlineTable),
				Key: map[string]types.AttributeValue{
					"clientId": &types.AttributeValueMemberS{Value: minimalNodeID},
				},
			})
			Expect(err).To(BeNil())
			Expect(result.Item).ToNot(BeNil())

			sessionAttr, ok := result.Item["sessionIdentifier"]
			Expect(ok).To(BeTrue())
			sessionValue := sessionAttr.(*types.AttributeValueMemberS).Value
			Expect(sessionValue).To(Equal(minimalSessionID))

			eventTypeAttr, ok := result.Item["eventType"]
			Expect(ok).To(BeTrue())
			eventTypeValue := eventTypeAttr.(*types.AttributeValueMemberS).Value
			Expect(eventTypeValue).To(Equal("connected"))

			_, ok = result.Item["ipAddress"]
			Expect(ok).To(BeFalse())

			_, ok = result.Item["principalIdentifier"]
			Expect(ok).To(BeFalse())

			_, ok = result.Item["timestamp"]
			Expect(ok).To(BeFalse())
		})
	})

	Describe("parsePresenceEvent", func() {
		It("should parse a valid presence event JSON", func() {
			body := `{
				"clientId": "test-node",
				"eventType": "disconnected",
				"ipAddress": "192.168.1.1",
				"principalIdentifier": "test-principal",
				"sessionIdentifier": "test-session",
				"timestamp": 1234567890,
				"versionNumber": 1
			}`

			event, err := parsePresenceEvent(body)
			Expect(err).To(BeNil())
			Expect(event.ClientID).To(Equal("test-node"))
			Expect(event.EventType).To(Equal("disconnected"))
			Expect(event.SessionID).To(Equal("test-session"))
		})

		It("should fail on invalid JSON", func() {
			_, err := parsePresenceEvent(`not valid json`)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal"))
		})

		It("should fail when clientId is missing", func() {
			_, err := parsePresenceEvent(`{"eventType": "disconnected"}`)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("clientId"))
		})

		It("should fail when eventType is missing", func() {
			_, err := parsePresenceEvent(`{"clientId": "test-node"}`)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("eventType"))
		})
	})

	Describe("handleSQSBatch", func() {
		It("should process a batch of presence events", func() {
			body, _ := json.Marshal(node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "disconnected",
				SessionID:   sessionID,
				IPAddress:   "192.168.1.1",
				PrincipalID: "principal-1",
			})

			sqsEvent := events.SQSEvent{
				Records: []events.SQSMessage{
					{MessageId: "msg-1", Body: string(body)},
				},
			}

			response, err := handleSQSBatch(ctx, sqsEvent)
			Expect(err).To(BeNil())
			Expect(response.BatchItemFailures).To(BeEmpty())
		})

		It("should report partial batch failures for invalid messages", func() {
			validBody, _ := json.Marshal(node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "disconnected",
				SessionID:   sessionID,
				IPAddress:   "192.168.1.1",
				PrincipalID: "principal-1",
			})

			sqsEvent := events.SQSEvent{
				Records: []events.SQSMessage{
					{MessageId: "msg-valid", Body: string(validBody)},
					{MessageId: "msg-invalid", Body: "not valid json"},
				},
			}

			response, err := handleSQSBatch(ctx, sqsEvent)
			Expect(err).To(BeNil())
			Expect(response.BatchItemFailures).To(HaveLen(1))
			Expect(response.BatchItemFailures[0].ItemIdentifier).To(Equal("msg-invalid"))
		})

		It("should handle empty batch", func() {
			response, err := handleSQSBatch(ctx, events.SQSEvent{Records: []events.SQSMessage{}})
			Expect(err).To(BeNil())
			Expect(response.BatchItemFailures).To(BeEmpty())
		})

		It("should process multiple valid events", func() {
			nodeID2 := "test-node-2"
			sessionID2 := "test-session-2"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID2)
			AddNodeSessionToDB(ctx, nodeID2, sessionID2)

			body1, _ := json.Marshal(node.PresenceEvent{
				ClientID:    nodeID,
				EventType:   "disconnected",
				SessionID:   sessionID,
				IPAddress:   "192.168.1.1",
				PrincipalID: "principal-1",
			})
			body2, _ := json.Marshal(node.PresenceEvent{
				ClientID:    nodeID2,
				EventType:   "disconnected",
				SessionID:   sessionID2,
				IPAddress:   "192.168.1.2",
				PrincipalID: "principal-2",
			})

			sqsEvent := events.SQSEvent{
				Records: []events.SQSMessage{
					{MessageId: "msg-1", Body: string(body1)},
					{MessageId: "msg-2", Body: string(body2)},
				},
			}

			response, err := handleSQSBatch(ctx, sqsEvent)
			Expect(err).To(BeNil())
			Expect(response.BatchItemFailures).To(BeEmpty())
		})

		It("should skip user events", func() {
			body, _ := json.Marshal(node.PresenceEvent{
				ClientID:    "user:test-user-id",
				EventType:   "disconnected",
				SessionID:   "user-session",
				IPAddress:   "192.168.1.1",
				PrincipalID: "principal-1",
			})

			sqsEvent := events.SQSEvent{
				Records: []events.SQSMessage{
					{MessageId: "msg-user", Body: string(body)},
				},
			}

			response, err := handleSQSBatch(ctx, sqsEvent)
			Expect(err).To(BeNil())
			Expect(response.BatchItemFailures).To(BeEmpty())
		})
	})
})

func TestPresenceEventHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PresenceEventHandler Suite")
}
