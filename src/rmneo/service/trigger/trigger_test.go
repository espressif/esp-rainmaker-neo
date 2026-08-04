// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package trigger_test

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/trigger"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTriggerService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Trigger Service Suite")
}

// MockTriggerService extends the real service with test methods
type MockTriggerService struct {
	*trigger.TriggerService
	version map[string]int
}

func NewMockTriggerService() *MockTriggerService {
	return &MockTriggerService{
		TriggerService: trigger.NewTriggerService(),
		version:        make(map[string]int),
	}
}

// GetVersion mock implementation
func (m *MockTriggerService) GetVersion(ctx *rmngctx.RmngContext, nodeId string) (int, error) {
	version, exists := m.version[nodeId]
	if !exists {
		return 0, nil
	}
	return version, nil
}

// Override Put to track versions
func (m *MockTriggerService) Put(ctx *rmngctx.RmngContext, nodeId string, payload interface{}) error {
	err := m.TriggerService.Put(ctx, nodeId, payload)
	if err != nil {
		return err
	}

	// Increment version on successful put
	m.version[nodeId] = m.version[nodeId] + 1
	return nil
}

// Delete mock implementation
func (m *MockTriggerService) Delete(ctx *rmngctx.RmngContext, nodeId string) error {
	// Use Put with empty triggers as a delete operation
	emptyPayload := map[string]interface{}{
		"triggers": []interface{}{},
	}
	return m.Put(ctx, nodeId, emptyPayload)
}

