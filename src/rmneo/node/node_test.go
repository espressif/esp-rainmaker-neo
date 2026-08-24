// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_test

import (
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NodeDetailsDB", func() {
	var (
		nodeDetailsDB         *node_details_db.NodeDetailsDB
		testNodeID            string
		testNode              *node.Node
		testUser              *user.User
		rmngContext           *rmngctx.RmngContext
		testUserContext       *rmngctx.RmngContext
		testUserNodeDetailsDB *node_details_db.NodeDetailsDB
		iotDataClient         *mock.IoTDataPlaneMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		testNodeID = "test-node-id"
		testNode = node.NewNode(testNodeID)
		rmngContext = rmngctx.NewRmngContext(testNode)
		nodeDetailsDB = node_details_db.NewNodeDetailsDB(rmngContext)

		testUser = user.NewUser("test-user-id")
		testUser.Permissions.SetAllow(utils.NodeAll.String(), testNodeID)
		testUserContext = rmngctx.NewRmngContext(testUser)
		testUserNodeDetailsDB = node_details_db.NewNodeDetailsDB(testUserContext)

		iotDataClient = awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	})

	Describe("NodeWriteShadow", func() {
		BeforeEach(func() {
			testUser.Permissions.SetAllow(utils.GroupAll.String(), "test-group-id")
			test_utils.ManuallyAddNodeToGroup(testUserContext.Context, "test-group-id", testNodeID)
		})

		It("should successfully write to the shadow", func() {
			data := node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"key1": "value1",
					"key2": 42,
				},
			}
			err := testNode.WriteToShadow(testUserContext, "reported", data)
			Expect(err).To(BeNil())

			Expect(iotDataClient.Shadows[testNodeID]).To(HaveKey("params-test-group-id"))
			Expect(iotDataClient.Shadows[testNodeID]["params-test-group-id"]).To(MatchJSON(`{"state":{"reported":{"params":{"key1":"value1","key2":42}}}}`))
		})

		It("should fail if user does not have access to the node", func() {
			data := node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"key1": "value1",
					"key2": 42,
				},
			}

			invalidUserContext := rmngctx.NewRmngContext(user.NewUser("invalid-user-id"))

			err := testNode.WriteToShadow(invalidUserContext, "reported", data)
			Expect(err).To(HaveOccurred())
			Expect(iotDataClient.Shadows[testNodeID]).To(HaveLen(0))
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("entity invalid-user-id is not authorized to perform node:writeshadow on test-node-id"))
		})
	})

	Describe("NodeWriteIndexedReportedShadow", func() {
		BeforeEach(func() {
			testUser.Permissions.SetAllow(utils.GroupAll.String(), "test-group-id")
			test_utils.ManuallyAddNodeToGroup(testUserContext.Context, "test-group-id", testNodeID)
		})

		// Ideally user should not be able to write to the indexed reported shadow directly, but as this is accessed via lambda, we are allowing it
		// TODO: Revisit this

		It("should successfully write to the shadow", func() {
			data := node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"key1": "value1",
					"key2": 42,
				},
			}
			err := testNode.WriteToIndexedReportedShadow(testUserContext, data)
			Expect(err).To(BeNil())
			Expect(iotDataClient.Shadows[testNodeID]).To(HaveKey("iparams"))
			Expect(iotDataClient.Shadows[testNodeID]["iparams"]).To(MatchJSON(`{"state":{"reported":{"params":{"key1":"value1","key2":42}}}}`))
		})

		It("should fail if user does not have access to the node", func() {
			data := node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"key1": "value1",
					"key2": 42,
				},
			}

			invalidUserContext := rmngctx.NewRmngContext(user.NewUser("invalid-user-id"))

			err := testNode.WriteToIndexedReportedShadow(invalidUserContext, data)
			Expect(err).To(HaveOccurred())
			Expect(iotDataClient.Shadows[testNodeID]).To(HaveLen(0))
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("entity invalid-user-id is not authorized to perform node:writeshadow on test-node-id"))
		})
	})

	Describe("PublishToDevice", func() {
		It("should successfully publish to the device", func() {
			err := testNode.PublishToDevice(testUserContext, "rainmaker/nodes/desired", map[string]interface{}{"key1": "value1", "key2": 42})
			Expect(err).To(BeNil())

			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotDataClient.PublishCalls).To(HaveLen(1))
			Expect(*iotDataClient.PublishCalls[0].Topic).To(Equal("rainmaker/nodes/desired"))
			Expect(iotDataClient.PublishCalls[0].Payload).To(Equal([]byte(`{"key1":"value1","key2":42}`)))

			iotDataClient.Shadows[testNodeID] = nil
		})

		It("should fail if user does not have publish to device access", func() {
			invalidUserContext := rmngctx.NewRmngContext(user.NewUser("invalid-user-id"))
			err := testNode.PublishToDevice(invalidUserContext, "desired", map[string]interface{}{"key1": "value1", "key2": 42})
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("user does not have publish to device access"))

			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotDataClient.PublishCalls).To(HaveLen(0))
		})
	})
	Describe("PutNodeConfig", func() {
		It("should successfully put a node config", func() {
			config := map[string]interface{}{
				"key1": "value1",
				"key2": 42,
			}

			err := nodeDetailsDB.UpdateServiceData("config", config)
			Expect(err).To(BeNil())

			// Validate the database entry
			test_utils.AssertRowInDB(node_details_db.NodeDetailsTable, map[string]types.AttributeValue{
				"node_id": &types.AttributeValueMemberS{Value: testNodeID},
				"config": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
					"key1": &types.AttributeValueMemberS{Value: "value1"},
					"key2": &types.AttributeValueMemberN{Value: "42"},
				}},
			})
		})

		It("should return an error when called by a non-node accessor", func() {
			nonNodeContext := rmngctx.NewRmngContext(user.NewUser("user-id"))
			service := config.NewConfigService()

			err := service.SetNodeConfig(nonNodeContext, map[string]interface{}{"key": "value"})
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("only nodes can set their own config"))
		})

		It("should successfully update Alexa enabled status", func() {
			// First put some initial config as the node
			err := nodeDetailsDB.UpdateServiceData("config", map[string]interface{}{"key": "value"})
			Expect(err).To(BeNil())

			// Update Alexa enabled status to true as a user with proper permissions
			testUser.Permissions.SetAllow(utils.NodePutConfig.String(), testNodeID)
			err = testNode.UpdateAlexaEnabled(testUserContext.Context, true)
			Expect(err).To(BeNil())

			// Verify the update
			alexaEn, err := testNode.GetAlexaEnStatus(testUserContext.Context)
			Expect(err).To(BeNil())
			Expect(alexaEn).To(Not(BeNil()))
			Expect(*alexaEn).To(BeTrue())

			// Update Alexa enabled status to false
			err = testNode.UpdateAlexaEnabled(testUserContext.Context, false)
			Expect(err).To(BeNil())

			// Verify the update
			alexaEn, err = testNode.GetAlexaEnStatus(testUserContext.Context)
			Expect(err).To(BeNil())
			Expect(alexaEn).To(Not(BeNil()))
			Expect(*alexaEn).To(BeFalse())

			// Verify original config is still preserved
			nd, err := testUserNodeDetailsDB.GetNodeDetails(testNodeID)
			Expect(err).To(BeNil())
			Expect(nd).ToNot(BeNil())
			config, err := nd.GetServiceData("config")
			Expect(err).To(BeNil())
			Expect(config).ToNot(BeNil())
			Expect(config.(map[string]interface{})["key"]).To(Equal("value"))
		})
	})

})

