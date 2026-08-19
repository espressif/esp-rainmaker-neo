// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Service Suite")
}

var _ = Describe("ConfigService", func() {
	var (
		configService *config.ConfigService
		testUser      *user.User
		rmngCtx       *rmngctx.RmngContext
		testNodeID    string
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		service.Initialize()
		configService = config.NewConfigService()
		testNodeID = "test-node-id"

		testUser = user.NewUser("test-user-id")
		testUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
		testUser.Permissions.SetAllow(utils.NodePutConfig.String(), testNodeID)
		testUser.Permissions.SetAllow(utils.NodeDeleteConfig.String(), testNodeID)
		rmngCtx = rmngctx.NewRmngContext(testUser)
	})

	Describe("Get", func() {
		It("should return error when node config doesn't exist", func() {
			_, err := configService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("Node has no config"))
		})

		It("should return config data when node config exists", func() {
			// Create test config data
			testConfig := map[string]interface{}{
				"devices": []interface{}{
					map[string]interface{}{
						"id":   "Test Device",
						"type": "switch",
						"params": []interface{}{
							map[string]interface{}{
								"id":        "power",
								"type":      "bool",
								"data_type": "bool",
							},
						},
					},
				},
				"info": map[string]interface{}{
					"fw_version": "1.0.0",
				},
			}

			// Store test config in DB using node context
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(nodeCtx)
			err := nodeDetailsDB.UpdateServiceData("config", testConfig)
			Expect(err).To(BeNil())

			// Get config data using user context
			data, err := configService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Verify data
			retrievedMap, ok := data.(map[string]interface{})
			Expect(ok).To(BeTrue(), "Retrieved data should be a map")
			Expect(retrievedMap).To(HaveKey("devices"))
			Expect(retrievedMap).To(HaveKey("info"))

			devicesArray, ok := retrievedMap["devices"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(devicesArray).To(HaveLen(1))

			deviceMap, ok := devicesArray[0].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(deviceMap["id"]).To(Equal("Test Device"))
			Expect(deviceMap["type"]).To(Equal("switch"))

			paramsArray, ok := deviceMap["params"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(paramsArray).To(HaveLen(1))
			paramMap, ok := paramsArray[0].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(paramMap["id"]).To(Equal("power"))
			Expect(paramMap["type"]).To(Equal("bool"))

			// Verify ToNodeCfg conversion
			nodeCfg, convErr := config.ToNodeCfg(data)
			Expect(convErr).To(BeNil())
			Expect(nodeCfg.Devices).To(HaveLen(1))
			Expect(nodeCfg.Devices[0].ID).To(Equal("Test Device"))
			Expect(nodeCfg.Devices[0].Type).To(Equal("switch"))
			Expect(nodeCfg.Devices[0].Params).To(HaveLen(1))
			Expect(nodeCfg.Devices[0].Params[0].ID).To(Equal("power"))
			Expect(nodeCfg.Devices[0].Params[0].Type).To(Equal("bool"))
			Expect(nodeCfg.Devices[0].Params[0].DataType).To(Equal("bool"))
			Expect(nodeCfg.Info.FWVersion).To(Equal("1.0.0"))

			// Verify ToMap conversion
			convertedMap := nodeCfg.ToMap()
			Expect(convertedMap).To(Equal(testConfig))
		})

		It("should handle Matter data model config with nested endpoints, clusters, and attributes", func() {
			matterConfig := map[string]interface{}{
				"data_model": "matter",
				"endpoints": map[string]interface{}{
					"0x1": map[string]interface{}{
						"c": map[string]interface{}{
							// Buckets reflect the corrected data model: "a" lists only plain
							// attributes (not indexed/timeseries/config-only); "i" and "ts"
							// are independent and may overlap; "v" holds config-only values.
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
									// plain attributes only
									"a": []interface{}{"0x2", "0x3", "0x4", "0xf", "0x15", "0x16", "0x17", "0x19", "0x1a", "0x1b", "0x20", "0x21", "0x22", "0x24", "0x25", "0x26", "0xfffc", "0xfffd"},
									// indexed and timeseries overlap on 0x0, 0x1, 0x7
									"i":  []interface{}{"0x0", "0x1", "0x7", "0x8", "0x11", "0x12", "0x13"},
									"ts": []interface{}{"0x0", "0x1", "0x7"},
									// config-only attribute values (id -> value), sibling to a/i/ts
									"v": map[string]interface{}{"0x10": float64(42)},
								},
								// cluster with only config-only attrs: no "a" array is emitted
								"0x1d": map[string]interface{}{
									"v": map[string]interface{}{"0x0": float64(7)},
								},
							},
						},
					},
				},
				"info": map[string]interface{}{
					"fw_version": "36b47e808-dirty",
					"type":       "smartlight-mtr-app",
				},
			}

			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(nodeCtx)
			err := nodeDetailsDB.UpdateServiceData("config", matterConfig)
			Expect(err).To(BeNil())

			data, err := configService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			retrievedMap, ok := data.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(retrievedMap).To(HaveKey("data_model"))
			Expect(retrievedMap["data_model"]).To(Equal("matter"))
			Expect(retrievedMap).To(HaveKey("endpoints"))
			Expect(retrievedMap).To(HaveKey("info"))

			endpoints, ok := retrievedMap["endpoints"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(endpoints).To(HaveKey("0x1"))

			endpoint1, ok := endpoints["0x1"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(endpoint1).To(HaveKey("c"))

			clusters, ok := endpoint1["c"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(clusters).To(HaveKey("s"))

			serverClusters, ok := clusters["s"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(serverClusters).To(HaveKey("0x3"))
			Expect(serverClusters).To(HaveKey("0x300"))
			Expect(serverClusters).To(HaveKey("0x4"))
			Expect(serverClusters).To(HaveKey("0x6"))
			Expect(serverClusters).To(HaveKey("0x8"))

			cluster300, ok := serverClusters["0x300"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(cluster300).To(HaveKey("a"))

			attributes, ok := cluster300["a"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(attributes).To(HaveLen(18))
			// plain "a" excludes indexed/timeseries/config-only IDs
			Expect(attributes).To(ContainElement("0x2"))
			Expect(attributes).ToNot(ContainElement("0x0"))
			Expect(attributes).ToNot(ContainElement("0x10"))

			cluster6, ok := serverClusters["0x6"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			cluster6Attrs, ok := cluster6["a"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(cluster6Attrs).To(HaveLen(5))
			Expect(cluster6Attrs).To(ContainElement("0x4001"))
			Expect(cluster6Attrs).ToNot(ContainElement("0x0"))

			info, ok := retrievedMap["info"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(info["fw_version"]).To(Equal("36b47e808-dirty"))
			Expect(info["type"]).To(Equal("smartlight-mtr-app"))

			// Verify ToNodeCfg conversion
			nodeCfg, convErr := config.ToNodeCfg(data)
			Expect(convErr).To(BeNil())
			Expect(nodeCfg.DataModel).To(Equal("matter"))
			Expect(nodeCfg.Endpoints).To(HaveKey("0x1"))
			Expect(nodeCfg.Info.FWVersion).To(Equal("36b47e808-dirty"))
			Expect(nodeCfg.Info.Type).To(Equal("smartlight-mtr-app"))

			endpoint1Val := nodeCfg.Endpoints["0x1"]
			Expect(endpoint1Val.Clusters.Servers).To(HaveKey("0x3"))
			Expect(endpoint1Val.Clusters.Servers).To(HaveKey("0x4"))
			Expect(endpoint1Val.Clusters.Servers).To(HaveKey("0x6"))
			Expect(endpoint1Val.Clusters.Servers).To(HaveKey("0x8"))
			Expect(endpoint1Val.Clusters.Servers).To(HaveKey("0x300"))

			cluster300Val := endpoint1Val.Clusters.Servers["0x300"]
			Expect(cluster300Val.Attributes).To(HaveLen(18))
			Expect(cluster300Val.Attributes).To(ContainElement("0x2"))
			Expect(cluster300Val.Indexed).To(HaveLen(7))
			Expect(cluster300Val.TimeSeries).To(HaveLen(3))
			// "i" and "ts" are independent and may overlap on the same attribute ID
			Expect(cluster300Val.Indexed).To(ContainElement("0x0"))
			Expect(cluster300Val.TimeSeries).To(ContainElement("0x0"))

			cluster6Val := endpoint1Val.Clusters.Servers["0x6"]
			Expect(cluster6Val.Attributes).To(HaveLen(5))
			Expect(cluster6Val.Attributes).To(ContainElement("0x4001"))
			Expect(cluster6Val.Indexed).To(ContainElement("0x4000"))

			// Config-only attributes surface under "v" (object), not "a"
			Expect(cluster300).To(HaveKey("v"))
			Expect(cluster300Val.ConfigOnly).To(HaveKeyWithValue("0x10", float64(42)))

			// Cluster with only config-only attrs omits the "a" array entirely
			Expect(serverClusters).To(HaveKey("0x1d"))
			cluster1d, ok := serverClusters["0x1d"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(cluster1d).ToNot(HaveKey("a"))
			Expect(cluster1d).To(HaveKey("v"))
			cluster1dVal := endpoint1Val.Clusters.Servers["0x1d"]
			Expect(cluster1dVal.Attributes).To(BeEmpty())
			Expect(cluster1dVal.ConfigOnly).To(HaveKeyWithValue("0x0", float64(7)))

			// Verify ToMap conversion
			convertedMap := nodeCfg.ToMap()
			Expect(convertedMap).To(Equal(matterConfig))
		})

		It("should return error when user is not authorized", func() {
			// Create user without permissions
			unauthorizedUser := user.NewUser("unauthorized-user")
			unauthorizedCtx := rmngctx.NewRmngContext(unauthorizedUser)

			_, err := configService.Get(unauthorizedCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node config"))
		})

		It("should allow primary access via group ownership", func() {
			testConfig := map[string]interface{}{
				"device_type": "thermostat",
			}
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(nodeCtx)
			err := nodeDetailsDB.UpdateServiceData("config", testConfig)
			Expect(err).To(BeNil())

			ownerUser := user.NewUser("owner-user")
			ownerUser.Permissions.SetAllow(utils.GroupCreate.String(), "*")
			ownerCtx := rmngctx.NewRmngContext(ownerUser)

			createdGroup, err := group.CreateGroupForUser(ownerCtx, "Test Group")
			Expect(err).To(BeNil())
			groupID := createdGroup.GroupID

			test_utils.ManuallyAddNodeToGroup(context.Background(), groupID, testNodeID)

			err = user.LoadNodePermissions(ownerCtx, groupID, testNodeID)
			Expect(err).To(BeNil())

			data, err := configService.Get(ownerCtx, testNodeID)
			Expect(err).To(BeNil())
			Expect(data).To(Equal(testConfig))
		})

		It("should allow access after group is shared and remove after unshared", func() {
			// Create test config data
			testConfig := map[string]interface{}{
				"device_type": "air-conditioner",
			}

			// Store test config in DB using node context
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(nodeCtx)
			err := nodeDetailsDB.UpdateServiceData("config", testConfig)
			Expect(err).To(BeNil())

			// Create users
			ownerUser := user.NewUser("owner-user")
			ownerUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
			ownerUser.Permissions.SetAllow(utils.GroupCreate.String(), "*")
			ownerCtx := rmngctx.NewRmngContext(ownerUser)

			sharedUser := user.NewUser("shared-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			// Initially, shared user should not have access
			_, err = configService.Get(sharedCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node config"))

			// Create a group first
			createdGroup, err := group.CreateGroupForUser(ownerCtx, "Test Group")
			Expect(err).To(BeNil())
			Expect(createdGroup).ToNot(BeNil())
			groupID := createdGroup.GroupID

			// Add node to group
			test_utils.ManuallyAddNodeToGroup(context.Background(), groupID, testNodeID)

			// Share group with shared user
			ownerUser.Permissions.SetAllow(utils.GroupShare.String(), groupID)
			_, err = group.ShareGroup(ownerCtx, groupID, "shared-user", utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			// Accept sharing request
			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			// Load node permissions for shared user
			err = user.LoadNodePermissions(sharedCtx, groupID, testNodeID)
			Expect(err).To(BeNil())

			// Now shared user should have access
			data, err := configService.Get(sharedCtx, testNodeID)
			Expect(err).To(BeNil())
			Expect(data).To(Equal(testConfig))

			// Unshare group
			err = group.UnshareGroup(ownerCtx, groupID, "shared-user")
			Expect(err).To(BeNil())

			// Create a new context for the shared user to ensure permissions are reloaded
			sharedUser = user.NewUser("shared-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			// Verify access is removed
			_, err = configService.Get(sharedCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node config"))
		})

		It("should allow access after subgroup is shared and remove after unshared", func() {
			// Create test config data
			testConfig := map[string]interface{}{
				"device_type": "air-conditioner",
			}

			// Store test config in DB using node context
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(nodeCtx)
			err := nodeDetailsDB.UpdateServiceData("config", testConfig)
			Expect(err).To(BeNil())

			// Create users
			ownerUser := user.NewUser("owner-user")
			ownerUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
			ownerUser.Permissions.SetAllow(utils.GroupCreate.String(), "*")
			ownerCtx := rmngctx.NewRmngContext(ownerUser)

			sharedUser := user.NewUser("shared-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			// Initially, shared user should not have access
			_, err = configService.Get(sharedCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node config"))

			// Create a group first
			createdGroup, err := group.CreateGroupForUser(ownerCtx, "Test Group")
			Expect(err).To(BeNil())
			Expect(createdGroup).ToNot(BeNil())
			groupID := createdGroup.GroupID

			// Create a subgroup
			createdSubgroup, err := group.CreateSubGroup(ownerCtx, groupID, "Test Subgroup")
			Expect(err).To(BeNil())
			Expect(createdSubgroup).ToNot(BeNil())
			subgroupID := createdSubgroup.SubGroupID

			// Add node to subgroup
			test_utils.ManuallyAddNodeToGroup(context.Background(), groupID, testNodeID)
			_, err = group.UpdateNodeAndSubgroup(ownerCtx, groupID, testNodeID, subgroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Share subgroup with shared user
			ownerUser.Permissions.SetAllow(utils.GroupShare.String(), groupID)
			_, err = group.ShareSubGroup(ownerCtx, groupID, subgroupID, "shared-user", auth.UserInfo{})
			Expect(err).To(BeNil())

			// Accept sharing request
			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			// Load node permissions for shared user
			err = user.LoadNodePermissions(sharedCtx, groupID, testNodeID)
			Expect(err).To(BeNil())

			// Set up permissions for the shared user
			// sharedUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)

			// Now shared user should have access
			data, err := configService.Get(sharedCtx, testNodeID)
			Expect(err).To(BeNil())
			Expect(data).To(Equal(testConfig))

			// Unshare subgroup
			err = group.UnshareSubGroup(ownerCtx, groupID, subgroupID, "shared-user")
			Expect(err).To(BeNil())

			// Create a new context for the shared user to ensure permissions are reloaded
			sharedUser = user.NewUser("shared-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			// Verify access is removed
			_, err = configService.Get(sharedCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node config"))
		})

		Context("when dealing with missing or incomplete configurations", func() {
			var (
				nodeWithoutConfig   string
				nodeWithEmptyConfig string
			)

			BeforeEach(func() {
				nodeWithoutConfig = "node-without-config"
				nodeWithEmptyConfig = "node-with-empty-config"

				// Set up permissions for test user
				testUser.Permissions.SetAllow(utils.NodeGet.String(), nodeWithoutConfig)
				testUser.Permissions.SetAllow(utils.NodeGet.String(), nodeWithEmptyConfig)

				// Create empty config for one node
				nodeCtx := rmngctx.NewRmngContext(node.NewNode(nodeWithEmptyConfig))
				nodeDetailsDB := node_details_db.NewNodeDetailsDB(nodeCtx)
				err := nodeDetailsDB.UpdateServiceData("config", map[string]interface{}{})
				Expect(err).To(BeNil())
			})

			It("should return error when node has no configuration", func() {
				_, err := configService.Get(rmngCtx, nodeWithoutConfig)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("Node has no config"))
			})

			It("should return empty map when node has empty configuration", func() {
				data, err := configService.Get(rmngCtx, nodeWithEmptyConfig)
				Expect(err).To(BeNil())
				Expect(data).To(BeEmpty())
			})

			It("should return error for non-existent node", func() {
				nonExistentNode := "non-existent-node"
				testUser.Permissions.SetAllow(utils.NodeGet.String(), nonExistentNode)

				_, err := configService.Get(rmngCtx, nonExistentNode)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("Node has no config"))
			})

			// A node writes its own config over MQTT, so its field types are not under cloud
			// control. A mistyped field decodes to a zero value, and dropping the error left the
			// caller unable to tell "the node declared nothing" from "we could not read it".
			It("should surface a decode error rather than returning a zero-valued config", func() {
				mistyped := map[string]interface{}{
					"data_model": "default",
					// devices must be a list; a node that serialises it as an object is an
					// ordinary slip for a C JSON writer.
					"devices": map[string]interface{}{"unexpected": "object"},
				}

				cfg, err := config.ToNodeCfg(mistyped)

				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("failed to decode node config"))
				Expect(cfg.Devices).To(BeEmpty())
			})

			It("should still decode a well-formed config without error", func() {
				cfg, err := config.ToNodeCfg(map[string]interface{}{
					"data_model": "default",
					"devices": []interface{}{map[string]interface{}{
						"id":     "Light",
						"type":   "esp.device.lightbulb",
						"params": []interface{}{},
					}},
				})

				Expect(err).To(BeNil())
				Expect(cfg.Devices).To(HaveLen(1))
				Expect(cfg.Devices[0].ID).To(Equal("Light"))
			})

			It("should treat a nil config as an empty one without erroring", func() {
				cfg, err := config.ToNodeCfg(nil)
				Expect(err).To(BeNil())
				Expect(cfg.Devices).To(BeEmpty())
			})
		})
	})

	Describe("Put", func() {
		const testGroupID = "test-group-id"

		validMatterConfig := func() map[string]interface{} {
			return map[string]interface{}{
				"data_model": "matter",
				"info": map[string]interface{}{
					"name":       "Living Room Light",
					"type":       "matter",
					"fw_version": "1.0",
					"model":      "0x010D",
				},
				"endpoints": map[string]interface{}{
					"0x0": map[string]interface{}{
						"dt": "0x0016",
						"c": map[string]interface{}{
							"s": map[string]interface{}{
								"0x1d": map[string]interface{}{},
								"0x28": map[string]interface{}{},
								"0x1f": map[string]interface{}{},
								"0x3e": map[string]interface{}{},
							},
						},
					},
					"0x1": map[string]interface{}{
						"dt": "0x010D",
						"c": map[string]interface{}{
							"s": map[string]interface{}{
								"0x3": map[string]interface{}{},
								"0x4": map[string]interface{}{},
								"0x6": map[string]interface{}{
									"a": []interface{}{"0x0"},
								},
								"0x8": map[string]interface{}{
									"a": []interface{}{"0x0"},
								},
								"0x300": map[string]interface{}{
									"a": []interface{}{"0x7", "0x8", "0x0f"},
								},
								"0x1d": map[string]interface{}{},
							},
						},
					},
				},
			}
		}

		It("should write config for a pure Matter node and read it back", func() {
			test_utils.ManuallyAddNodeToGroupWithCapabilities(rmngCtx.Context, testGroupID, testNodeID, group.MatterCapabilityName)

			cfg := validMatterConfig()
			err := configService.Put(rmngCtx, testNodeID, cfg)
			Expect(err).To(BeNil())

			data, err := configService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())
			Expect(data).To(Equal(cfg))
		})

		It("should reject a RainMaker node (rmng capability)", func() {
			test_utils.ManuallyAddNodeToGroupWithCapabilities(rmngCtx.Context, testGroupID, testNodeID, group.NodeCapabilityRMNG)

			err := configService.Put(rmngCtx, testNodeID, validMatterConfig())
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, config.ErrNotPureMatter)).To(BeTrue())
		})

		It("should reject a hybrid rmng+matter node", func() {
			test_utils.ManuallyAddNodeToGroupWithCapabilities(rmngCtx.Context, testGroupID, testNodeID, group.MatterCapabilityName, group.NodeCapabilityRMNG)

			err := configService.Put(rmngCtx, testNodeID, validMatterConfig())
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, config.ErrNotPureMatter)).To(BeTrue())
		})

		It("should reject a node that is not in any group", func() {
			err := configService.Put(rmngCtx, testNodeID, validMatterConfig())
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, config.ErrNotPureMatter)).To(BeTrue())
		})

		It("should reject a user without node access", func() {
			test_utils.ManuallyAddNodeToGroupWithCapabilities(rmngCtx.Context, testGroupID, testNodeID, group.MatterCapabilityName)

			unauthorizedCtx := rmngctx.NewRmngContext(user.NewUser("other-user-id"))
			err := configService.Put(unauthorizedCtx, testNodeID, validMatterConfig())
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Delete", func() {
		It("should always return error as DELETE is not allowed", func() {
			err := configService.Delete(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("DELETE operation not allowed for config service"))
		})
	})

	Describe("SetNodeConfig", func() {
		It("should allow node to set its own config", func() {
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			configService := config.NewConfigService()

			testConfig := map[string]interface{}{
				"device": map[string]interface{}{
					"name": "Test Device",
					"type": "switch",
				},
			}

			err := configService.SetNodeConfig(nodeCtx, testConfig)
			Expect(err).To(BeNil())

			// Verify the config was set
			data, err := configService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())
			Expect(data).To(Equal(testConfig))
		})

		It("should not allow user to set node config", func() {
			userCtx := rmngctx.NewRmngContext(user.NewUser("test-user"))
			configService := config.NewConfigService()

			testConfig := map[string]interface{}{
				"device": map[string]interface{}{
					"name": "Test Device",
					"type": "switch",
				},
			}

			err := configService.SetNodeConfig(userCtx, testConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only nodes can set their own config"))
		})
	})

	Describe("DeleteNodeConfig", func() {
		It("should allow node to delete its own config", func() {
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			configService := config.NewConfigService()

			// First set some config
			testConfig := map[string]interface{}{
				"device": map[string]interface{}{
					"name": "Test Device",
					"type": "switch",
				},
			}

			err := configService.SetNodeConfig(nodeCtx, testConfig)
			Expect(err).To(BeNil())

			// Now delete the config
			err = configService.DeleteNodeConfig(nodeCtx)
			Expect(err).To(BeNil())

			// Verify the config was deleted
			data, err := configService.Get(rmngCtx, testNodeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Node has no config"))
			Expect(data).To(BeNil())
		})

		It("should not allow user to delete node config", func() {
			userCtx := rmngctx.NewRmngContext(user.NewUser("test-user"))
			configService := config.NewConfigService()

			err := configService.DeleteNodeConfig(userCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only nodes can delete their own config"))
		})
	})
})