var _ = Describe("TriggerService", func() {
	var (
		service *MockTriggerService
		n       *node.Node
		rmngCtx *rmngctx.RmngContext
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		service = NewMockTriggerService()
		n = node.NewNode("test-node")
		rmngCtx = rmngctx.NewRmngContextWithCtx(context.Background(), n)
	})

	Describe("Validation", func() {
		DescribeTable("validation scenarios",
			func(payload interface{}, shouldError bool, errorMsg string) {
				err := service.Put(rmngCtx, "test-node", payload)
				if shouldError {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(errorMsg))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
			Entry("non-object payload",
				[]interface{}{},
				true,
				"trigger payload must be an object",
			),
			Entry("missing triggers field",
				map[string]interface{}{"other": "value"},
				true,
				"trigger payload must have 'triggers' field",
			),
			Entry("triggers field not array",
				map[string]interface{}{"triggers": "not-an-array"},
				true,
				"'triggers' field must be an array",
			),
			Entry("valid payload with unique IDs",
				map[string]interface{}{
					"triggers": []interface{}{
						map[string]interface{}{"id": "trigger1", "action": "do_something"},
						map[string]interface{}{"id": "trigger2", "action": "do_other"},
					},
				},
				false,
				"",
			),
			Entry("payload with duplicate IDs",
				map[string]interface{}{
					"triggers": []interface{}{
						map[string]interface{}{"id": "trigger1", "action": "do_something"},
						map[string]interface{}{"id": "trigger1", "action": "do_other"},
					},
				},
				true,
				"duplicate trigger id found: trigger1",
			),
			Entry("payload with missing ID",
				map[string]interface{}{
					"triggers": []interface{}{
						map[string]interface{}{"id": "trigger1"},
						map[string]interface{}{"action": "do_something"},
					},
				},
				true,
				"trigger element at index 1 missing string 'id' field",
			),
			Entry("payload with non-string ID",
				map[string]interface{}{
					"triggers": []interface{}{
						map[string]interface{}{"id": 123},
					},
				},
				true,
				"trigger element at index 0 missing string 'id' field",
			),
			Entry("empty triggers array",
				map[string]interface{}{
					"triggers": []interface{}{},
				},
				false,
				"",
			),
			Entry("complex payload with additional fields",
				map[string]interface{}{
					"triggers": []interface{}{
						map[string]interface{}{
							"id":        "trigger1",
							"name":      "Test Trigger",
							"condition": map[string]interface{}{"temperature": ">30"},
							"action":    map[string]interface{}{"switch": "on"},
							"enabled":   true,
						},
					},
				},
				false,
				"",
			),
		)
	})

	Describe("Put", func() {
		It("should reject payload with invalid validation", func() {
			// Test invalid payload validation
			invalidPayload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger1"},
					map[string]interface{}{"id": "trigger1"}, // Duplicate ID
				},
			}

			err := service.Put(rmngCtx, "test-node", invalidPayload)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate trigger id found: trigger1"))
		})
	})

	Describe("Trigger Storage and Retrieval", func() {
		It("should store and retrieve triggers", func() {
			// Set triggers
			payload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{
						"id":     "trigger1",
						"action": "action1",
					},
					map[string]interface{}{
						"id":     "trigger2",
						"action": "action2",
					},
				},
			}

			// Store triggers
			err := service.Put(rmngCtx, "test-node", payload)
			Expect(err).ToNot(HaveOccurred())

			// Retrieve triggers
			result, err := service.Get(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Type assertion for result
			resultMap, ok := result.(map[string]interface{})
			Expect(ok).To(BeTrue(), "Result should be a map[string]interface{}")
			Expect(resultMap).To(HaveKey("triggers"))

			triggers, ok := resultMap["triggers"].([]interface{})
			Expect(ok).To(BeTrue(), "Triggers should be a []interface{}")
			Expect(triggers).To(HaveLen(2))

			// Verify trigger contents
			trigger1 := findTriggerById(triggers, "trigger1")
			Expect(trigger1).ToNot(BeNil())
			Expect(trigger1).To(HaveKeyWithValue("action", "action1"))

			trigger2 := findTriggerById(triggers, "trigger2")
			Expect(trigger2).ToNot(BeNil())
			Expect(trigger2).To(HaveKeyWithValue("action", "action2"))
		})

		It("should return empty triggers when none exist", func() {
			// First ensure we delete any existing data for test-node
			err := service.Delete(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Now get triggers for the same node (which should be empty)
			result, err := service.Get(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Type assertion for result
			resultMap, ok := result.(map[string]interface{})
			Expect(ok).To(BeTrue(), "Result should be a map[string]interface{}")
			Expect(resultMap).To(HaveKey("triggers"))

			triggers, ok := resultMap["triggers"].([]interface{})
			Expect(ok).To(BeTrue(), "Triggers should be a []interface{}")
			Expect(triggers).To(HaveLen(0))
		})
	})

	Describe("Version Management", func() {
		It("should increment version on trigger updates", func() {
			// Set initial triggers
			initialPayload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger1", "action": "action1"},
				},
			}
			err := service.Put(rmngCtx, "test-node", initialPayload)
			Expect(err).ToNot(HaveOccurred())

			// Get initial version
			initialVersion, err := service.GetVersion(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())
			Expect(initialVersion).To(BeNumerically(">", 0))

			// Update triggers
			updatedPayload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger1", "action": "updated-action"},
				},
			}
			err = service.Put(rmngCtx, "test-node", updatedPayload)
			Expect(err).ToNot(HaveOccurred())

			// Get updated version
			updatedVersion, err := service.GetVersion(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedVersion).To(BeNumerically(">", initialVersion))
		})

		It("should not increment version on get operations", func() {
			// Set triggers
			payload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger1", "action": "action1"},
				},
			}
			err := service.Put(rmngCtx, "test-node", payload)
			Expect(err).ToNot(HaveOccurred())

			// Get initial version
			initialVersion, err := service.GetVersion(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Perform get operation
			_, err = service.Get(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Verify version hasn't changed
			currentVersion, err := service.GetVersion(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())
			Expect(currentVersion).To(Equal(initialVersion))
		})
	})

	Describe("Update Operations", func() {
		It("should properly replace existing triggers", func() {
			// Set initial triggers
			initialPayload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger1", "action": "action1"},
					map[string]interface{}{"id": "trigger2", "action": "action2"},
				},
			}
			err := service.Put(rmngCtx, "test-node", initialPayload)
			Expect(err).ToNot(HaveOccurred())

			// Update with different triggers
			updatedPayload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger2", "action": "updated-action"}, // Keep this one but update it
					map[string]interface{}{"id": "trigger3", "action": "action3"},        // Add new one
				},
			}
			err = service.Put(rmngCtx, "test-node", updatedPayload)
			Expect(err).ToNot(HaveOccurred())

			// Retrieve and verify
			result, err := service.Get(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Type assertion for result
			resultMap, ok := result.(map[string]interface{})
			Expect(ok).To(BeTrue(), "Result should be a map[string]interface{}")

			triggers, ok := resultMap["triggers"].([]interface{})
			Expect(ok).To(BeTrue(), "Triggers should be a []interface{}")
			Expect(triggers).To(HaveLen(2))

			// Verify trigger1 is gone
			trigger1 := findTriggerById(triggers, "trigger1")
			Expect(trigger1).To(BeNil())

			// Verify trigger2 is updated
			trigger2 := findTriggerById(triggers, "trigger2")
			Expect(trigger2).ToNot(BeNil())
			Expect(trigger2).To(HaveKeyWithValue("action", "updated-action"))

			// Verify trigger3 is added
			trigger3 := findTriggerById(triggers, "trigger3")
			Expect(trigger3).ToNot(BeNil())
			Expect(trigger3).To(HaveKeyWithValue("action", "action3"))
		})
	})

	Describe("Delete Operations", func() {
		It("should delete all triggers when empty array is provided", func() {
			// Set initial triggers
			initialPayload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger1", "action": "action1"},
					map[string]interface{}{"id": "trigger2", "action": "action2"},
				},
			}
			err := service.Put(rmngCtx, "test-node", initialPayload)
			Expect(err).ToNot(HaveOccurred())

			// Delete by setting empty triggers array
			emptyPayload := map[string]interface{}{
				"triggers": []interface{}{},
			}
			err = service.Put(rmngCtx, "test-node", emptyPayload)
			Expect(err).ToNot(HaveOccurred())

			// Verify all triggers are gone
			result, err := service.Get(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Type assertion for result
			resultMap, ok := result.(map[string]interface{})
			Expect(ok).To(BeTrue(), "Result should be a map[string]interface{}")

			triggers, ok := resultMap["triggers"].([]interface{})
			Expect(ok).To(BeTrue(), "Triggers should be a []interface{}")
			Expect(triggers).To(HaveLen(0))
		})

		It("should delete triggers when Delete method is called", func() {
			// Set initial triggers
			initialPayload := map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{"id": "trigger1", "action": "action1"},
				},
			}
			err := service.Put(rmngCtx, "test-node", initialPayload)
			Expect(err).ToNot(HaveOccurred())

			// Delete triggers
			err = service.Delete(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Verify all triggers are gone
			result, err := service.Get(rmngCtx, "test-node")
			Expect(err).ToNot(HaveOccurred())

			// Type assertion for result
			resultMap, ok := result.(map[string]interface{})
			Expect(ok).To(BeTrue(), "Result should be a map[string]interface{}")

			triggers, ok := resultMap["triggers"].([]interface{})
			Expect(ok).To(BeTrue(), "Triggers should be a []interface{}")
			Expect(triggers).To(HaveLen(0))
		})
	})
})