var _ = Describe("Node", func() {
	Describe("MigrateShadow", func() {
		var (
			testNode             *node.Node
			rmngContext          *rmngctx.RmngContext
			oldGroup             group_node_db.NodesGroups
			newGroup             group_node_db.NodesGroups
			oldGroupWithSubgroup group_node_db.NodesGroups
			oldGroupNewSubgroup  group_node_db.NodesGroups

			shadowState node.IoTNodeShadow
		)

		BeforeEach(func() {
			test_utils.TestSetup()
			testNode = node.NewNode("test-node-id")
			rmngContext = rmngctx.NewRmngContext(testNode)
			oldGroup = group_node_db.NodesGroups{Group: "grabc"}
			oldGroupWithSubgroup = group_node_db.NodesGroups{
				Group:     "grabc",
				SubGroups: []string{"pqr"},
			}
			oldGroupNewSubgroup = group_node_db.NodesGroups{
				Group:     "grabc",
				SubGroups: []string{"lmn", "xyz"},
			}

			newGroup = group_node_db.NodesGroups{Group: "grxyz"}

			// Initial shadow state
			shadowState = node.IoTNodeShadow{
				State: &node.ShadowState{
					Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"status": "online",
							"value":  42.0,
						},
					},
				},
			}
		})

		Context("Subgroup Shadow Migration", func() {
			It("should migrate shadow from old subgroup to new subgroup", func() {
				ValidateShadowMigration(testNode, oldGroupWithSubgroup, oldGroupNewSubgroup, shadowState)
			})

			It("should migrate shadow when moving from group to subgroup", func() {
				ValidateShadowMigration(testNode, oldGroup, oldGroupNewSubgroup, shadowState)
			})

			It("should migrate shadow when moving from subgroup to group", func() {
				ValidateShadowMigration(testNode, oldGroupWithSubgroup, oldGroup, shadowState)
			})

			It("should handle migration between different subgroups in same group", func() {
				test_utils.SetupShadow(testNode.GetID(), shadowState, oldGroupWithSubgroup)

				sameGroupDiffSubgroup := group_node_db.NodesGroups{
					Group:     oldGroupWithSubgroup.Group,
					SubGroups: []string{"ijk"},
				}

				// Migrate between subgroups in same group
				err := testNode.MoveShadow(rmngContext, oldGroupWithSubgroup, sameGroupDiffSubgroup)
				Expect(err).To(BeNil())

				// Verify shadow migration
				migratedState := test_utils.GetShadowForNodeGroup(testNode, sameGroupDiffSubgroup)

				// Convert floats to ints for comparison
				migratedStateConverted := test_utils.ConvertAllFloatToInt(migratedState.State)
				shadowStateConverted := test_utils.ConvertAllFloatToInt(shadowState.State)

				Expect(migratedStateConverted).To(BeEquivalentTo(shadowStateConverted))

				// Verify old shadow is deleted
				AssertShadowIsDeleted(testNode, oldGroupWithSubgroup)
			})

			It("should ensure that the order of subgroups in the shadow name is sorted", func() {
				test_utils.SetupShadow(testNode.GetID(), shadowState, oldGroupWithSubgroup)
				nodeSubGroups := group_node_db.NodesGroups{
					Group:     "grabc",
					SubGroups: []string{"pqr", "xyz", "1bc"},
				}
				shadowName := node.GetShadowNameForNodeGroups(nodeSubGroups)
				Expect(shadowName).To(Equal("params-grabc-1bc-pqr-xyz"))

				// Migrate between subgroups in same group
				err := testNode.MoveShadow(rmngContext, oldGroupWithSubgroup, nodeSubGroups)
				Expect(err).To(BeNil())

				// Verify shadow migration
				migratedState := test_utils.GetShadowForNodeGroup(testNode, nodeSubGroups)

				// Convert floats to ints for comparison
				migratedStateConverted := test_utils.ConvertAllFloatToInt(migratedState.State)
				shadowStateConverted := test_utils.ConvertAllFloatToInt(shadowState.State)

				Expect(migratedStateConverted).To(BeEquivalentTo(shadowStateConverted))

				// Verify old shadow is deleted
				AssertShadowIsDeleted(testNode, oldGroupWithSubgroup)
			})

			It("should work when old shadow doesn't exist", func() {
				// Try to migrate non-existent shadow
				nonExistentGroup := group_node_db.NodesGroups{Group: "non-existent-group"}
				err := testNode.MoveShadow(rmngContext, nonExistentGroup, oldGroupNewSubgroup)
				Expect(err).To(BeNil())
			})

			It("should handle empty shadow state", func() {
				// Set up empty shadow state
				emptyState := node.IoTNodeShadow{
					State: &node.ShadowState{}, // Empty state
				}
				test_utils.SetupShadow(testNode.GetID(), emptyState, oldGroup)

				// Migrate shadow
				err := testNode.MoveShadow(rmngContext, oldGroup, oldGroupNewSubgroup)
				Expect(err).To(BeNil())

				// Verify new shadow has empty state
				migratedState := test_utils.GetShadowForNodeGroup(testNode, oldGroupNewSubgroup)
				Expect(migratedState.State.Reported).To(BeNil())
				Expect(migratedState.State.Desired).To(BeNil())
			})

			It("should preserve only 'state' fields during migration", func() {
				// Set up complex shadow state
				complexState := node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{
								"status": "online",
								"config": map[string]interface{}{
									"brightness": 75,
									"color":      "blue",
								},
								"metrics": []interface{}{
									map[string]interface{}{
										"timestamp": 1234567890,
										"value":     42,
									},
								},
							},
						},
						Desired: &node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{
								"brightness": 80,
							},
						},
					},
					Version: utils.Ptr(5),
				}
				test_utils.SetupShadow(testNode.GetID(), complexState, oldGroup)

				// Migrate shadow
				err := testNode.MoveShadow(rmngContext, oldGroup, oldGroupNewSubgroup)
				Expect(err).To(BeNil())

				// Verify new shadow has all the content
				migratedState := test_utils.GetShadowForNodeGroup(testNode, oldGroupNewSubgroup)
				complexState.Version = nil
				Expect(test_utils.ConvertAllFloatToInt(migratedState)).To(BeEquivalentTo(test_utils.ConvertAllFloatToInt(complexState)))
			})

			It("should do nothing when shadow state is nil", func() {
				// Set up shadow with no state field
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				complexState := node.IoTNodeShadow{
					Version: utils.Ptr(5),
				}
				test_utils.SetupShadow(testNode.GetID(), complexState, oldGroup)

				// Migrate shadow
				err := testNode.MoveShadow(rmngContext, oldGroup, oldGroupNewSubgroup)
				Expect(err).To(BeNil())

				Expect(iotDataClient.Shadows[testNode.GetID()]).To(BeEmpty())
			})
		})

		It("should not migrate shadow if the group itself is changed", func() {
			test_utils.SetupShadow(testNode.GetID(), shadowState, oldGroup)
			// Migrate shadow
			err := testNode.MoveShadow(rmngContext, oldGroup, newGroup)
			Expect(err).To(BeNil())

			// Verify new shadow has no content
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			_, err = iotDataClient.GetDirect(testNode.GetID(), node.GetShadowNameForNodeGroups(newGroup))
			Expect(err).To(HaveOccurred())
			Expect(iotDataClient.Shadows[testNode.GetID()]).To(BeEmpty())

			// Verify old shadow is deleted
			_, err = iotDataClient.GetDirect(testNode.GetID(), node.GetShadowNameForNodeGroups(oldGroup))
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ReadShadow", func() {
		var (
			testNode        *node.Node
			testUser        *user.User
			testUserContext *rmngctx.RmngContext
			shadowState     node.IoTNodeShadow
		)

		BeforeEach(func() {
			test_utils.TestSetup()
			testNode = node.NewNode("test-node-id")

			testUser = user.NewUser("test-user-id")
			testUser.Permissions.SetAllow(utils.NodeAll.String(), "test-node-id")
			testUser.Permissions.SetAllow(utils.GroupAll.String(), "test-group-id")
			testUserContext = rmngctx.NewRmngContext(testUser)

			test_utils.ManuallyAddNodeToGroup(testUserContext.Context, "test-group-id", "test-node-id")

			// Initial shadow state
			shadowState = node.IoTNodeShadow{
				State: &node.ShadowState{
					Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"status": "online",
							"value":  42,
						},
					},
					Desired: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"status": "offline",
							"value":  24,
						},
					},
				},
			}
		})

		It("should successfully read from reported shadow", func() {
			test_utils.SetupShadow(testNode.GetID(), shadowState, group_node_db.NodesGroups{Group: "test-group-id"})

			reportedState, err := testNode.ReadFromReportedShadow(testUserContext)
			Expect(err).To(BeNil())
			Expect(reportedState.Params).ToNot(BeNil())
			Expect(reportedState.Params["status"]).To(Equal("online"))
			Expect(reportedState.Params["value"]).To(Equal(float64(42.0)))
		})

		It("should successfully read from desired shadow", func() {
			test_utils.SetupShadow(testNode.GetID(), shadowState, group_node_db.NodesGroups{Group: "test-group-id"})

			desiredState, err := testNode.ReadFromDesiredShadow(testUserContext)
			Expect(err).To(BeNil())
			Expect(desiredState.Params).ToNot(BeNil())
			Expect(desiredState.Params["status"]).To(Equal("offline"))
			Expect(desiredState.Params["value"]).To(Equal(float64(24.0)))
		})

		It("should return empty struct when shadow does not exist", func() {
			reportedState, err := testNode.ReadFromReportedShadow(testUserContext)
			Expect(err).To(BeNil())
			Expect(reportedState).To(Equal(node.ReportedOrDesiredShadow{}))

			desiredState, err := testNode.ReadFromDesiredShadow(testUserContext)
			Expect(err).To(BeNil())
			Expect(desiredState).To(Equal(node.ReportedOrDesiredShadow{}))
		})

		It("should fail if user does not have read access to the node", func() {
			test_utils.SetupShadow(testNode.GetID(), shadowState, group_node_db.NodesGroups{Group: "test-group-id"})

			invalidUserContext := rmngctx.NewRmngContext(user.NewUser("invalid-user-id"))

			_, err := testNode.ReadFromReportedShadow(invalidUserContext)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("entity invalid-user-id is not authorized to perform node:readshadow on test-node-id"))
		})

		It("should return empty struct when target state does not exist", func() {
			emptyState := node.IoTNodeShadow{
				State: &node.ShadowState{}, // Empty state
			}
			test_utils.SetupShadow(testNode.GetID(), emptyState, group_node_db.NodesGroups{Group: "test-group-id"})

			reportedState, err := testNode.ReadFromReportedShadow(testUserContext)
			Expect(err).To(BeNil())
			Expect(reportedState).To(Equal(node.ReportedOrDesiredShadow{}))
		})

		It("should return empty struct when state field is missing", func() {
			invalidState := node.IoTNodeShadow{
				// State field intentionally omitted
			}
			test_utils.SetupShadow(testNode.GetID(), invalidState, group_node_db.NodesGroups{Group: "test-group-id"})

			reportedState, err := testNode.ReadFromReportedShadow(testUserContext)
			Expect(err).To(BeNil())
			Expect(reportedState).To(Equal(node.ReportedOrDesiredShadow{}))
		})
	})

	Describe("RegisterInIotCore", func() {
		var (
			testNode        *node.Node
			testUserContext *rmngctx.RmngContext
			nodeCert        string
			iotClient       *mock.IoTClientMock
		)

		BeforeEach(func() {
			test_utils.TestSetup()
			testNode = node.NewNode("test-node")
			testUserContext = rmngctx.NewRmngContext(user.NewUser("test-user-id"))
			testUserContext.SetAllow(utils.NodeAdminAdd, "*") //Just for testing
			nodeCert = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqwwwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MTFaFw0yNjA1\nMjAwOTI1MTFaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBALV+wQl/PUFuiX9BgSKcuRLy7aw67/Z98KJ9jBsozaBm\nQEkqJMCrEjYK8zsnDoVsXbbbj2qEE3w4LuYyhcNdDcDIzq+l68qOB5PUqTtkMlR1\n0v6LNFdOtNBYCJvINILem+cvqybrMfHrR3nF33cg9kvshoIVcEUpnPk9vqic8Px7\n2KMnaIUgvRg6tRqr8Xt3ou4RNgLUUYUpBc2jBYM+mKlL+RDCXmDWNXugDHvQbqto\nq39b3yemQfi92LStKrRv1ivBlp6vhZbDzqv4lFcawxWNV+UHLU2T/basPOCqLRZ5\n24DbHSk/edSj9b+/X729ZXOGuWArFDNT1VFDXLln7FcCAwEAAaNCMEAwHQYDVR0O\nBBYEFGAxuVICNSz/0iQNS7lsZNs+0eHTMB8GA1UdIwQYMBaAFHZpcCukxQSAVo2M\nJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQBfzHCP0YcT0c1mH2DOp4Orw9we\n029sEHLHtQK2Gq+iu2VAK8/FgwIrTVfTMQGQnedu10DoETDWvmGrOphdC+YeWnZY\ntcH/LyOmdSRZF7YuQr9I328pVQ8ECDPteoVBq8QwrYNsSRx+MIDOHgPGBNswpL5h\nP1GFsviy9SDqVzfffHUfQEiTKU4la4cLRU8oPtTmbgCx+d0FlfBkz2IvaJyhoMKd\nx0bV/MLeLnbghiyGZ9oqsLLO5dZSA8wtV2DLvxHz4feRqlHfN9Fm9dWGvx4lJ/fA\n7tPMKuBTfPEKB9IpG/BtVNOk4G7+058t5s6GotJNm2zCpp3Nc3BQrBzqdR+Y\n-----END CERTIFICATE-----"
			iotClient = awscommon.GetIoTClient().(*mock.IoTClientMock)
		})

		Context("Basic tests", func() {
			It("should successfully register a node in IoT Core", func() {
				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(BeNil())
				Expect(nodeIDRegistered).To(Equal(testNode.GetID()))

				// Verify the thing was created
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeTrue())

				// Verify certificate registration and activation
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeTrue())
				Expect(iotClient.VerifyCertificateActive(nodeCert)).To(BeTrue())

				// Verify the certificate is attached to the thing
				thing, exists := iotClient.GetThingDirect(testNode.GetID())
				Expect(exists).To(BeTrue())
				Expect(len(thing.CertificateIds)).To(BeNumerically(">", 0))
			})

			It("should reject a duplicate registration", func() {
				// First registration
				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(BeNil())
				Expect(nodeIDRegistered).To(Equal(testNode.GetID()))

				// Second registration with same certificate. The node id still
				// comes back alongside the error so callers can report it.
				nodeIDRegistered, err = node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(MatchError(node.ErrNodeAlreadyRegistered))
				Expect(nodeIDRegistered).To(Equal(testNode.GetID()))

				// The rejected call must leave the live node untouched.
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeTrue())
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeTrue())
				Expect(iotClient.VerifyCertificateActive(nodeCert)).To(BeTrue())
				thing, exists := iotClient.GetThingDirect(testNode.GetID())
				Expect(exists).To(BeTrue())
				Expect(len(thing.CertificateIds)).To(BeNumerically(">", 0))
			})

			It("should reject a duplicate before touching IoT Core", func() {
				_, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(BeNil())

				// Drop the Thing so any IoT work in the second attempt is visible:
				// the duplicate is caught from the node_details row alone, so the
				// Thing must still be missing afterwards.
				delete(iotClient.GetThingsDirect(), testNode.GetID())
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeFalse())

				_, err = node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(MatchError(node.ErrNodeAlreadyRegistered))
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeFalse(),
					"a duplicate must not re-provision the node in IoT Core")
			})

			It("should handle invalid certificate format", func() {
				invalidCert := "invalid-certificate-format"
				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, invalidCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(HaveOccurred())
				Expect(nodeIDRegistered).To(Equal(""))

				// Verify nothing was registered
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeFalse())
				Expect(iotClient.VerifyCertificateExists(invalidCert)).To(BeFalse())
			})
		})

		Context("Idempotency Tests", func() {
			It("should be idempotent at certificate registration stage", func() {
				// Pre-stage: cert is already registered from a previous (partial)
				// attempt, but Thing/attachment/policy were never set up.
				_, err := iotutil.RegisterCertificate(testUserContext, nodeCert)
				Expect(err).To(BeNil())
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeTrue())

				// Full registration should now recover the ARN of the existing
				// cert and complete the rest of the flow rather than failing.
				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(BeNil())
				Expect(nodeIDRegistered).To(Equal(testNode.GetID()))

				// Verify the Thing was created and the cert is attached to it
				// — the gap that the old early-return path left behind.
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeTrue())
				thing, exists := iotClient.GetThingDirect(testNode.GetID())
				Expect(exists).To(BeTrue())
				Expect(len(thing.CertificateIds)).To(BeNumerically(">", 0))
			})

			It("should be idempotent at thing creation stage", func() {
				// First create the thing
				err := iotutil.CreateThing(testUserContext, testNode.GetID())
				Expect(err).To(BeNil())

				// Full registration should still work
				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(BeNil())
				Expect(nodeIDRegistered).To(Equal(testNode.GetID()))

				// Verify everything is registered correctly
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeTrue())
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeTrue())
			})

			It("should be idempotent at policy attachment stage", func() {

				err := iotutil.CreateThing(testUserContext, testNode.GetID())
				Expect(err).To(BeNil())

				certID, err := iotutil.GetCertIDFromPEM(nodeCert)
				Expect(err).To(BeNil())

				// Attach policy
				err = iotutil.AttachDefaultPolicy(testUserContext, certID, nil)
				Expect(err).To(BeNil())

				// Full registration should still work
				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(BeNil())
				Expect(nodeIDRegistered).To(Equal(testNode.GetID()))

				// Verify everything is registered correctly
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeTrue())
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeTrue())
			})
		})

		Context("Rollback Scenarios", func() {
			It("should rollback certificate registration if thing creation fails", func() {
				// Make thing creation fail
				iotClient.ForceThingCreationError = true

				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(HaveOccurred())
				Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("failed to create thing"))
				Expect(nodeIDRegistered).To(Equal("test-node"))

				// Verify certificate was rolled back
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeFalse())
			})

			It("should rollback thing and certificate if policy attachment fails", func() {
				// Make policy attachment fail
				iotClient.ForcePolicyAttachmentError = true

				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(HaveOccurred())
				Expect(nodeIDRegistered).To(Equal("test-node"))

				// Verify both thing and certificate were rolled back
				thing, exists := iotClient.GetThingDirect(testNode.GetID())
				Expect(exists).To(BeFalse())
				Expect(thing).To(BeNil())

				cert, exists := iotClient.GetCertificateDirect(testNode.GetID())
				Expect(exists).To(BeFalse())
				Expect(cert).To(BeNil())
			})

			It("should rollback everything if certificate attachment fails", func() {
				// Force certificate attachment to fail
				iotClient.ForceCertAttachmentError = true

				// Try full registration which should fail at attachment
				nodeIDRegistered, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", []string{}, []string{}, "test-user-id", nil)
				Expect(err).To(HaveOccurred())
				Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("failed to attach certificate to thing"))
				Expect(nodeIDRegistered).To(Equal("test-node"))

				// Verify both thing and certificate were rolled back
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeFalse())
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeFalse())
			})
		})

		Context("Node-register hook (OnNodeRegister)", func() {
			It("invokes the node-register hook with the node's capabilities", func() {
				lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)

				_, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", nil, nil, "test-user-id", []string{"camera"})
				Expect(err).To(BeNil())

				events := hookInvocations(lambdaMock)
				Expect(events).To(HaveLen(1))
				Expect(events[0].NodeID).To(Equal(testNode.GetID()))
				Expect(events[0].Capabilities).To(Equal([]string{"camera"}))
				Expect(events[0].CertArn).NotTo(BeEmpty())
			})

			It("does not invoke the hook when no capabilities are requested", func() {
				lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)

				_, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", nil, nil, "test-user-id", nil)
				Expect(err).To(BeNil())
				Expect(hookInvocations(lambdaMock)).To(BeEmpty())
			})

			It("fails registration and rolls back when the hook errors", func() {
				lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
				// A non-not-found error from the hook Lambda must abort registration
				// (a not-found is the "hook not deployed" no-op and is handled
				// separately by lambdautil.InvokeSync).
				lambdaMock.InvokeError = rmerror.NewRMError(nil, "hook unavailable")

				_, err := node.RegisterNodeInRmng(testUserContext, nodeCert, "", nil, nil, "test-user-id", []string{"camera"})
				Expect(err).To(HaveOccurred())
				Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("node-register hook failed"))

				// The deferred rollback removed the half-provisioned thing and cert.
				Expect(iotClient.VerifyThingExists(testNode.GetID())).To(BeFalse())
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeFalse())
			})
		})
	})

	Describe("AddToAdminGroups", func() {
		var (
			testNode        *node.Node
			testUserContext *rmngctx.RmngContext
			iotClient       *mock.IoTClientMock
		)

		BeforeEach(func() {
			test_utils.TestSetup()
			testNode = node.NewNode("test-node-id")
			testUserContext = rmngctx.NewRmngContext(user.NewUser("test-user-id"))
			iotClient = awscommon.GetIoTClient().(*mock.IoTClientMock)

			// Create the thing first since it's required for group operations
			err := iotutil.CreateThing(testUserContext, testNode.GetID())
			Expect(err).To(BeNil())
		})

		Context("Basic tests", func() {
			It("should successfully add node to thing groups", func() {
				groupNames := []string{"Group1", "Group2"}
				err := testNode.AddToAdminGroups(testUserContext, groupNames)
				Expect(err).To(BeNil())

				// Verify groups were created and node was added
				for _, groupName := range groupNames {
					Expect(iotClient.VerifyThingInGroup(testNode.GetID(), groupName)).To(BeTrue())
				}
			})

			It("should handle adding to existing groups", func() {
				// Create a group first
				existingGroup := "ExistingGroup"
				err := iotutil.CreateThingGroup(testUserContext, existingGroup, "")
				Expect(err).To(BeNil())

				// Add node to the existing group
				err = testNode.AddToAdminGroups(testUserContext, []string{existingGroup})
				Expect(err).To(BeNil())

				// Verify node was added to the group
				Expect(iotClient.VerifyThingInGroup(testNode.GetID(), existingGroup)).To(BeTrue())
			})

			It("should handle empty group list", func() {
				err := testNode.AddToAdminGroups(testUserContext, []string{})
				Expect(err).To(BeNil())
			})

			It("should handle adding to multiple groups at once", func() {
				groupNames := []string{"Group1", "Group2", "Group3"}
				err := testNode.AddToAdminGroups(testUserContext, groupNames)
				Expect(err).To(BeNil())

				// Verify node was added to all groups
				for _, groupName := range groupNames {
					Expect(iotClient.VerifyThingInGroup(testNode.GetID(), groupName)).To(BeTrue())
				}
			})
		})

		Context("Error Handling and Rollback", func() {
			It("should handle idempotent group addition", func() {
				// First add to groups
				err := testNode.AddToAdminGroups(testUserContext, []string{"Group1"})
				Expect(err).To(BeNil())

				// Second add should succeed
				err = testNode.AddToAdminGroups(testUserContext, []string{"Group1"})
				Expect(err).To(BeNil())

				// Verify thing is in group exactly once
				groups := iotClient.GetThingGroupsDirect(testNode.GetID())
				Expect(groups).To(HaveLen(1))
				Expect(groups).To(ContainElement("Group1"))
			})

			It("should handle non-existent thing", func() {
				// Try to add non-existent thing to group
				nonExistentNode := node.NewNode("non-existent")
				err := nonExistentNode.AddToAdminGroups(testUserContext, []string{"Group1"})
				Expect(err).To(HaveOccurred())
				Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("failed to add thing to group"))
			})

			It("should rollback all groups on partial failure", func() {
				// Make adding to second group fail
				iotClient.ForceGroupAdditionError = true

				err := testNode.AddToAdminGroups(testUserContext, []string{"Group1", "InvalidGroup"})
				Expect(err).To(HaveOccurred())

				// Verify thing is not in any groups
				groups := iotClient.GetThingGroupsDirect(testNode.GetID())
				Expect(groups).To(BeEmpty())
			})

			It("should create new groups if they do not exist (flat)", func() {
				groupNames := []string{"AdminGroup1", "AdminGroup2"}
				err := node.CreateAdminGroupIfNotExists(testUserContext, groupNames, "")
				Expect(err).To(BeNil())
				for _, group := range groupNames {
					_, exists := iotClient.ThingGroups[group]
					Expect(exists).To(BeTrue())
				}
			})

			It("should not fail if groups already exist (idempotent, flat)", func() {
				groupNames := []string{"AdminGroup1", "AdminGroup2"}
				_ = node.CreateAdminGroupIfNotExists(testUserContext, groupNames, "")
				err := node.CreateAdminGroupIfNotExists(testUserContext, groupNames, "")
				Expect(err).To(BeNil())
			})

			It("should return error if group creation fails", func() {
				iotClient.ForceGroupCreationError = true

				err := node.CreateAdminGroupIfNotExists(testUserContext, []string{"ErrGroup"}, "")
				Expect(err).To(HaveOccurred())
			})

			It("should create parent and child groups when neither exist", func() {
				err := node.CreateAdminGroupIfNotExists(testUserContext, []string{"ChildGroup"}, "ParentGroup")
				Expect(err).To(BeNil())
				_, parentExists := iotClient.ThingGroups["ParentGroup"]
				Expect(parentExists).To(BeTrue())
				_, childExists := iotClient.ThingGroups["ChildGroup"]
				Expect(childExists).To(BeTrue())
				Expect(iotClient.ThingGroupParents["ChildGroup"]).To(Equal("ParentGroup"))
			})

			It("should create child under existing parent", func() {
				// Pre-create parent
				_ = node.CreateAdminGroupIfNotExists(testUserContext, []string{"ExistingParent"}, "")
				err := node.CreateAdminGroupIfNotExists(testUserContext, []string{"NewChild"}, "ExistingParent")
				Expect(err).To(BeNil())
				Expect(iotClient.ThingGroupParents["NewChild"]).To(Equal("ExistingParent"))
			})

			It("should succeed if group exists with correct parent", func() {
				_ = node.CreateAdminGroupIfNotExists(testUserContext, []string{"Child1"}, "Parent1")
				err := node.CreateAdminGroupIfNotExists(testUserContext, []string{"Child1"}, "Parent1")
				Expect(err).To(BeNil())
			})

			It("should fail if group exists under different parent", func() {
				_ = node.CreateAdminGroupIfNotExists(testUserContext, []string{"Child2"}, "ParentA")
				err := node.CreateAdminGroupIfNotExists(testUserContext, []string{"Child2"}, "ParentB")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("parent mismatch"))
			})

			It("should fail if standalone group is requested under a parent", func() {
				// Create a flat group (no parent)
				_ = node.CreateAdminGroupIfNotExists(testUserContext, []string{"StandaloneGroup"}, "")
				// Now request it under a parent — should fail because it already exists without a parent
				err := node.CreateAdminGroupIfNotExists(testUserContext, []string{"StandaloneGroup"}, "NewParent")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("parent mismatch"))
			})
		})
	})

	Describe("UpdateNodeInRmng", func() {
		var (
			testCtx  *rmngctx.RmngContext
			nodeCert string
		)

		BeforeEach(func() {
			test_utils.TestSetup()
			testCtx = rmngctx.NewRmngContext(user.NewUser("test-admin"))
			// Update path needs registration permissions for the seed and
			// update permissions for the operations under test.
			testCtx.SetAllow(utils.NodeAdminAdd, "*")
			testCtx.SetAllow(utils.NodeAll, "*")
			testCtx.SetAllow(utils.NodeWriteShadow, "*")
			nodeCert = "-----BEGIN CERTIFICATE-----\nMIIC+TCCAeGgAwIBAgIUEvfpBfOpYVx91uR1w6QHRx6WqwwwDQYJKoZIhvcNAQEL\nBQAwFTETMBEGA1UEAwwKTXkgUm9vdCBDQTAeFw0yNTA1MjAwOTI1MTFaFw0yNjA1\nMjAwOTI1MTFaMBQxEjAQBgNVBAMMCXRlc3Qtbm9kZTCCASIwDQYJKoZIhvcNAQEB\nBQADggEPADCCAQoCggEBALV+wQl/PUFuiX9BgSKcuRLy7aw67/Z98KJ9jBsozaBm\nQEkqJMCrEjYK8zsnDoVsXbbbj2qEE3w4LuYyhcNdDcDIzq+l68qOB5PUqTtkMlR1\n0v6LNFdOtNBYCJvINILem+cvqybrMfHrR3nF33cg9kvshoIVcEUpnPk9vqic8Px7\n2KMnaIUgvRg6tRqr8Xt3ou4RNgLUUYUpBc2jBYM+mKlL+RDCXmDWNXugDHvQbqto\nq39b3yemQfi92LStKrRv1ivBlp6vhZbDzqv4lFcawxWNV+UHLU2T/basPOCqLRZ5\n24DbHSk/edSj9b+/X729ZXOGuWArFDNT1VFDXLln7FcCAwEAAaNCMEAwHQYDVR0O\nBBYEFGAxuVICNSz/0iQNS7lsZNs+0eHTMB8GA1UdIwQYMBaAFHZpcCukxQSAVo2M\nJlVEAmCoYHjvMA0GCSqGSIb3DQEBCwUAA4IBAQBfzHCP0YcT0c1mH2DOp4Orw9we\n029sEHLHtQK2Gq+iu2VAK8/FgwIrTVfTMQGQnedu10DoETDWvmGrOphdC+YeWnZY\ntcH/LyOmdSRZF7YuQr9I328pVQ8ECDPteoVBq8QwrYNsSRx+MIDOHgPGBNswpL5h\nP1GFsviy9SDqVzfffHUfQEiTKU4la4cLRU8oPtTmbgCx+d0FlfBkz2IvaJyhoMKd\nx0bV/MLeLnbghiyGZ9oqsLLO5dZSA8wtV2DLvxHz4feRqlHfN9Fm9dWGvx4lJ/fA\n7tPMKuBTfPEKB9IpG/BtVNOk4G7+058t5s6GotJNm2zCpp3Nc3BQrBzqdR+Y\n-----END CERTIFICATE-----"
		})

		It("returns ErrNodeNotFound when the node is not registered", func() {
			err := node.UpdateNodeInRmng(testCtx, "no-such-node", "", []string{"GroupA"}, []string{"env:prod"}, nil)
			Expect(err).To(MatchError(node.ErrNodeNotFound))
		})

		It("applies tags and admin groups to an already-registered node", func() {
			// Pre-register the node so node_details exists.
			nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", []string{}, []string{}, "test-admin", nil)
			Expect(err).To(BeNil())

			err = node.UpdateNodeInRmng(testCtx, nodeID, "", []string{"UpdatedGroup"}, []string{"env:staging"}, nil)
			Expect(err).To(BeNil())

			iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
			Expect(iotClient.VerifyThingInGroup(nodeID, "UpdatedGroup")).To(BeTrue())

			iotData := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			Expect(iotData.VerifyTags(nodeID, map[string]string{"env": "staging"}, nil, nil)).To(BeTrue())
		})

		It("is idempotent — repeating an update is a no-op", func() {
			nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", []string{}, []string{}, "test-admin", nil)
			Expect(err).To(BeNil())

			Expect(node.UpdateNodeInRmng(testCtx, nodeID, "", []string{"GroupX"}, []string{"k:v"}, nil)).To(BeNil())
			Expect(node.UpdateNodeInRmng(testCtx, nodeID, "", []string{"GroupX"}, []string{"k:v"}, nil)).To(BeNil())
		})

		It("accepts empty inputs (zero-row update is allowed)", func() {
			nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", []string{}, []string{}, "test-admin", nil)
			Expect(err).To(BeNil())

			Expect(node.UpdateNodeInRmng(testCtx, nodeID, "", nil, nil, nil)).To(BeNil())
		})

		Context("cert update path", func() {
			// Different cert (CN "bulknode1") used as the replacement; the
			// cert CN doesn't have to match the Thing name — we're updating
			// the cert binding, not the Thing.
			replacementCert := "-----BEGIN CERTIFICATE-----\nMIIDCTCCAfGgAwIBAgIUJxFymgxSNmN/Y1VA1xpjsfyE+P8wDQYJKoZIhvcNAQEL\nBQAwFDESMBAGA1UEAwwJYnVsa25vZGUxMB4XDTI1MDYyMDE3MDA0N1oXDTI2MDYy\nMDE3MDA0N1owFDESMBAGA1UEAwwJYnVsa25vZGUxMIIBIjANBgkqhkiG9w0BAQEF\nAAOCAQ8AMIIBCgKCAQEAxkOoaj9mf4bw7N9SV1zHvgtvszvauaay+k1eSeqbgOde\nfu0qwSZ8BLNtMstibHOwmpS4OPoxbW5KhyoBRdhcO2wUamEk6UdapXcOJiKa+u7I\n3AcpqMe5i3WVSAFttotfSeI0nTqAGPkTZOrDqZCwp2Hg+m6SFH2i1efXRYyMlGBP\nmU8B4HC84HoM19EJw4CIMUIUWR8WEugvHuaf5ano00lGr6QoHsgCWNyj533KgQ4A\nNwdqQ0h1gnv+Bdz/mCZ+FmveUn1jFfRokbceZqxaMmm5BN9cEmv2abpZgC9A5If6\n3rcv59aSLAIn/Sj/x1N9G9d/IyKQbwkKw1zquuyUdQIDAQABo1MwUTAdBgNVHQ4E\nFgQU4aX1iM6cEWbWl+Aua5WxJyaj9YswHwYDVR0jBBgwFoAU4aX1iM6cEWbWl+Au\na5WxJyaj9YswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAVrsy\n8Fdeds02qFb5A5r2usNfSw3c30qIDtv0HSgfd5lVpvG1p5CE+ziyWtBuwxkzwEdE\n4JPJmXX5bQGyrZKlkD60K3kHq+Ed2hakiLJjB15DqNTk8pKTHjfYA0/mfXZKUtqP\nc03a8DPqjfbncHUOIyUaVr+o8O5dZIouGx9M84/RDbbemPjHAapshDNejLLA/gzT\n7/PGRQ7lGRlHu3NgIaoLAE0Q4Uwj7cycNugfQCnQF8nWSZyR192gh00alLb38p98\nE8tvw9wWDS8RMYV8KTP2nuB59OWxUSoNGtY+dnb88tLIiUE7odMjaBQY8id+Pg/9\n/fz2Ocw6G8/996nJdw==\n-----END CERTIFICATE-----"

			It("replaces the existing cert with a new one and deactivates the old", func() {
				nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", nil, nil, "test-admin", nil)
				Expect(err).To(BeNil())

				iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
				Expect(iotClient.VerifyCertificateExists(nodeCert)).To(BeTrue())
				Expect(iotClient.VerifyCertificateActive(nodeCert)).To(BeTrue())

				err = node.UpdateNodeInRmng(testCtx, nodeID, replacementCert, nil, nil, nil)
				Expect(err).To(BeNil())

				// New cert is registered, attached, and active.
				Expect(iotClient.VerifyCertificateExists(replacementCert)).To(BeTrue())
				Expect(iotClient.VerifyCertificateActive(replacementCert)).To(BeTrue())
				thing, exists := iotClient.GetThingDirect(nodeID)
				Expect(exists).To(BeTrue())
				newCertID, _ := iotutil.GetCertIDFromPEM(replacementCert)
				Expect(thing.CertificateIds).To(ContainElement(newCertID))

				// Old cert is detached from the Thing AND deactivated.
				oldCertID, _ := iotutil.GetCertIDFromPEM(nodeCert)
				Expect(thing.CertificateIds).NotTo(ContainElement(oldCertID))
				Expect(iotClient.VerifyCertificateActive(nodeCert)).To(BeFalse())
			})

			It("is a no-op when the supplied cert is already attached", func() {
				nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", nil, nil, "test-admin", nil)
				Expect(err).To(BeNil())

				// Re-submit the same cert as a "replacement".
				err = node.UpdateNodeInRmng(testCtx, nodeID, nodeCert, nil, nil, nil)
				Expect(err).To(BeNil())

				iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
				Expect(iotClient.VerifyCertificateActive(nodeCert)).To(BeTrue())
				thing, exists := iotClient.GetThingDirect(nodeID)
				Expect(exists).To(BeTrue())
				certID, _ := iotutil.GetCertIDFromPEM(nodeCert)
				Expect(thing.CertificateIds).To(ContainElement(certID))
				Expect(thing.CertificateIds).To(HaveLen(1))
			})

			It("is idempotent across repeated cert-update calls", func() {
				nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", nil, nil, "test-admin", nil)
				Expect(err).To(BeNil())

				Expect(node.UpdateNodeInRmng(testCtx, nodeID, replacementCert, nil, nil, nil)).To(BeNil())
				Expect(node.UpdateNodeInRmng(testCtx, nodeID, replacementCert, nil, nil, nil)).To(BeNil())

				iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
				Expect(iotClient.VerifyCertificateActive(replacementCert)).To(BeTrue())
				Expect(iotClient.VerifyCertificateActive(nodeCert)).To(BeFalse())
			})

			It("returns ErrNodeNotFound when a cert is supplied but the node is missing", func() {
				err := node.UpdateNodeInRmng(testCtx, "no-such-node", replacementCert, nil, nil, nil)
				Expect(err).To(MatchError(node.ErrNodeNotFound))
			})

			It("rejects a malformed cert PEM with a clear reason", func() {
				nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", nil, nil, "test-admin", nil)
				Expect(err).To(BeNil())

				err = node.UpdateNodeInRmng(testCtx, nodeID, "not-a-pem", nil, nil, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("update node certificate"))
			})

			It("combines cert update with metadata updates in one call", func() {
				nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", nil, nil, "test-admin", nil)
				Expect(err).To(BeNil())

				err = node.UpdateNodeInRmng(testCtx, nodeID, replacementCert, []string{"NewGroup"}, []string{"env:replaced"}, nil)
				Expect(err).To(BeNil())

				iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
				Expect(iotClient.VerifyCertificateActive(replacementCert)).To(BeTrue())
				Expect(iotClient.VerifyCertificateActive(nodeCert)).To(BeFalse())
				Expect(iotClient.VerifyThingInGroup(nodeID, "NewGroup")).To(BeTrue())

				iotData := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				Expect(iotData.VerifyTags(nodeID, map[string]string{"env": "replaced"}, nil, nil)).To(BeTrue())
			})

			It("fires the node-register hook with capabilities on a cert update (re-claim)", func() {
				lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)

				// Plain registration (no capabilities) must not fire the hook.
				nodeID, err := node.RegisterNodeInRmng(testCtx, nodeCert, "", nil, nil, "test-admin", nil)
				Expect(err).To(BeNil())
				Expect(hookInvocations(lambdaMock)).To(BeEmpty())

				// A re-claim replaces the cert and re-supplies the claim's
				// capabilities, so the hook must fire again with the NEW cert ARN —
				// otherwise the capability policies would silently not be re-attached
				// to the replacement cert (see spec 3.6).
				err = node.UpdateNodeInRmng(testCtx, nodeID, replacementCert, nil, nil, []string{"camera"})
				Expect(err).To(BeNil())

				events := hookInvocations(lambdaMock)
				Expect(events).To(HaveLen(1))
				Expect(events[0].NodeID).To(Equal(nodeID))
				Expect(events[0].Capabilities).To(Equal([]string{"camera"}))
				Expect(events[0].CertArn).NotTo(BeEmpty())
			})
		})
	})
})

