// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/automation"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/schedule"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/trigger"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNodeDataReset(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeDataReset Suite")
}

// buildPerms creates an EntityPermissions granting NodeAll for each node and GroupAll for the group.
func buildPerms(nodeIDs []string, groupID string) *rbac.EntityPermissions {
	perms := make(rbac.EntityPermissions)
	for _, nid := range nodeIDs {
		perms.SetAllow(utils.NodeAll.String(), nid)
	}
	if groupID != "" {
		perms.SetAllow(utils.GroupAll.String(), groupID)
	}
	return &perms
}

var _ = Describe("handleRequest", func() {
	var (
		mockDB *mock.DynamoDBMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

		// Register services exactly as the Lambda main() does
		service.Initialize()
		trigger.Register()
		schedule.Register()
		timeseries.Register()
		service.Registry().RegisterGroupService(automation.NewAutomationService())
	})

	It("should delete automations whose triggers reference the node but not other nodes' automations", func() {
		nodeID := "auto-node"
		otherNodeID := "other-node"
		groupID := "auto-grp"

		// Automation with node in trigger — should be deleted
		triggerAuto := automation_db.AutomationItem{
			GroupID:      groupID,
			AutomationID: "auto1",
			Payload: map[string]interface{}{
				"name":       "Trigger automation",
				"conditions": map[string]interface{}{"and": []interface{}{nodeID + "~auto1~0"}},
				"actions":    map[string]interface{}{"targets": []interface{}{map[string]interface{}{"node": otherNodeID, "path": "Light.Power", "value": true}}},
			},
		}
		// Automation referencing only other node — must remain
		otherNodeAuto := automation_db.AutomationItem{
			GroupID:      groupID,
			AutomationID: "auto2",
			Payload: map[string]interface{}{
				"name":       "Other node automation",
				"conditions": map[string]interface{}{"and": []interface{}{otherNodeID + "~auto2~0"}},
				"actions":    map[string]interface{}{"targets": []interface{}{map[string]interface{}{"node": otherNodeID, "path": "Light.Power", "value": true}}},
			},
		}
		for _, a := range []automation_db.AutomationItem{triggerAuto, otherNodeAuto} {
			item, _ := attributevalue.MarshalMap(a)
			mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(automation_db.AutomationsTable),
				Item:      item,
			})
		}

		// Also set up schedule/trigger data for the other node — must survive
		test_utils.SetupNodeServiceData(otherNodeID, "schedule", map[string]interface{}{
			"Schedules": []interface{}{map[string]interface{}{"id": "os1", "name": "Other Morning"}},
		})
		test_utils.SetupNodeServiceData(otherNodeID, "trigger", map[string]interface{}{
			"triggers": []interface{}{map[string]interface{}{"id": "ot1", "name": "Other Temp"}},
		})

		err := handleRequest(context.Background(), node.NodeDataResetEvent{
			NodeIDs: []string{nodeID}, OldGroupID: groupID,
		})
		Expect(err).To(BeNil())

		test_utils.AssertAutomationNotExists(groupID, "auto1")
		test_utils.AssertAutomationExists(groupID, "auto2")

		// Other node's service data must be untouched
		test_utils.AssertNodeServiceDataExists(otherNodeID, "schedule")
		test_utils.AssertNodeServiceDataExists(otherNodeID, "trigger")
	})

	It("should update automation actions when node is only in actions (not triggers)", func() {
		nodeID := "action-node"
		otherNodeID := "other-node"
		groupID := "action-grp"

		// Automation with node only in actions, other targets remain
		actionAuto := automation_db.AutomationItem{
			GroupID:      groupID,
			AutomationID: "auto-act",
			Payload: map[string]interface{}{
				"name":       "Action automation",
				"conditions": map[string]interface{}{"and": []interface{}{otherNodeID + "~auto-act~0"}},
				"actions": map[string]interface{}{"targets": []interface{}{
					map[string]interface{}{"node": nodeID, "path": "Fan.Speed", "value": 3},
					map[string]interface{}{"node": otherNodeID, "path": "Light.Power", "value": true},
				}},
			},
		}
		// Automation where node is the sole action target — should be deleted
		soleActionAuto := automation_db.AutomationItem{
			GroupID:      groupID,
			AutomationID: "auto-sole",
			Payload: map[string]interface{}{
				"name":       "Sole action",
				"conditions": map[string]interface{}{"and": []interface{}{otherNodeID + "~auto-sole~0"}},
				"actions": map[string]interface{}{"targets": []interface{}{
					map[string]interface{}{"node": nodeID, "path": "Fan.Speed", "value": 3},
				}},
			},
		}
		// Automation belonging entirely to other node — must remain untouched
		otherNodeAuto := automation_db.AutomationItem{
			GroupID:      groupID,
			AutomationID: "auto-other",
			Payload: map[string]interface{}{
				"name":       "Other node only",
				"conditions": map[string]interface{}{"and": []interface{}{otherNodeID + "~auto-other~0"}},
				"actions": map[string]interface{}{"targets": []interface{}{
					map[string]interface{}{"node": otherNodeID, "path": "Light.Power", "value": true},
				}},
			},
		}
		for _, a := range []automation_db.AutomationItem{actionAuto, soleActionAuto, otherNodeAuto} {
			item, _ := attributevalue.MarshalMap(a)
			mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(automation_db.AutomationsTable),
				Item:      item,
			})
		}

		err := handleRequest(context.Background(), node.NodeDataResetEvent{
			NodeIDs: []string{nodeID}, OldGroupID: groupID,
		})
		Expect(err).To(BeNil())

		// auto-act should still exist (node removed from actions, other target remains)
		test_utils.AssertAutomationExists(groupID, "auto-act")

		// auto-sole should be deleted (sole action target was the removed node)
		test_utils.AssertAutomationNotExists(groupID, "auto-sole")

		// Other node's automation must remain untouched
		test_utils.AssertAutomationExists(groupID, "auto-other")
	})

	It("should delete schedules, triggers, and automations for the target node without affecting another node", func() {
		nodeID := "full-reset-node"
		otherNodeID := "innocent-node"
		groupID := "full-reset-grp"

		// Set up schedule and trigger data for both nodes
		test_utils.SetupNodeServiceData(nodeID, "schedule", map[string]interface{}{
			"Schedules": []interface{}{map[string]interface{}{"id": "s1", "name": "Morning"}},
		})
		test_utils.SetupNodeServiceData(nodeID, "trigger", map[string]interface{}{
			"triggers": []interface{}{map[string]interface{}{"id": "t1", "name": "Temp High"}},
		})
		test_utils.SetupNodeServiceData(otherNodeID, "schedule", map[string]interface{}{
			"Schedules": []interface{}{map[string]interface{}{"id": "s2", "name": "Evening"}},
		})
		test_utils.SetupNodeServiceData(otherNodeID, "trigger", map[string]interface{}{
			"triggers": []interface{}{map[string]interface{}{"id": "t2", "name": "Humidity"}},
		})

		// Set up automations — one per node
		targetAuto := automation_db.AutomationItem{
			GroupID:      groupID,
			AutomationID: "full-auto",
			Payload: map[string]interface{}{
				"name":       "Target node automation",
				"conditions": map[string]interface{}{"and": []interface{}{nodeID + "~full-auto~0"}},
			},
		}
		otherAuto := automation_db.AutomationItem{
			GroupID:      groupID,
			AutomationID: "other-auto",
			Payload: map[string]interface{}{
				"name":       "Other node automation",
				"conditions": map[string]interface{}{"and": []interface{}{otherNodeID + "~other-auto~0"}},
			},
		}
		for _, a := range []automation_db.AutomationItem{targetAuto, otherAuto} {
			item, _ := attributevalue.MarshalMap(a)
			mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(automation_db.AutomationsTable),
				Item:      item,
			})
		}

		iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
		iotDataClient.PublishCalls = nil

		err := handleRequest(context.Background(), node.NodeDataResetEvent{
			NodeIDs: []string{nodeID}, OldGroupID: groupID,
		})
		Expect(err).To(BeNil())

		// Target node's data deleted
		test_utils.AssertNodeServiceDataDeleted(nodeID, "schedule")
		test_utils.AssertNodeServiceDataDeleted(nodeID, "trigger")
		test_utils.AssertAutomationNotExists(groupID, "full-auto")

		// Verify notifications were sent for schedule and trigger deletions
		Expect(iotDataClient.PublishCalls).To(HaveLen(2))

		scheduleNotificationFound := false
		triggerNotificationFound := false
		expectedTopic := fmt.Sprintf("rainmaker/nodes/%s/from_cloud", nodeID)

		for _, call := range iotDataClient.PublishCalls {
			Expect(*call.Topic).To(Equal(expectedTopic))

			var publishedData map[string]interface{}
			err = json.Unmarshal(call.Payload, &publishedData)
			Expect(err).To(BeNil())

			event, ok := publishedData["event"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(event).To(HaveLen(1))

			eventType := event[0].(string)
			if eventType == "getSchedDetails" {
				scheduleNotificationFound = true
				Expect(publishedData).To(HaveKey("getSchedDetails"))
			} else if eventType == "getTriggerDetails" {
				triggerNotificationFound = true
				Expect(publishedData).To(HaveKey("getTriggerDetails"))
			}
		}

		Expect(scheduleNotificationFound).To(BeTrue(), "Schedule notification should have been sent")
		Expect(triggerNotificationFound).To(BeTrue(), "Trigger notification should have been sent")

		// Other node's data must be completely untouched
		test_utils.AssertNodeServiceDataExists(otherNodeID, "schedule")
		test_utils.AssertNodeServiceDataExists(otherNodeID, "trigger")
		test_utils.AssertAutomationExists(groupID, "other-auto")
	})

	It("should return error on empty node_ids without deleting anything", func() {
		// Set up data for a node that must not be touched
		otherNodeID := "bystander-node"
		test_utils.SetupNodeServiceData(otherNodeID, "schedule", map[string]interface{}{
			"Schedules": []interface{}{map[string]interface{}{"id": "s1"}},
		})

		err := handleRequest(context.Background(), node.NodeDataResetEvent{
			NodeIDs: []string{}, OldGroupID: "grp1",
		})
		Expect(err).ToNot(BeNil())

		// Bystander data untouched
		test_utils.AssertNodeServiceDataExists(otherNodeID, "schedule")
	})

	It("should delete all automations for the group when group_delete is true", func() {
		nodeID1 := "gd-node1"
		nodeID2 := "gd-node2"
		otherNodeID := "gd-other"
		groupID := "gd-grp"

		// Set up service data for all nodes
		for _, nid := range []string{nodeID1, nodeID2} {
			test_utils.SetupNodeServiceData(nid, "schedule", map[string]interface{}{
				"Schedules": []interface{}{map[string]interface{}{"id": "s1"}},
			})
			test_utils.SetupNodeServiceData(nid, "trigger", map[string]interface{}{
				"triggers": []interface{}{map[string]interface{}{"id": "t1"}},
			})
		}
		// Other node in a different group — must survive
		test_utils.SetupNodeServiceData(otherNodeID, "schedule", map[string]interface{}{
			"Schedules": []interface{}{map[string]interface{}{"id": "s-other"}},
		})

		// Set up automations — some reference the nodes, one is unrelated to the nodes
		// but belongs to the same group (should still be deleted on group_delete)
		auto1 := automation_db.AutomationItem{
			GroupID: groupID, AutomationID: "a1",
			Payload: map[string]interface{}{
				"name":       "Node1 automation",
				"conditions": map[string]interface{}{"and": []interface{}{nodeID1 + "~a1~0"}},
			},
		}
		auto2 := automation_db.AutomationItem{
			GroupID: groupID, AutomationID: "a2",
			Payload: map[string]interface{}{
				"name":       "Node2 automation",
				"conditions": map[string]interface{}{"and": []interface{}{nodeID2 + "~a2~0"}},
			},
		}
		autoUnrelated := automation_db.AutomationItem{
			GroupID: groupID, AutomationID: "a3",
			Payload: map[string]interface{}{
				"name":       "Time-based automation (no node ref)",
				"conditions": map[string]interface{}{"and": []interface{}{"time~a3~0"}},
			},
		}
		for _, a := range []automation_db.AutomationItem{auto1, auto2, autoUnrelated} {
			item, _ := attributevalue.MarshalMap(a)
			mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(automation_db.AutomationsTable),
				Item:      item,
			})
		}

		iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
		iotDataClient.PublishCalls = nil

		err := handleRequest(context.Background(), node.NodeDataResetEvent{
			NodeIDs:     []string{nodeID1, nodeID2},
			OldGroupID:  groupID,
			GroupDelete: true,
		})
		Expect(err).To(BeNil())

		// All service data deleted for both target nodes
		for _, nid := range []string{nodeID1, nodeID2} {
			test_utils.AssertNodeServiceDataDeleted(nid, "schedule")
			test_utils.AssertNodeServiceDataDeleted(nid, "trigger")
		}

		// Verify notifications were sent for both nodes (schedule + trigger for each)
		Expect(iotDataClient.PublishCalls).To(HaveLen(4))

		node1ScheduleNotificationFound := false
		node1TriggerNotificationFound := false
		node2ScheduleNotificationFound := false
		node2TriggerNotificationFound := false

		for _, call := range iotDataClient.PublishCalls {
			var publishedData map[string]interface{}
			err = json.Unmarshal(call.Payload, &publishedData)
			Expect(err).To(BeNil())

			event, ok := publishedData["event"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(event).To(HaveLen(1))

			eventType := event[0].(string)
			topic := *call.Topic

			if topic == fmt.Sprintf("rainmaker/nodes/%s/from_cloud", nodeID1) {
				if eventType == "getSchedDetails" {
					node1ScheduleNotificationFound = true
					Expect(publishedData).To(HaveKey("getSchedDetails"))
				} else if eventType == "getTriggerDetails" {
					node1TriggerNotificationFound = true
					Expect(publishedData).To(HaveKey("getTriggerDetails"))
				}
			} else if topic == fmt.Sprintf("rainmaker/nodes/%s/from_cloud", nodeID2) {
				if eventType == "getSchedDetails" {
					node2ScheduleNotificationFound = true
					Expect(publishedData).To(HaveKey("getSchedDetails"))
				} else if eventType == "getTriggerDetails" {
					node2TriggerNotificationFound = true
					Expect(publishedData).To(HaveKey("getTriggerDetails"))
				}
			}
		}

		Expect(node1ScheduleNotificationFound).To(BeTrue(), "Schedule notification should have been sent for node1")
		Expect(node1TriggerNotificationFound).To(BeTrue(), "Trigger notification should have been sent for node1")
		Expect(node2ScheduleNotificationFound).To(BeTrue(), "Schedule notification should have been sent for node2")
		Expect(node2TriggerNotificationFound).To(BeTrue(), "Trigger notification should have been sent for node2")

		// ALL automations for the group wiped (not just ones referencing the nodes)
		test_utils.AssertNoAutomationsForGroup(groupID)

		// Other node's data in a different context must survive
		test_utils.AssertNodeServiceDataExists(otherNodeID, "schedule")
	})
})