var _ = Describe("TriggerService Sharing access", func() {
	var (
		triggerService *trigger.TriggerService
		ownerUser      *user.User
		ownerCtx       *rmngctx.RmngContext
		groupID        string
		testNodeID     string
	)

	triggerPayload := func() map[string]interface{} {
		return map[string]interface{}{
			"triggers": []interface{}{
				map[string]interface{}{"id": "t1", "action": "do"},
			},
		}
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		triggerService = trigger.NewTriggerService()
		testNodeID = "test-node-id"

		ownerUser = user.NewUser("owner-user")
		ownerUser.Permissions.SetAllow(utils.GroupCreate.String(), "*")
		ownerCtx = rmngctx.NewRmngContext(ownerUser)

		createdGroup, err := group.CreateGroupForUser(ownerCtx, "Test Group")
		Expect(err).To(BeNil())
		Expect(createdGroup).ToNot(BeNil())
		groupID = createdGroup.GroupID

		test_utils.ManuallyAddNodeToGroup(context.Background(), groupID, testNodeID)
		ownerUser.Permissions.SetAllow(utils.GroupShare.String(), groupID)

		err = user.LoadNodePermissions(ownerCtx, groupID, testNodeID)
		Expect(err).To(BeNil())
	})

	It("primary access: owner can get/put/delete trigger via group ownership", func() {
		err := triggerService.Put(ownerCtx, testNodeID, triggerPayload())
		Expect(err).To(BeNil())

		retrieved, err := triggerService.Get(ownerCtx, testNodeID)
		Expect(err).To(BeNil())
		retrievedMap, ok := retrieved.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(retrievedMap).To(HaveKey("triggers"))

		err = triggerService.Delete(ownerCtx, testNodeID)
		Expect(err).To(BeNil())
	})

	It("secondary access: full group share grants get/put/delete then unshare revokes", func() {
		sharedUser := user.NewUser("shared-user")
		sharedCtx := rmngctx.NewRmngContext(sharedUser)

		_, err := triggerService.Get(sharedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to node trigger"))

		_, err = group.ShareGroup(ownerCtx, groupID, "shared-user", utils.GroupSecondaryAccess, auth.UserInfo{})
		Expect(err).To(BeNil())

		sharingRequests, err := group.GetMySharingRequests(sharedCtx)
		Expect(err).To(BeNil())
		Expect(sharingRequests).To(HaveLen(1))
		err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
		Expect(err).To(BeNil())

		err = user.LoadNodePermissions(sharedCtx, groupID, testNodeID)
		Expect(err).To(BeNil())

		err = triggerService.Put(sharedCtx, testNodeID, triggerPayload())
		Expect(err).To(BeNil())

		retrieved, err := triggerService.Get(sharedCtx, testNodeID)
		Expect(err).To(BeNil())
		retrievedMap, ok := retrieved.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(retrievedMap).To(HaveKey("triggers"))

		err = triggerService.Delete(sharedCtx, testNodeID)
		Expect(err).To(BeNil())

		err = group.UnshareGroup(ownerCtx, groupID, "shared-user")
		Expect(err).To(BeNil())

		sharedUser = user.NewUser("shared-user")
		sharedCtx = rmngctx.NewRmngContext(sharedUser)

		_, err = triggerService.Get(sharedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to node trigger"))

		err = triggerService.Put(sharedCtx, testNodeID, triggerPayload())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to update node trigger"))

		err = triggerService.Delete(sharedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to delete node trigger"))
	})

	It("subgroup access: subgroup share grants get/put/delete then unshare revokes", func() {
		createdSubgroup, err := group.CreateSubGroup(ownerCtx, groupID, "Test Subgroup")
		Expect(err).To(BeNil())
		Expect(createdSubgroup).ToNot(BeNil())
		subgroupID := createdSubgroup.SubGroupID

		_, err = group.UpdateNodeAndSubgroup(ownerCtx, groupID, testNodeID, subgroupID, group_node_db.SubGroupOperationTypeAdd)
		Expect(err).To(BeNil())

		sharedUser := user.NewUser("shared-user")
		sharedCtx := rmngctx.NewRmngContext(sharedUser)

		_, err = triggerService.Get(sharedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to node trigger"))

		_, err = group.ShareSubGroup(ownerCtx, groupID, subgroupID, "shared-user", auth.UserInfo{})
		Expect(err).To(BeNil())

		sharingRequests, err := group.GetMySharingRequests(sharedCtx)
		Expect(err).To(BeNil())
		Expect(sharingRequests).To(HaveLen(1))
		err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
		Expect(err).To(BeNil())

		err = user.LoadNodePermissions(sharedCtx, groupID, testNodeID)
		Expect(err).To(BeNil())

		err = triggerService.Put(sharedCtx, testNodeID, triggerPayload())
		Expect(err).To(BeNil())

		retrieved, err := triggerService.Get(sharedCtx, testNodeID)
		Expect(err).To(BeNil())
		retrievedMap, ok := retrieved.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(retrievedMap).To(HaveKey("triggers"))

		err = triggerService.Delete(sharedCtx, testNodeID)
		Expect(err).To(BeNil())

		err = group.UnshareSubGroup(ownerCtx, groupID, subgroupID, "shared-user")
		Expect(err).To(BeNil())

		sharedUser = user.NewUser("shared-user")
		sharedCtx = rmngctx.NewRmngContext(sharedUser)

		_, err = triggerService.Get(sharedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to node trigger"))

		err = triggerService.Put(sharedCtx, testNodeID, triggerPayload())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to update node trigger"))

		err = triggerService.Delete(sharedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to delete node trigger"))
	})

})

// Helper function to find a trigger by ID
func findTriggerById(triggers []interface{}, id string) map[string]interface{} {
	for _, t := range triggers {
		trigger, ok := t.(map[string]interface{})
		if !ok {
			continue
		}

		triggerId, ok := trigger["id"].(string)
		if ok && triggerId == id {
			return trigger
		}
	}
	return nil
}