// hookInvocations returns the decoded payloads of every node-register hook
// (OnNodeRegister) invocation the Lambda mock recorded, so a test can assert
// the hook fired with the expected node, capabilities, and cert.
func hookInvocations(lm *mock.LambdaMock) []nodelifecycle.NodeRegisterEvent {
	var events []nodelifecycle.NodeRegisterEvent
	for _, call := range lm.InvokeCalls {
		if call.FunctionName == nil || *call.FunctionName != nodelifecycle.OnNodeRegisterFunctionName {
			continue
		}
		var e nodelifecycle.NodeRegisterEvent
		Expect(json.Unmarshal(call.Payload, &e)).To(Succeed())
		events = append(events, e)
	}
	return events
}

func AssertShadowIsDeleted(testNode *node.Node, groups group_node_db.NodesGroups) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	_, err := iotDataClient.GetDirect(testNode.GetID(), node.GetShadowNameForNodeGroups(groups))
	Expect(err).To(HaveOccurred())
}

func ValidateShadowMigration(testNode *node.Node, oldGroups group_node_db.NodesGroups, newGroups group_node_db.NodesGroups, shadowState node.IoTNodeShadow) {
	rmngContext := rmngctx.NewRmngContext(testNode)
	test_utils.SetupShadow(testNode.GetID(), shadowState, oldGroups)
	// Migrate shadow
	err := testNode.MoveShadow(rmngContext, oldGroups, newGroups)
	Expect(err).To(BeNil())

	// Verify new shadow has the content
	migratedState := test_utils.GetShadowForNodeGroup(testNode, newGroups)

	// Use ConvertAllFloatToInt to normalize data types for comparison
	migratedStateConverted := test_utils.ConvertAllFloatToInt(migratedState.State)
	shadowStateConverted := test_utils.ConvertAllFloatToInt(shadowState.State)

	Expect(migratedStateConverted).To(BeEquivalentTo(shadowStateConverted))

	// Verify old shadow is deleted
	AssertShadowIsDeleted(testNode, oldGroups)
}

