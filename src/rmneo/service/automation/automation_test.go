// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package automation

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// unwrapAutomations extracts the inner array from the wrapped Get response.
// API contract: GET returns `{"automations": [...]}`.
func unwrapAutomations(data interface{}) ([]map[string]interface{}, bool) {
	wrapper, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}
	arr, ok := wrapper["automations"].([]map[string]interface{})
	return arr, ok
}

var _ = Describe("AutomationService", func() {
	var (
		automationService *AutomationService
		testUser          *user.User
		rmngCtx           *rmngctx.RmngContext
		testGroupID       string
		mockDB            *mock.DynamoDBMock
	)

	BeforeEach(func() {
		// Initialize service registry
		service.Initialize()
		// Register automation service
		service.Registry().RegisterGroupService(NewAutomationService())

		test_utils.TestSetup()
		automationService = NewAutomationService()
		testGroupID = "test-group-id"

		testUser = user.NewUser("test-user-id")
		// Add required permissions for automation operations
		testUser.Permissions.SetAllow(utils.GroupGet.String(), testGroupID)
		testUser.Permissions.SetAllow(utils.GroupEditNodes.String(), testGroupID)
		testUser.Permissions.SetAllow(utils.GroupListSubEntities.String(), testGroupID)
		testUser.Permissions.SetAllow(utils.GroupUpdate.String(), testGroupID)
		testUser.Permissions.SetAllow(utils.GroupGetAutomation.String(), testGroupID)
		testUser.Permissions.SetAllow(utils.GroupEditAutomation.String(), testGroupID)
		testUser.Permissions.SetAllow(utils.GroupDeleteAutomation.String(), testGroupID)
		rmngCtx = rmngctx.NewRmngContext(testUser)

		// Initialize mock DynamoDB
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.ProfileReset() // Reset the profile instead of mockDB.Reset()
	})

	It("should check automation service implements ResourceID support", func() {
		Expect(automationService.SupportsResourceID()).To(BeTrue())
	})

	It("should return empty array when no automations exist", func() {
		// No need to setup mock query response since we're testing for empty results
		// The database is already empty from the BeforeEach reset

		// Verify there are no automations in the DB by checking that a non-existent item returns empty values
		var directResult automation_db.AutomationItem
		err := mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, "any-id", &directResult)
		Expect(err).To(BeNil())                         // GetDirect doesn't error for non-existent items, just returns empty struct
		Expect(directResult.GroupID).To(BeEmpty())      // Should be empty since item doesn't exist
		Expect(directResult.AutomationID).To(BeEmpty()) // Should be empty since item doesn't exist

		// Get automations through the service
		data, err := automationService.Get(rmngCtx, testGroupID)

		// Verify result
		Expect(err).To(BeNil())
		automations, ok := unwrapAutomations(data)
		Expect(ok).To(BeTrue())
		Expect(automations).To(HaveLen(0))
		// Explicitly verify it's an empty array, not null
		Expect(automations).NotTo(BeNil())
	})

	It("should successfully store and retrieve automation data", func() {
		// Test automation data
		automationData := map[string]interface{}{
			"name": "Test Automation",
			"conditions": []interface{}{
				map[string]interface{}{
					"type": "time",
					"time": "08:00",
				},
			},
			"actions": []interface{}{
				map[string]interface{}{
					"type":  "switch",
					"state": "on",
				},
			},
		}

		// Put automation data
		result, err := automationService.Put(rmngCtx, testGroupID, automationData)
		Expect(err).To(BeNil())

		// Verify result contains the correct fields
		resultMap, ok := result.(map[string]string)
		Expect(ok).To(BeTrue())
		Expect(resultMap).To(HaveKey("automation_id"))
		Expect(resultMap).To(HaveKey("message"))
		Expect(resultMap["message"]).To(Equal("success"))
		automationID := resultMap["automation_id"]
		Expect(automationID).To(HaveLen(3)) // Based on automationIDLength = 3

		// Create an automation item for testing
		automation := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: automationID,
			Payload:      automationData,
			State:        "",
		}

		// Add the automation directly to the mock database
		automationItem, _ := attributevalue.MarshalMap(automation)
		mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(automation_db.AutomationsTable),
			Item:      automationItem,
		})

		// Prepare a variable to hold the direct DB result
		var directResult automation_db.AutomationItem

		// Use GetDirect API to retrieve the automation directly from the mock database
		err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automationID, &directResult)
		Expect(err).To(BeNil())
		Expect(directResult.GroupID).To(Equal(testGroupID))
		Expect(directResult.AutomationID).To(Equal(automationID))

		// Get automations through the service to verify integration
		getData, err := automationService.Get(rmngCtx, testGroupID)
		Expect(err).To(BeNil())

		// Verify the returned data
		automations, ok := unwrapAutomations(getData)
		Expect(ok).To(BeTrue())
		Expect(automations).To(HaveLen(1))
		Expect(automations[0]).To(HaveKey("id"))
		Expect(automations[0]["id"]).To(Equal(automationID))

		// Verify payload fields are flattened onto the automation
		Expect(automations[0]).To(HaveKey("name"))
		Expect(automations[0]["name"]).To(Equal("Test Automation"))
		Expect(automations[0]["status"]).To(Equal(automation_db.AutomationStatusEnabled))
	})

	It("should retrieve a specific automation by ID", func() {
		automationID := "abc"
		automationData := map[string]interface{}{
			"name":    "Specific Automation",
			"enabled": true,
		}

		// Create an automation item for the mock response
		automation := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: automationID,
			Payload:      automationData,
			State:        "",
		}

		// Add the automation directly to the mock database
		automationItem, _ := attributevalue.MarshalMap(automation)
		mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(automation_db.AutomationsTable),
			Item:      automationItem,
		})

		// Verify we can retrieve it directly from the mock database
		var directResult automation_db.AutomationItem
		err := mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automationID, &directResult)
		Expect(err).To(BeNil())
		Expect(directResult.GroupID).To(Equal(testGroupID))
		Expect(directResult.AutomationID).To(Equal(automationID))

		// Get specific automation via the service
		data, err := automationService.GetWithResourceID(rmngCtx, testGroupID, automationID)
		Expect(err).To(BeNil())

		// Verify the result
		automationMap, ok := data.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(automationMap).To(HaveKey("id"))
		Expect(automationMap["id"]).To(Equal(automationID))

		// Verify payload fields are flattened onto the automation
		Expect(automationMap).To(HaveKey("name"))
		Expect(automationMap["name"]).To(Equal("Specific Automation"))
		Expect(automationMap["status"]).To(Equal(automation_db.AutomationStatusEnabled))
	})

	It("should update an existing automation", func() {
		automationID := "abc"
		updatedData := map[string]interface{}{
			"name":    "Updated Automation",
			"enabled": false,
		}

		// Update automation
		result, err := automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, updatedData)
		Expect(err).To(BeNil())

		// Verify result: update returns only a success message
		resultMap, ok := result.(map[string]string)
		Expect(ok).To(BeTrue())
		Expect(resultMap["message"]).To(Equal("success"))
	})

	It("should delete a specific automation", func() {
		automationID := "abc"
		automationData := map[string]interface{}{
			"name":    "Test Automation to Delete",
			"enabled": true,
		}

		// First create the automation
		automation := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: automationID,
			Payload:      automationData,
			State:        "",
		}

		// Add the automation to the mock database
		automationItem, _ := attributevalue.MarshalMap(automation)
		mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(automation_db.AutomationsTable),
			Item:      automationItem,
		})

		// Verify the automation exists before deletion
		var directResult automation_db.AutomationItem
		err := mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automationID, &directResult)
		Expect(err).To(BeNil())
		Expect(directResult.GroupID).To(Equal(testGroupID))
		Expect(directResult.AutomationID).To(Equal(automationID))

		// Delete automation
		err = automationService.DeleteWithResourceID(rmngCtx, testGroupID, automationID)
		Expect(err).To(BeNil())

		// Verify automation was deleted - use a fresh struct to avoid any residual data
		var deletedResult automation_db.AutomationItem
		err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automationID, &deletedResult)
		Expect(err).To(BeNil())                          // GetDirect doesn't error, just returns empty struct
		Expect(deletedResult.GroupID).To(BeEmpty())      // Should be empty since item was deleted
		Expect(deletedResult.AutomationID).To(BeEmpty()) // Should be empty since item was deleted
	})

	It("should delete all automations for a group", func() {
		// Create automation items for testing
		automation1ID := "abc"
		automation2ID := "def"

		// Add the test items to the mock database
		mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(automation_db.AutomationsTable),
			Item: map[string]types.AttributeValue{
				"group_id":      &types.AttributeValueMemberS{Value: testGroupID},
				"automation_id": &types.AttributeValueMemberS{Value: automation1ID},
			},
		})
		mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(automation_db.AutomationsTable),
			Item: map[string]types.AttributeValue{
				"group_id":      &types.AttributeValueMemberS{Value: testGroupID},
				"automation_id": &types.AttributeValueMemberS{Value: automation2ID},
			},
		})

		// Verify items exist using GetDirect
		var item1 automation_db.AutomationItem
		var item2 automation_db.AutomationItem
		err1 := mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automation1ID, &item1)
		err2 := mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automation2ID, &item2)
		Expect(err1).To(BeNil())
		Expect(err2).To(BeNil())

		// Delete all automations
		err := automationService.Delete(rmngCtx, testGroupID)
		Expect(err).To(BeNil())

		// Verify items no longer exist - use fresh structs to avoid any residual data
		var deletedItem1 automation_db.AutomationItem
		var deletedItem2 automation_db.AutomationItem
		err1 = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automation1ID, &deletedItem1)
		err2 = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automation2ID, &deletedItem2)
		Expect(err1).To(BeNil())                        // GetDirect doesn't error, just returns empty struct
		Expect(err2).To(BeNil())                        // GetDirect doesn't error, just returns empty struct
		Expect(deletedItem1.GroupID).To(BeEmpty())      // Should be empty since item was deleted
		Expect(deletedItem1.AutomationID).To(BeEmpty()) // Should be empty since item was deleted
		Expect(deletedItem2.GroupID).To(BeEmpty())      // Should be empty since item was deleted
		Expect(deletedItem2.AutomationID).To(BeEmpty()) // Should be empty since item was deleted
	})

	Context("automation status", func() {
		It("defaults to enabled on create when status is omitted", func() {
			automationID := "en1"
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, map[string]interface{}{
				"name": "Enabled by default",
			})
			Expect(err).To(BeNil())

			data, err := automationService.GetWithResourceID(rmngCtx, testGroupID, automationID)
			Expect(err).To(BeNil())
			automation := data.(map[string]interface{})
			Expect(automation["status"]).To(Equal(automation_db.AutomationStatusEnabled))

			var stored automation_db.AutomationItem
			err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, automationID, &stored)
			Expect(err).To(BeNil())
			storedPayload := stored.Payload.(map[string]interface{})
			Expect(storedPayload).NotTo(HaveKey("status"))
		})

		It("persists disabled status in payload when provided", func() {
			automationID := "dis"
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, map[string]interface{}{
				"status": automation_db.AutomationStatusDisabled,
				"name":   "Disabled automation",
			})
			Expect(err).To(BeNil())

			data, err := automationService.GetWithResourceID(rmngCtx, testGroupID, automationID)
			Expect(err).To(BeNil())
			Expect(data.(map[string]interface{})["status"]).To(Equal(automation_db.AutomationStatusDisabled))
		})

		It("defaults to enabled on read when an update omits status", func() {
			automationID := "prs"
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, map[string]interface{}{
				"status": automation_db.AutomationStatusDisabled,
				"name":   "Original",
			})
			Expect(err).To(BeNil())

			_, err = automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, map[string]interface{}{
				"name": "Renamed",
			})
			Expect(err).To(BeNil())

			data, err := automationService.GetWithResourceID(rmngCtx, testGroupID, automationID)
			Expect(err).To(BeNil())
			automation := data.(map[string]interface{})
			Expect(automation["status"]).To(Equal(automation_db.AutomationStatusEnabled))
			Expect(automation["name"]).To(Equal("Renamed"))
		})

		It("defaults pre-feature items without payload status to enabled on read", func() {
			automationID := "leg"
			automation := automation_db.AutomationItem{
				GroupID:      testGroupID,
				AutomationID: automationID,
				Payload: map[string]interface{}{
					"name": "Pre-feature automation",
				},
				State: "",
			}
			automationItem, _ := attributevalue.MarshalMap(automation)
			mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(automation_db.AutomationsTable),
				Item:      automationItem,
			})

			data, err := automationService.GetWithResourceID(rmngCtx, testGroupID, automationID)
			Expect(err).To(BeNil())
			Expect(data.(map[string]interface{})["status"]).To(Equal(automation_db.AutomationStatusEnabled))
		})

		It("rejects an invalid status value", func() {
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "bad", map[string]interface{}{
				"status": "paused",
				"name":   "Invalid",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Context("action target group-membership gate (security)", func() {
		// Creation-time counterpart of the executeActionTarget membership
		// check: a foreign action target must be rejected when the automation
		// is written, not only silently skipped when it triggers.
		memberNodeID := "gate-member-node"

		BeforeEach(func() {
			test_utils.ManuallyAddNodeToGroup(context.Background(), testGroupID, memberNodeID)
		})

		targetsPayload := func(nodeID string) map[string]interface{} {
			return map[string]interface{}{
				"name": "Gate test",
				"actions": map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{"node": nodeID, "path": "Light.Power", "value": true},
					},
				},
			}
		}

		It("rejects creating an automation whose action target is outside the group", func() {
			_, err := automationService.Put(rmngCtx, testGroupID, targetsPayload("foreign-node"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a member of group"))
			Expect(errors.Is(err, ErrActionTargetNotInGroup)).To(BeTrue())

			data, err := automationService.Get(rmngCtx, testGroupID)
			Expect(err).To(BeNil())
			automations, ok := unwrapAutomations(data)
			Expect(ok).To(BeTrue())
			Expect(automations).To(BeEmpty())
		})

		It("rejects updating an automation to point at an out-of-group action target", func() {
			automationID := "gt1"
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, targetsPayload(memberNodeID))
			Expect(err).To(BeNil())

			_, err = automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, targetsPayload("foreign-node"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a member of group"))

			// The stored automation keeps its original member target.
			data, err := automationService.GetWithResourceID(rmngCtx, testGroupID, automationID)
			Expect(err).To(BeNil())
			var entry AutomationEntry
			Expect(utils.ConvertAnyToAny(data, &entry)).To(Succeed())
			Expect(entry.Actions.Targets).To(HaveLen(1))
			Expect(entry.Actions.Targets[0].Node).To(Equal(memberNodeID))
		})

		It("accepts action targets that are members of the group", func() {
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "gt2", targetsPayload(memberNodeID))
			Expect(err).To(BeNil())
		})

		It("does not gate payloads whose actions are not in the executable shape", func() {
			// Free-form actions cannot execute (ExecuteActions rejects them at
			// trigger time), so there is no node reference to validate.
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "gt3", map[string]interface{}{
				"name":    "Legacy shape",
				"actions": []interface{}{map[string]interface{}{"type": "switch"}},
			})
			Expect(err).To(BeNil())
		})
	})

	Context("condition trigger group-membership gate (security)", func() {
		condMemberNodeID := "cond-member-node"

		BeforeEach(func() {
			test_utils.ManuallyAddNodeToGroup(context.Background(), testGroupID, condMemberNodeID)
		})

		conditionsPayload := func(key, nodeID string) map[string]interface{} {
			return map[string]interface{}{
				"name": "Condition gate test",
				"conditions": map[string]interface{}{
					key: []interface{}{nodeID + "~aut1~trig0"},
				},
			}
		}

		It("rejects creating an automation whose AND condition is outside the group", func() {
			_, err := automationService.Put(rmngCtx, testGroupID, conditionsPayload("and", "foreign-node"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a member of group"))
			Expect(errors.Is(err, ErrConditionNodeNotInGroup)).To(BeTrue())

			data, err := automationService.Get(rmngCtx, testGroupID)
			Expect(err).To(BeNil())
			automations, ok := unwrapAutomations(data)
			Expect(ok).To(BeTrue())
			Expect(automations).To(BeEmpty())
		})

		It("rejects a foreign node in an OR condition", func() {
			_, err := automationService.Put(rmngCtx, testGroupID, conditionsPayload("or", "foreign-node"))
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrConditionNodeNotInGroup)).To(BeTrue())
		})

		It("rejects updating an automation to condition on an out-of-group node", func() {
			automationID := "cg1"
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, conditionsPayload("and", condMemberNodeID))
			Expect(err).To(BeNil())

			_, err = automationService.PutWithResourceID(rmngCtx, testGroupID, automationID, conditionsPayload("and", "foreign-node"))
			Expect(err).To(HaveOccurred())

			data, err := automationService.GetWithResourceID(rmngCtx, testGroupID, automationID)
			Expect(err).To(BeNil())
			var entry AutomationEntry
			Expect(utils.ConvertAnyToAny(data, &entry)).To(Succeed())
			Expect(entry.Conditions.And).To(HaveLen(1))
			Expect(entry.Conditions.And[0]).To(HavePrefix(condMemberNodeID + "~"))
		})

		It("accepts conditions on nodes that are members of the group", func() {
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "cg2", conditionsPayload("and", condMemberNodeID))
			Expect(err).To(BeNil())
		})

		It("does not gate trigger IDs that carry no node segment", func() {
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "cg3", map[string]interface{}{
				"name":       "No separator",
				"conditions": map[string]interface{}{"and": []interface{}{"bare-trigger"}},
			})
			Expect(err).To(BeNil())
		})

		It("accepts a trigger ID whose automation segment is not this automation's ID", func() {
			// The automation ID is server-assigned, so callers cannot know it when
			// writing conditions and send a placeholder; gating it would reject
			// every normal create.
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "cg4", map[string]interface{}{
				"name": "Placeholder automation segment",
				"conditions": map[string]interface{}{
					"and": []interface{}{condMemberNodeID + "~placeholder~0"},
				},
			})
			Expect(err).To(BeNil())
		})

		It("validates the node segment regardless of the automation segment", func() {
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "cg5", map[string]interface{}{
				"name": "Foreign node with placeholder segment",
				"conditions": map[string]interface{}{
					"and": []interface{}{"foreign-node~placeholder~0"},
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrConditionNodeNotInGroup)).To(BeTrue())
		})

		It("validates every node referenced across both and/or lists", func() {
			_, err := automationService.PutWithResourceID(rmngCtx, testGroupID, "cg6", map[string]interface{}{
				"name": "Foreign node in a later slot",
				"conditions": map[string]interface{}{
					"and": []interface{}{condMemberNodeID + "~p~0"},
					"or":  []interface{}{condMemberNodeID + "~p~1", "foreign-node~p~2"},
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrConditionNodeNotInGroup)).To(BeTrue())
		})
	})

	It("should fail when trying to delete a non-existent automation", func() {
		nonExistentID := "xyz"

		// For this test, we need to ensure GetAutomation in the mock returns "not found"
		// First, let's make sure the item doesn't exist in our mock DB
		// We don't need to add it to the database

		// Attempt to delete non-existent automation
		err := automationService.DeleteWithResourceID(rmngCtx, testGroupID, nonExistentID)

		// Should return an error
		Expect(err).To(HaveOccurred())
		// The error message might vary depending on how the mock is implemented
		// but it should indicate the item was not found
		Expect(err.Error()).To(ContainSubstring("failed to delete automation"))
	})

	It("should delete automations whose triggers reference the removed node", func() {
		nodeID := "node-to-remove"
		otherNodeID := "node-to-keep"

		// Trigger IDs use format: nodeID~automationID~triggerIndex
		automation1 := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: "auto1",
			Payload: map[string]interface{}{
				"name": "Automation with removed node in trigger",
				"conditions": map[string]interface{}{
					"and": []interface{}{nodeID + "~auto1~0", otherNodeID + "~auto1~1"},
				},
			},
			State: "",
		}
		automation2 := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: "auto2",
			Payload: map[string]interface{}{
				"name": "Automation only referencing other node",
				"conditions": map[string]interface{}{
					"and": []interface{}{otherNodeID + "~auto2~0"},
				},
			},
			State: "",
		}

		// Add automations to mock DB
		item1, _ := attributevalue.MarshalMap(automation1)
		mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(automation_db.AutomationsTable),
			Item:      item1,
		})
		item2, _ := attributevalue.MarshalMap(automation2)
		mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(automation_db.AutomationsTable),
			Item:      item2,
		})

		// Remove automations referencing the node via service
		err := automationService.DeleteNodeFromAutomations(rmngCtx, testGroupID, nodeID)
		Expect(err).To(BeNil())

		// auto1 should be deleted (has removed node in trigger)
		var result1 automation_db.AutomationItem
		err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, "auto1", &result1)
		Expect(err).To(BeNil())
		Expect(result1.GroupID).To(BeEmpty())

		// auto2 should still exist (only references other node)
		var result2 automation_db.AutomationItem
		err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, "auto2", &result2)
		Expect(err).To(BeNil())
		Expect(result2.GroupID).To(Equal(testGroupID))
	})

	It("should update automation actions when node is only in actions (not triggers)", func() {
		nodeID := "node-in-actions"
		otherNodeID := "node-to-keep"

		// Automation where node is only in actions, not in triggers
		autoWithActions := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: "auto-act",
			Payload: map[string]interface{}{
				"name": "Automation with node in actions only",
				"conditions": map[string]interface{}{
					"and": []interface{}{otherNodeID + "~auto-act~0"},
				},
				"actions": map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{"node": nodeID, "path": "Fan.Speed", "value": 3},
						map[string]interface{}{"node": otherNodeID, "path": "Light.Power", "value": true},
					},
				},
			},
			State: "",
		}
		// Automation where node is the only action target — should be deleted entirely
		autoSoleAction := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: "auto-sole",
			Payload: map[string]interface{}{
				"name": "Automation with node as sole action target",
				"conditions": map[string]interface{}{
					"and": []interface{}{otherNodeID + "~auto-sole~0"},
				},
				"actions": map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{"node": nodeID, "path": "Fan.Speed", "value": 3},
					},
				},
			},
			State: "",
		}
		// Unrelated automation — should remain untouched
		autoUnrelated := automation_db.AutomationItem{
			GroupID:      testGroupID,
			AutomationID: "auto-unr",
			Payload: map[string]interface{}{
				"name": "Unrelated automation",
				"conditions": map[string]interface{}{
					"and": []interface{}{otherNodeID + "~auto-unr~0"},
				},
				"actions": map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{"node": otherNodeID, "path": "Light.Power", "value": true},
					},
				},
			},
			State: "",
		}

		for _, a := range []automation_db.AutomationItem{autoWithActions, autoSoleAction, autoUnrelated} {
			item, _ := attributevalue.MarshalMap(a)
			mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(automation_db.AutomationsTable),
				Item:      item,
			})
		}

		// Remove node from automations
		err := automationService.DeleteNodeFromAutomations(rmngCtx, testGroupID, nodeID)
		Expect(err).To(BeNil())

		// auto-act should still exist (node removed from actions, other target remains)
		var resultAct automation_db.AutomationItem
		err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, "auto-act", &resultAct)
		Expect(err).To(BeNil())
		Expect(resultAct.GroupID).To(Equal(testGroupID))

		// auto-sole should be deleted (sole action target was the removed node)
		var resultSole automation_db.AutomationItem
		err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, "auto-sole", &resultSole)
		Expect(err).To(BeNil())
		Expect(resultSole.GroupID).To(BeEmpty())

		// auto-unr should remain
		var resultUnr automation_db.AutomationItem
		err = mockDB.GetDirect(automation_db.AutomationsTable, testGroupID, "auto-unr", &resultUnr)
		Expect(err).To(BeNil())
		Expect(resultUnr.GroupID).To(Equal(testGroupID))
	})

	Context("permission tests", func() {
		var unauthorizedUser *user.User
		var unauthorizedCtx *rmngctx.RmngContext

		BeforeEach(func() {
			unauthorizedUser = user.NewUser("unauthorized-user")
			unauthorizedCtx = rmngctx.NewRmngContext(unauthorizedUser)
		})

		It("should fail when user doesn't have GroupGetAutomation permission", func() {
			// Attempt to get automation
			_, err := automationService.GetWithResourceID(unauthorizedCtx, testGroupID, "abc")
			Expect(err).To(HaveOccurred())

			// Attempt to list automations
			_, err = automationService.Get(unauthorizedCtx, testGroupID)
			Expect(err).To(HaveOccurred())
		})

		It("should fail when user doesn't have GroupEditAutomation permission for create/update", func() {
			// Attempt to create automation
			_, err := automationService.Put(unauthorizedCtx, testGroupID, map[string]interface{}{"key": "value"})
			Expect(err).To(HaveOccurred())

			// Attempt to update automation
			_, err = automationService.PutWithResourceID(unauthorizedCtx, testGroupID, "abc", map[string]interface{}{"key": "value"})
			Expect(err).To(HaveOccurred())
		})

		It("should fail when user doesn't have GroupDeleteAutomation permission for delete", func() {
			// Attempt to delete specific automation
			err := automationService.DeleteWithResourceID(unauthorizedCtx, testGroupID, "abc")
			Expect(err).To(HaveOccurred())

			// Attempt to delete all automations
			err = automationService.Delete(unauthorizedCtx, testGroupID)
			Expect(err).To(HaveOccurred())
		})

		It("subgroup-shared user (only GroupListSubEntities + GroupUpdateSubGroup) is denied all automation ops", func() {
			subgroupUser := user.NewUser("subgroup-user")
			for _, p := range utils.GetGroupPermissions(utils.GroupSubEntityAccess) {
				subgroupUser.Permissions.SetAllow(p, testGroupID)
			}
			subgroupCtx := rmngctx.NewRmngContext(subgroupUser)

			_, err := automationService.Get(subgroupCtx, testGroupID)
			Expect(err).To(HaveOccurred())

			_, err = automationService.GetWithResourceID(subgroupCtx, testGroupID, "abc")
			Expect(err).To(HaveOccurred())

			_, err = automationService.Put(subgroupCtx, testGroupID, map[string]interface{}{"key": "value"})
			Expect(err).To(HaveOccurred())

			_, err = automationService.PutWithResourceID(subgroupCtx, testGroupID, "abc", map[string]interface{}{"key": "value"})
			Expect(err).To(HaveOccurred())

			err = automationService.DeleteWithResourceID(subgroupCtx, testGroupID, "abc")
			Expect(err).To(HaveOccurred())

			err = automationService.Delete(subgroupCtx, testGroupID)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Sharing access", func() {
		var (
			ownerUser   *user.User
			ownerCtx    *rmngctx.RmngContext
			sharedGroup string
			testNodeID  string
		)

		// The action target must be the node seeded into the group in
		// BeforeEach: PutWithResourceID now rejects targets that are not
		// group members.
		automationData := func() map[string]interface{} {
			return map[string]interface{}{
				"name": "Test Automation",
				"conditions": map[string]interface{}{
					"and": []interface{}{testNodeID + "~auto~0"},
				},
				"actions": map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{"node": testNodeID, "path": "Light.Power", "value": true},
					},
				},
			}
		}

		BeforeEach(func() {
			testNodeID = "test-node-id"

			ownerUser = user.NewUser("owner-user")
			ownerUser.Permissions.SetAllow(utils.GroupCreate.String(), "*")
			ownerCtx = rmngctx.NewRmngContext(ownerUser)

			createdGroup, err := group.CreateGroupForUser(ownerCtx, "Test Group")
			Expect(err).To(BeNil())
			Expect(createdGroup).ToNot(BeNil())
			sharedGroup = createdGroup.GroupID

			test_utils.ManuallyAddNodeToGroup(context.Background(), sharedGroup, testNodeID)
			ownerUser.Permissions.SetAllow(utils.GroupShare.String(), sharedGroup)

			err = user.LoadNodePermissions(ownerCtx, sharedGroup, testNodeID)
			Expect(err).To(BeNil())
		})

		It("primary access: owner can put/get/delete automation via group ownership", func() {
			result, err := automationService.Put(ownerCtx, sharedGroup, automationData())
			Expect(err).To(BeNil())
			resultMap, ok := result.(map[string]string)
			Expect(ok).To(BeTrue())
			automationID := resultMap["automation_id"]
			Expect(automationID).ToNot(BeEmpty())

			data, err := automationService.Get(ownerCtx, sharedGroup)
			Expect(err).To(BeNil())
			automations, ok := unwrapAutomations(data)
			Expect(ok).To(BeTrue())
			Expect(automations).ToNot(BeEmpty())

			err = automationService.DeleteWithResourceID(ownerCtx, sharedGroup, automationID)
			Expect(err).To(BeNil())
		})

		It("secondary access: full group share grants get/put/delete then unshare revokes", func() {
			sharedUser := user.NewUser("shared-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			_, err := automationService.Get(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())

			_, err = group.ShareGroup(ownerCtx, sharedGroup, "shared-user", utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			err = user.LoadNodePermissions(sharedCtx, sharedGroup, testNodeID)
			Expect(err).To(BeNil())

			result, err := automationService.Put(sharedCtx, sharedGroup, automationData())
			Expect(err).To(BeNil())
			resultMap, ok := result.(map[string]string)
			Expect(ok).To(BeTrue())
			automationID := resultMap["automation_id"]

			data, err := automationService.Get(sharedCtx, sharedGroup)
			Expect(err).To(BeNil())
			automations, ok := unwrapAutomations(data)
			Expect(ok).To(BeTrue())
			Expect(automations).ToNot(BeEmpty())

			err = automationService.DeleteWithResourceID(sharedCtx, sharedGroup, automationID)
			Expect(err).To(BeNil())

			err = group.UnshareGroup(ownerCtx, sharedGroup, "shared-user")
			Expect(err).To(BeNil())

			sharedUser = user.NewUser("shared-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			_, err = automationService.Get(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())

			_, err = automationService.Put(sharedCtx, sharedGroup, automationData())
			Expect(err).To(HaveOccurred())

			err = automationService.Delete(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())
		})

		It("subgroup access: subgroup share denies all automation ops (read+write)", func() {
			createdSubgroup, err := group.CreateSubGroup(ownerCtx, sharedGroup, "Test Subgroup")
			Expect(err).To(BeNil())
			Expect(createdSubgroup).ToNot(BeNil())
			subgroupID := createdSubgroup.SubGroupID

			_, err = group.UpdateNodeAndSubgroup(ownerCtx, sharedGroup, testNodeID, subgroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			result, err := automationService.Put(ownerCtx, sharedGroup, automationData())
			Expect(err).To(BeNil())
			ownerResultMap, ok := result.(map[string]string)
			Expect(ok).To(BeTrue())
			ownerAutomationID := ownerResultMap["automation_id"]

			sharedUser := user.NewUser("shared-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			_, err = automationService.Get(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())

			_, err = group.ShareSubGroup(ownerCtx, sharedGroup, subgroupID, "shared-user", auth.UserInfo{})
			Expect(err).To(BeNil())

			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			err = user.LoadNodePermissions(sharedCtx, sharedGroup, testNodeID)
			Expect(err).To(BeNil())

			// Subgroup user cannot read, write, or delete automations.
			_, err = automationService.Get(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())

			_, err = automationService.GetWithResourceID(sharedCtx, sharedGroup, ownerAutomationID)
			Expect(err).To(HaveOccurred())

			_, err = automationService.Put(sharedCtx, sharedGroup, automationData())
			Expect(err).To(HaveOccurred())

			err = automationService.DeleteWithResourceID(sharedCtx, sharedGroup, ownerAutomationID)
			Expect(err).To(HaveOccurred())

			err = group.UnshareSubGroup(ownerCtx, sharedGroup, subgroupID, "shared-user")
			Expect(err).To(BeNil())

			sharedUser = user.NewUser("shared-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			_, err = automationService.Get(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())

			_, err = automationService.Put(sharedCtx, sharedGroup, automationData())
			Expect(err).To(HaveOccurred())

			err = automationService.Delete(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())
		})

		// Verifies that the dedicated GroupGetAutomation permission blocks subgroup-shared
		// users from listing automations referencing nodes outside their subgroup.
		It("subgroup access: blocks list of automations referencing out-of-scope nodes", func() {
			inScopeNodeID := "node-in-subgroup"
			outOfScopeNodeID := "node-other-subgroup"

			subgroupVisible, err := group.CreateSubGroup(ownerCtx, sharedGroup, "Visible Subgroup")
			Expect(err).To(BeNil())
			subgroupHidden, err := group.CreateSubGroup(ownerCtx, sharedGroup, "Hidden Subgroup")
			Expect(err).To(BeNil())

			test_utils.ManuallyAddNodeToGroup(context.Background(), sharedGroup, inScopeNodeID)
			test_utils.ManuallyAddNodeToGroup(context.Background(), sharedGroup, outOfScopeNodeID)
			_, err = group.UpdateNodeAndSubgroup(ownerCtx, sharedGroup, inScopeNodeID, subgroupVisible.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())
			_, err = group.UpdateNodeAndSubgroup(ownerCtx, sharedGroup, outOfScopeNodeID, subgroupHidden.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			outOfScopePayload := map[string]interface{}{
				"name": "Hidden node automation",
				"conditions": map[string]interface{}{
					"and": []interface{}{outOfScopeNodeID + "~auto~0"},
				},
				"actions": map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"node": outOfScopeNodeID, "path": "Light.Power", "value": true,
						},
					},
				},
			}
			result, err := automationService.Put(ownerCtx, sharedGroup, outOfScopePayload)
			Expect(err).To(BeNil())
			ownerResultMap, ok := result.(map[string]string)
			Expect(ok).To(BeTrue())
			outOfScopeAutoID := ownerResultMap["automation_id"]

			sharedUser := user.NewUser("shared-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			_, err = group.ShareSubGroup(ownerCtx, sharedGroup, subgroupVisible.SubGroupID, "shared-user", auth.UserInfo{})
			Expect(err).To(BeNil())

			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			err = user.LoadNodePermissions(sharedCtx, sharedGroup, inScopeNodeID)
			Expect(err).To(BeNil())

			Expect(sharedCtx.IsAuthorized(utils.NodeGet, outOfScopeNodeID)).To(HaveOccurred())

			// Subgroup-shared user lacks GroupGetAutomation, so the list endpoint denies them
			// and cannot leak out-of-scope automation payloads.
			_, err = automationService.Get(sharedCtx, sharedGroup)
			Expect(err).To(HaveOccurred())

			_, err = automationService.GetWithResourceID(sharedCtx, sharedGroup, outOfScopeAutoID)
			Expect(err).To(HaveOccurred())
		})
	})
})
