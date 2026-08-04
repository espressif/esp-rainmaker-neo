// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package automation

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// MockNode is a mock implementation of the Node interface for testing
type MockNode struct {
	PublishCalls []PublishCall
	PublishError error
}

type PublishCall struct {
	Context *rmngctx.RmngContext
	Data    map[string]interface{}
}

func (m *MockNode) PublishToDeviceDesired(ctx *rmngctx.RmngContext, data map[string]interface{}) error {
	m.PublishCalls = append(m.PublishCalls, PublishCall{
		Context: ctx,
		Data:    data,
	})
	return m.PublishError
}

// MockActionExecutor for testing that doesn't actually call nodes
type MockActionExecutor struct {
	ExecuteCalls []ExecuteCall
	ExecuteError error
}

type ExecuteCall struct {
	GroupID      string
	AutomationID string
	Actions      interface{}
}

func (m *MockActionExecutor) ExecuteActions(ctx *rmngctx.RmngContext, groupID, automationID string, actions interface{}) error {
	m.ExecuteCalls = append(m.ExecuteCalls, ExecuteCall{
		GroupID:      groupID,
		AutomationID: automationID,
		Actions:      actions,
	})
	return m.ExecuteError
}

var _ = Describe("ActionExecutor", func() {
	var (
		executor ActionExecutor
		rmngCtx  *rmngctx.RmngContext
	)

	BeforeEach(func() {
		executor = NewActionExecutor()
		// Create a proper mock context for testing
		rmngCtx = &rmngctx.RmngContext{
			// Initialize with minimal required fields to avoid panics
		}
	})

	AfterEach(func() {
		// Clean up any global state if needed
	})

	Describe("ExecuteActions", func() {
		Context("with nil or empty actions", func() {
			It("should handle nil actions gracefully", func() {
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", nil)
				Expect(err).To(BeNil())
			})

			It("should handle empty actions map", func() {
				actions := map[string]interface{}{}
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
			})

			It("should handle actions without targets", func() {
				actions := map[string]interface{}{
					"other_field": "value",
				}
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
			})

			It("should handle empty targets array", func() {
				actions := map[string]interface{}{
					"targets": []interface{}{},
				}
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
			})
		})

		Context("with invalid action formats", func() {
			It("should return error for non-map actions", func() {
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", "invalid")
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid actions format"))
			})

			It("should return error for non-array targets", func() {
				actions := map[string]interface{}{
					"targets": "not-an-array",
				}
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid actions format"))
			})
		})

		Context("with invalid target formats", func() {
			It("should return error for actions with invalid target array", func() {
				// lenient conversion will fail if the top-level actions structure is invalid
				actions := map[string]interface{}{
					"targets": "not-an-array", // this should cause the conversion to fail
				}
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid actions format"))
			})

			It("should skip targets with missing required fields", func() {
				actions := map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"path":  "Light.Power",
							"value": true,
							// missing "node" - will be empty string after conversion
						},
						map[string]interface{}{
							"node":  "node1",
							"value": true,
							// missing "path" - will be empty string after conversion
						},
					},
				}
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				// Should not crash, will skip targets with missing required fields during validation
				Expect(err).To(BeNil())
			})
		})

		Context("lenient actions conversion", func() {
			It("should handle valid actions with all fields", func() {
				mockExecutor := &MockActionExecutor{}

				actions := map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"node":  "test-node-001",
							"path":  "Light.Power",
							"value": true,
						},
					},
				}

				err := mockExecutor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
				Expect(mockExecutor.ExecuteCalls).To(HaveLen(1))
			})

			It("should handle different value types through lenient conversion", func() {
				mockExecutor := &MockActionExecutor{}

				testCases := []interface{}{
					true,
					false,
					123,
					45.67,
					"string_value",
					map[string]interface{}{"nested": "object"},
				}

				for _, value := range testCases {
					actions := map[string]interface{}{
						"targets": []interface{}{
							map[string]interface{}{
								"node":  "test-node",
								"path":  "Device.Param",
								"value": value,
							},
						},
					}

					err := mockExecutor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
					Expect(err).To(BeNil())
				}

				Expect(mockExecutor.ExecuteCalls).To(HaveLen(len(testCases)))
			})

			It("should ignore extra fields in action targets", func() {
				mockExecutor := &MockActionExecutor{}

				actions := map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"node":          "test-node",
							"path":          "Light.Power",
							"value":         true,
							"extra_field":   "should_be_ignored",
							"another_extra": 123,
						},
					},
				}

				err := mockExecutor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
				Expect(mockExecutor.ExecuteCalls).To(HaveLen(1))
			})
		})

		Context("validateActionTarget", func() {
			It("should validate valid action target", func() {
				target := &ActionTarget{
					Node:  "test-node-001",
					Path:  "Light.Power",
					Value: true,
				}

				err := executor.(*DefaultActionExecutor).validateActionTarget(target)
				Expect(err).To(BeNil())
			})

			It("should handle different value types", func() {
				testCases := []interface{}{
					true,
					false,
					123,
					45.67,
					"string_value",
					map[string]interface{}{"nested": "object"},
					nil, // nil values should be allowed
				}

				for _, value := range testCases {
					target := &ActionTarget{
						Node:  "test-node",
						Path:  "Device.Param",
						Value: value,
					}

					err := executor.(*DefaultActionExecutor).validateActionTarget(target)
					Expect(err).To(BeNil())
				}
			})

			It("should return error for missing node field", func() {
				target := &ActionTarget{
					Path:  "Light.Power",
					Value: true,
				}
				err := executor.(*DefaultActionExecutor).validateActionTarget(target)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("missing 'node' field"))
			})

			It("should return error for missing path field", func() {
				target := &ActionTarget{
					Node:  "test-node",
					Value: true,
				}
				err := executor.(*DefaultActionExecutor).validateActionTarget(target)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("missing 'path' field"))
			})
		})

		Context("actions format validation", func() {
			It("should return error for invalid actions format", func() {
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", "not-a-map")
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid actions format"))
			})

			It("should return error for actions that are not a map", func() {
				err := executor.ExecuteActions(rmngCtx, "group1", "auto1", []string{"invalid"})
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid actions format"))
			})
		})
	})

	Describe("Mock Action Executor", func() {
		Context("for testing integration without real node calls", func() {
			It("should track execute calls", func() {
				mockExecutor := &MockActionExecutor{}

				actions := map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"node":  "node1",
							"path":  "Light.Power",
							"value": true,
						},
					},
				}

				err := mockExecutor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
				Expect(mockExecutor.ExecuteCalls).To(HaveLen(1))
				Expect(mockExecutor.ExecuteCalls[0].GroupID).To(Equal("group1"))
				Expect(mockExecutor.ExecuteCalls[0].AutomationID).To(Equal("auto1"))
				Expect(mockExecutor.ExecuteCalls[0].Actions).To(Equal(actions))
			})

			It("should return configured error", func() {
				mockExecutor := &MockActionExecutor{
					ExecuteError: rmerror.NewRMError(nil, "mock error"),
				}

				err := mockExecutor.ExecuteActions(rmngCtx, "group1", "auto1", map[string]interface{}{})
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("mock error"))
			})
		})
	})

	Describe("action target group-membership enforcement (security)", func() {
		// Regression for the cross-tenant device-control finding: actions run
		// under a system actor whose IsAuthorized passes for any node, so a
		// target node that is not a member of the automation's group must be
		// rejected before publishing. Otherwise an automation created under a
		// group the caller owns could drive a node in another tenant's group.
		var (
			sysCtx  *rmngctx.RmngContext
			exec    *DefaultActionExecutor
			groupID = "grp-auto-security"
		)

		BeforeEach(func() {
			test_utils.TestSetup()
			sysCtx = rmngctx.NewRmngContextWithCtx(context.Background(), utils.NewSystemActor())
			exec = NewActionExecutor().(*DefaultActionExecutor)
			// Seed exactly one member node into the automation's group.
			Expect(group_node_db.NewGroupNodeDB(sysCtx).AddNode(groupID, "member-node", nil)).To(Succeed())
		})

		It("denies an action target node that is not a member of the automation group", func() {
			err := exec.executeActionTarget(sysCtx, groupID, "auto1", &ActionTarget{
				Node:  "foreign-node",
				Path:  "Light.Power",
				Value: true,
			})
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("not a member of automation group"))
		})

		It("allows an action target node that is a member of the automation group", func() {
			err := exec.executeActionTarget(sysCtx, groupID, "auto1", &ActionTarget{
				Node:  "member-node",
				Path:  "Light.Power",
				Value: true,
			})
			// Must at least pass the membership gate; any later error must not be
			// the authorization denial.
			if err != nil {
				Expect(err.Error()).ToNot(ContainSubstring("not a member"))
			}
		})
	})

	Describe("buildDesiredPayload", func() {
		Context("default data model", func() {
			It("builds a {device: {param: value}} payload", func() {
				payload, err := buildDesiredPayload(&ActionTarget{
					Node:  "node1",
					Path:  "Light.Power",
					Value: true,
				})
				Expect(err).To(BeNil())
				Expect(payload).To(Equal(map[string]interface{}{
					"Light": map[string]interface{}{
						"Power": true,
					},
				}))
			})

			It("preserves the value's original type", func() {
				payload, err := buildDesiredPayload(&ActionTarget{
					Node:  "node1",
					Path:  "Light.Brightness",
					Value: 75,
				})
				Expect(err).To(BeNil())
				Expect(payload).To(Equal(map[string]interface{}{
					"Light": map[string]interface{}{
						"Brightness": 75,
					},
				}))
			})

			It("returns an error for a path with an empty segment", func() {
				_, err := buildDesiredPayload(&ActionTarget{Node: "node1", Path: "Light."})
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("empty segment"))
			})
		})

		Context("matter attribute write", func() {
			It("nests each path segment as a key down to the value", func() {
				payload, err := buildDesiredPayload(&ActionTarget{
					Node:  "nodeC",
					Path:  "0x1.c.s.0x6.a.0x0",
					Value: true,
				})
				Expect(err).To(BeNil())
				Expect(payload).To(Equal(map[string]interface{}{
					"0x1": map[string]interface{}{
						"c": map[string]interface{}{
							"s": map[string]interface{}{
								"0x6": map[string]interface{}{
									"a": map[string]interface{}{
										"0x0": true,
									},
								},
							},
						},
					},
				}))
			})

			It("passes non-boolean values through with their original type", func() {
				cases := []interface{}{
					true,
					false,
					42,
					3.14,
					"str",
					[]interface{}{1, 2, 3},
					map[string]interface{}{"nested": "obj"},
					nil,
				}
				for _, v := range cases {
					payload, err := buildDesiredPayload(&ActionTarget{
						Node:  "nodeC",
						Path:  "0x1.c.s.0x6.a.0x0",
						Value: v,
					})
					Expect(err).To(BeNil())
					leaf := payload["0x1"].(map[string]interface{})["c"].(map[string]interface{})["s"].(map[string]interface{})["0x6"].(map[string]interface{})["a"].(map[string]interface{})
					if v == nil {
						Expect(leaf["0x0"]).To(BeNil())
					} else {
						Expect(leaf["0x0"]).To(Equal(v))
					}
				}
			})
		})

		Context("matter command invoke", func() {
			It("nests the command under the \"c\" segment", func() {
				payload, err := buildDesiredPayload(&ActionTarget{
					Node:  "nodeC",
					Path:  "0x1.c.s.0x6.c.0x1",
					Value: "150c000000",
				})
				Expect(err).To(BeNil())
				Expect(payload).To(Equal(map[string]interface{}{
					"0x1": map[string]interface{}{
						"c": map[string]interface{}{
							"s": map[string]interface{}{
								"0x6": map[string]interface{}{
									"c": map[string]interface{}{
										"0x1": "150c000000",
									},
								},
							},
						},
					},
				}))
			})

			It("keeps a TLV command value as a string", func() {
				payload, err := buildDesiredPayload(&ActionTarget{
					Node:  "nodeC",
					Path:  "0x1.c.s.0x8.c.0x4",
					Value: "042bff",
				})
				Expect(err).To(BeNil())
				cmd := payload["0x1"].(map[string]interface{})["c"].(map[string]interface{})["s"].(map[string]interface{})["0x8"].(map[string]interface{})["c"].(map[string]interface{})
				Expect(cmd["0x4"]).To(BeAssignableToTypeOf(""))
				Expect(cmd["0x4"]).To(Equal("042bff"))
			})
		})

		Context("arbitrary depth", func() {
			It("builds a two-segment matter path", func() {
				payload, err := buildDesiredPayload(&ActionTarget{
					Node:  "nodeC",
					Path:  "0x1.a",
					Value: 5,
				})
				Expect(err).To(BeNil())
				Expect(payload).To(Equal(map[string]interface{}{
					"0x1": map[string]interface{}{"a": 5},
				}))
			})
		})

		Context("malformed matter paths", func() {
			It("rejects a matter path with an empty leading segment", func() {
				_, err := buildDesiredPayload(&ActionTarget{Node: "nodeC", Path: "0x1..a.0x0"})
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("empty segment"))
			})

			It("rejects a matter path with an empty trailing segment", func() {
				_, err := buildDesiredPayload(&ActionTarget{Node: "nodeC", Path: "0x1.c.s.0x6.a."})
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("empty segment"))
			})
		})
	})

	Describe("Realistic automation scenarios", func() {
		Context("with typical home automation actions", func() {
			It("should convert single light control action correctly", func() {
				mockExecutor := &MockActionExecutor{}

				actions := map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"node":  "living-room-node-001",
							"path":  "Light.Power",
							"value": true,
						},
					},
				}

				err := mockExecutor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
				Expect(mockExecutor.ExecuteCalls).To(HaveLen(1))
				Expect(mockExecutor.ExecuteCalls[0].Actions).To(Equal(actions))
			})

			It("should convert multiple device control actions correctly", func() {
				mockExecutor := &MockActionExecutor{}

				actions := map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"node":  "living-room-node-001",
							"path":  "Light.Power",
							"value": true,
						},
						map[string]interface{}{
							"node":  "living-room-node-001",
							"path":  "Light.Brightness",
							"value": 75,
						},
						map[string]interface{}{
							"node":  "bedroom-node-002",
							"path":  "Fan.Speed",
							"value": 2,
						},
					},
				}

				err := mockExecutor.ExecuteActions(rmngCtx, "group1", "auto1", actions)
				Expect(err).To(BeNil())
				Expect(mockExecutor.ExecuteCalls).To(HaveLen(1))
				Expect(mockExecutor.ExecuteCalls[0].Actions).To(Equal(actions))
			})
		})
	})
})