// AWS IoT identity-policy hard size limit (post-substitution, UTF-8 bytes).
// See: https://docs.aws.amazon.com/general/latest/gr/iot-core.html#iot-policy-limits
const iotPolicyDocumentSizeLimit = 2048

var _ = Describe("Node Policy tests", func() {
	// node_policy.json is the document loaded by src/rmng_base_stack.py and
	// attached to every thing.

	// If this test fails, a topic ARN was added without budgeting for it: either
	// collapse ARNs (e.g. with a trailing `*`) or remove unused statements. Or
	// create another policy.
	It("fits within the AWS IoT 2048-byte policy size limit", func() {
		const (
			region    = "ap-southeast-1" // 14 chars, longest current commercial region
			accountID = "123456789012"   // 12 digits, fixed
		)

		raw, err := os.ReadFile("node_policy.json")
		Expect(err).NotTo(HaveOccurred(), "read node_policy.json")

		rendered := strings.NewReplacer(
			"__REGION__", region,
			"__ACCOUNT__", accountID,
		).Replace(string(raw))

		Expect(rendered).NotTo(ContainSubstring("__REGION__"),
			"unsubstituted region placeholder remained after rendering")
		Expect(rendered).NotTo(ContainSubstring("__ACCOUNT__"),
			"unsubstituted account placeholder remained after rendering")

		var doc interface{}
		Expect(json.Unmarshal([]byte(rendered), &doc)).To(Succeed(),
			"rendered policy is not valid JSON")

		// Re-marshal to compact form to match what CDK uploads to AWS.
		compact, err := json.Marshal(doc)
		Expect(err).NotTo(HaveOccurred(), "re-marshal failed")

		size := len(compact)
		headroom := iotPolicyDocumentSizeLimit - size
		GinkgoWriter.Printf("node_policy.json: %d/%d bytes (%d bytes free)\n",
			size, iotPolicyDocumentSizeLimit, headroom)

		Expect(size).To(BeNumerically("<=", iotPolicyDocumentSizeLimit),
			"node_policy.json exceeds AWS IoT %d-byte limit", iotPolicyDocumentSizeLimit)
	})
})
