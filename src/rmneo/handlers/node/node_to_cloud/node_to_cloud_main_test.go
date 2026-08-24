// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/s3util"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/schedule"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

var (
	profile    *mock.Profile
	timingFile *os.File
)

var _ = BeforeSuite(func() {
	timingFile, _ = test_utils.CreateCommonSummaryFile("publish_input_event_handler.txt")
})

var _ = AfterSuite(func() {
	if profile != nil {
		fmt.Fprintf(timingFile, "\n--- Publish Input Event (Set Node Config) ---\n")
		profile.Print(timingFile)
		fmt.Fprintf(timingFile, "-----------------------------\n\n")
	}
	timingFile.Close()
})

func sendGetGroupInfoGetResponse(thingName string, iotMock *mock.IoTDataPlaneMock, ctx context.Context) map[string]interface{} {
	event := node.PublishInputEvent{
		ThingName: thingName,
		Data: map[string]interface{}{
			"event": []interface{}{"getGroupInfo"},
		},
	}

	err := handlePublishInputEvent(ctx, event)
	Expect(err).To(BeNil())

	Expect(iotMock.PublishCalls).To(HaveLen(1))
	publishInput := iotMock.PublishCalls[0]
	Expect(*publishInput.Topic).To(Equal("rainmaker/nodes/" + thingName + "/from_cloud"))

	var publishedData map[string]interface{}
	err = json.Unmarshal(publishInput.Payload, &publishedData)
	Expect(err).To(BeNil())

	return publishedData
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

	It("returns false for a direct PublishInputEvent payload", func() {
		raw, _ := json.Marshal(node.PublishInputEvent{
			ThingName: "test-thing",
			Data:      map[string]interface{}{"event": []interface{}{"hello"}},
		})
		Expect(awscommon.IsSQSEvent(raw)).To(BeFalse())
	})

	It("returns false for malformed JSON", func() {
		Expect(awscommon.IsSQSEvent([]byte(`not valid json`))).To(BeFalse())
	})
})

// A node runs this exchange before it has any group: it connects, asks who it
// belongs to, syncs its clock and uploads its config. None of those answers
// depend on group membership, so a group-less node must be answered in full.
var _ = Describe("Publish Input Event Handler for a group-less node", func() {
	var (
		ctx       context.Context
		iotMock   *mock.IoTDataPlaneMock
		thingName string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		iotMock = mock.NewIoTDataPlaneMock()
		awscommon.SetIoTDataPlaneClient(iotMock)

		service.Initialize()
		config.Register()

		// Deliberately no ManuallyAddNodeToGroup.
		thingName = "test-thing-without-group"
	})

	send := func(events ...interface{}) map[string]interface{} {
		event := node.PublishInputEvent{
			ThingName: thingName,
			Data:      map[string]interface{}{"event": events},
		}
		Expect(handlePublishInputEvent(ctx, event)).To(BeNil())
		Expect(iotMock.PublishCalls).To(HaveLen(1))
		Expect(*iotMock.PublishCalls[0].Topic).To(Equal("rainmaker/nodes/" + thingName + "/from_cloud"))

		var published map[string]interface{}
		Expect(json.Unmarshal(iotMock.PublishCalls[0].Payload, &published)).To(BeNil())
		return published
	}

	It("answers getGroupInfo with an empty group rather than an error", func() {
		published := send("getGroupInfo")
		Expect(published).To(HaveKey("getGroupInfo"))
		// No pgrp key at all: the device reads that as "you belong to no group",
		// which is different from an absent reply (it re-asks on the next batch).
		Expect(published["getGroupInfo"]).To(BeEmpty())
	})

	It("answers getTimeSync with the server clock", func() {
		published := send("getTimeSync")
		timeSync, ok := published["getTimeSync"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		epochMillis, ok := timeSync["time"].(float64)
		Expect(ok).To(BeTrue())
		Expect(epochMillis).To(BeNumerically(">", 0))
	})

	It("accepts setNodeConfig", func() {
		event := node.PublishInputEvent{
			ThingName: thingName,
			Data: map[string]interface{}{
				"event": []interface{}{"setNodeConfig"},
				"setNodeConfig": map[string]interface{}{
					"info": map[string]interface{}{"fw_version": "1.0"},
				},
			},
		}
		Expect(handlePublishInputEvent(ctx, event)).To(BeNil())

		var published map[string]interface{}
		Expect(json.Unmarshal(iotMock.PublishCalls[0].Payload, &published)).To(BeNil())
		setNodeConfig, ok := published["setNodeConfig"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(setNodeConfig).To(HaveKeyWithValue("status", "success"))
	})

	// The batch firmware actually sends on connect. Answered in a single publish:
	// a details-dependent event must not hold the group/time answers hostage.
	It("answers every event in the real bootstrap batch", func() {
		published := send("getGroupInfo", "getAlexaEn", "getGVAEn", "getSchedVer", "getTriggerVer", "getTimeSync")
		Expect(published["event"]).To(ConsistOf(
			"getGroupInfo", "getAlexaEn", "getGVAEn", "getSchedVer", "getTriggerVer", "getTimeSync"))
		Expect(published["getGroupInfo"]).To(BeEmpty())
		Expect(published["getSchedVer"]).To(HaveKeyWithValue("version", float64(0)))
	})
})

var _ = Describe("Publish Input Event Handler", func() {
	var (
		ctx       context.Context
		iotMock   *mock.IoTDataPlaneMock
		testUser  *user.User
		groupID   string
		thingName string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		iotMock = mock.NewIoTDataPlaneMock()
		awscommon.SetIoTDataPlaneClient(iotMock)

		service.Initialize()
		schedule.Register()

		testUser = user.NewUser("test-user-id")
		rmngContext := rmngctx.NewRmngContext(testUser)
		grp, err := group.CreateGroupForUser(rmngContext, "Test Group")
		Expect(err).To(BeNil())
		groupID = grp.GroupID

		thingName = "test-thing"
		test_utils.ManuallyAddNodeToGroup(ctx, groupID, thingName)
	})

	DescribeTable("handlePublishInputEvent",
		func(inputJSON, expectedJSON string) {
			var event node.PublishInputEvent
			err := json.Unmarshal([]byte(inputJSON), &event)
			Expect(err).To(BeNil())

			if strings.Contains(inputJSON, "getSchedDetails") {
				n := node.NewNode(thingName)
				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
				nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
				scheduleData := map[string]interface{}{
					"key1": "value1",
				}
				err = nodeDetailsDB.UpdateServiceDataWithVersion(thingName, "schedule", scheduleData)
				Expect(err).To(BeNil())
			}

			err = handlePublishInputEvent(ctx, event)
			Expect(err).To(BeNil())

			Expect(iotMock.PublishCalls).To(HaveLen(1))
			publishInput := iotMock.PublishCalls[0]
			Expect(*publishInput.Topic).To(Equal("rainmaker/nodes/" + thingName + "/from_cloud"))

			var publishedData, expectedData map[string]interface{}
			err = json.Unmarshal(publishInput.Payload, &publishedData)
			Expect(err).To(BeNil())

			expectedJSON = strings.Replace(expectedJSON, "{{.GroupID}}", groupID, -1)
			err = json.Unmarshal([]byte(expectedJSON), &expectedData)
			Expect(err).To(BeNil())

			if getTimeSync, ok := publishedData["getTimeSync"].(map[string]interface{}); ok {
				Expect(getTimeSync).To(HaveKey("time"))
				serverTime, ok := getTimeSync["time"].(float64)
				Expect(ok).To(BeTrue())
				Expect(int64(serverTime)).To(BeNumerically("~", time.Now().UnixMilli(), 60_000))
				delete(publishedData, "getTimeSync")
				delete(expectedData, "getTimeSync")
			}

			if getSchedVer, ok := publishedData["getSchedVer"].(map[string]interface{}); ok {
				Expect(getSchedVer).To(HaveKey("version"))
				delete(publishedData, "getSchedVer")
				delete(expectedData, "getSchedVer")
			}

			if getSchedDetails, ok := publishedData["getSchedDetails"].(map[string]interface{}); ok {
				if _, hasVersion := getSchedDetails["version"]; hasVersion {
					Expect(getSchedDetails).To(HaveKey("version"))
					delete(getSchedDetails, "version")
					if expectedSchedDetails, ok := expectedData["getSchedDetails"].(map[string]interface{}); ok {
						delete(expectedSchedDetails, "version")
					}
				}
			}

			Expect(publishedData).To(Equal(expectedData))
		},
		Entry("getGroupInfo event",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getGroupInfo"]
			   }
			  }`,
			`{
				"event": ["getGroupInfo"],
				"getGroupInfo": {
					"pgrp": "{{.GroupID}}"
				}
			}`),
		Entry("getAlexaEn event",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getAlexaEn"]
			   }
			  }`,
			`{
				"event": ["getAlexaEn"],
				"getAlexaEn": {
					"enabled": false
				}
			}`),
		Entry("getGVAEn event",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getGVAEn"]
			   }
			  }`,
			`{
				"event": ["getGVAEn"],
				"getGVAEn": {
					"enabled": false
				}
			}`),
		Entry("hello event",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["hello"],
			   		"hello": {
			   			"id": "test-hello-id"
			   		}
			   }
			}`,
			`{
				"event": ["hello"],
				"hello": {
					"id": "test-hello-id"
				}
			}`),
		Entry("multiple events",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getGroupInfo", "hello", "getAlexaEn", "getGVAEn"],
			   		"hello": {
			   			"id": "test-hello-id"
			   		}
			   }
			}`,
			`{
				"event": ["getGroupInfo", "hello", "getAlexaEn", "getGVAEn"],
				"getGroupInfo": {
					"pgrp": "{{.GroupID}}"
				},
				"hello": {
					"id": "test-hello-id"
				},
				"getAlexaEn": {
					"enabled": false
				},
				"getGVAEn": {
					"enabled": false
				}
			}`),
		Entry("getTimeSync event",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getTimeSync"]
			   }
			  }`,
			`{
				"event": ["getTimeSync"],
				"getTimeSync": {}
			}`),
		Entry("getSchedVer event",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getSchedVer"]
			   }
			  }`,
			`{
				"event": ["getSchedVer"],
				"getSchedVer": {
					"version": 0
				}
			}`),
		Entry("getSchedDetails event with existing schedule",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getSchedDetails"]
			   }
			  }`,
			`{
				"event": ["getSchedDetails"],
				"getSchedDetails": {
					"key1": "value1"
				}
			}`),
		Entry("multiple events including schedule events",
			`{
			   "thing_name": "test-thing",
			   "data": {
			   		"event": ["getSchedVer", "getSchedDetails", "getAlexaEn", "getGVAEn"]
			   }
			}`,
			`{
				"event": ["getSchedVer", "getSchedDetails", "getAlexaEn", "getGVAEn"],
				"getSchedVer": {
					"version": 0
				},
				"getSchedDetails": {
					"key1": "value1"
				},
				"getAlexaEn": {
					"enabled": false
				},
				"getGVAEn": {
					"enabled": false
				}
			}`),
	)

	It("should handle unknown event types gracefully", func() {
		event := node.PublishInputEvent{
			ThingName: thingName,
			Data: map[string]interface{}{
				"event": []interface{}{"unknownEvent"},
			},
		}

		err := handlePublishInputEvent(ctx, event)
		Expect(err).To(BeNil())
		Expect(iotMock.PublishCalls).To(BeEmpty())
	})

	It("should handle getSchedDetails with existing schedule data", func() {
		n := node.NewNode(thingName)
		rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
		scheduleData := map[string]interface{}{
			"key1": "value1",
		}
		err := nodeDetailsDB.UpdateServiceDataWithVersion(thingName, "schedule", scheduleData)
		Expect(err).To(BeNil())

		// Now test getSchedDetails event
		event := node.PublishInputEvent{
			ThingName: thingName,
			Data: map[string]interface{}{
				"event": []interface{}{"getSchedDetails"},
			},
		}

		err = handlePublishInputEvent(ctx, event)
		Expect(err).To(BeNil())

		Expect(iotMock.PublishCalls).To(HaveLen(1))
		publishInput := iotMock.PublishCalls[0]
		Expect(*publishInput.Topic).To(Equal("rainmaker/nodes/" + thingName + "/from_cloud"))

		var publishedData map[string]interface{}
		err = json.Unmarshal(publishInput.Payload, &publishedData)
		Expect(err).To(BeNil())

		Expect(publishedData["event"]).To(Equal([]interface{}{"getSchedDetails"}))
		getSchedDetails, ok := publishedData["getSchedDetails"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(getSchedDetails).To(HaveKey("version"))

		for k, v := range scheduleData {
			Expect(getSchedDetails[k]).To(Equal(v))
		}
	})

	It("should handle getSchedDetails with no schedule data", func() {
		n := node.NewNode(thingName)
		rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
		err := nodeDetailsDB.DeleteNodeConfig()
		Expect(err).To(BeNil())

		event := node.PublishInputEvent{
			ThingName: thingName,
			Data: map[string]interface{}{
				"event": []interface{}{"getSchedDetails"},
			},
		}

		err = handlePublishInputEvent(ctx, event)
		Expect(err).To(BeNil())

		Expect(iotMock.PublishCalls).To(HaveLen(1))
		publishInput := iotMock.PublishCalls[0]
		Expect(*publishInput.Topic).To(Equal("rainmaker/nodes/" + thingName + "/from_cloud"))

		var publishedData map[string]interface{}
		err = json.Unmarshal(publishInput.Payload, &publishedData)
		Expect(err).To(BeNil())

		Expect(publishedData["event"]).To(Equal([]interface{}{"getSchedDetails"}))
		getSchedDetails, ok := publishedData["getSchedDetails"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(getSchedDetails).To(HaveKey("version"))
		Expect(len(getSchedDetails)).To(Equal(1))
	})

	It("should handle getGroupInfo for a thing not in any group", func() {
		unassociatedThingName := "unassociated-thing"
		publishedData := sendGetGroupInfoGetResponse(unassociatedThingName, iotMock, ctx)
		expectedData := map[string]interface{}{
			"event":        []interface{}{"getGroupInfo"},
			"getGroupInfo": map[string]interface{}{},
		}
		Expect(publishedData).To(Equal(expectedData))
	})

	It("should handle getGroupInfo for a thing in one or more sub-groups", func() {
		rmngContext := rmngctx.NewRmngContext(testUser)
		subGroup1, err := group.CreateSubGroup(rmngContext, groupID, "Test Sub-Group 1")
		Expect(err).To(BeNil())
		subGroup2, err := group.CreateSubGroup(rmngContext, groupID, "Test Sub-Group 2")
		Expect(err).To(BeNil())

		_, err = group.UpdateNodeAndSubgroup(rmngContext, groupID, thingName, subGroup1.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
		Expect(err).To(BeNil())
		_, err = group.UpdateNodeAndSubgroup(rmngContext, groupID, thingName, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
		Expect(err).To(BeNil())

		publishedData := sendGetGroupInfoGetResponse(thingName, iotMock, ctx)
		expectedData := map[string]interface{}{
			"event":        []interface{}{"getGroupInfo"},
			"getGroupInfo": map[string]interface{}{"pgrp": groupID, "subgrps": []interface{}{subGroup1.SubGroupID, subGroup2.SubGroupID}},
		}
		Expect(publishedData).To(Equal(expectedData))
	})

	Describe("handleSetNodeConfig", func() {
		It("should successfully set node config", func() {
			inputJSON := `{
				"thing_name": "test-thing",
				"data": {
					"event": ["setNodeConfig"],
					"setNodeConfig": {
						"key1": "value1",
						"key2": 42
					}
				}
			}`

			var event node.PublishInputEvent
			err := json.Unmarshal([]byte(inputJSON), &event)
			Expect(err).To(BeNil())

			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.ProfileReset()

			err = handlePublishInputEvent(ctx, event)
			Expect(err).To(BeNil())

			p := dbMock.ProfileGet()
			profile = &p
			readCount, writeCount := profile.TotalCounts()
			Expect(readCount).To(Equal(0))
			Expect(writeCount).To(Equal(1))

			// Verify that the configuration was stored in the database
			test_utils.AssertRowInDB(node_details_db.NodeDetailsTable, map[string]types.AttributeValue{
				"node_id": &types.AttributeValueMemberS{Value: "test-thing"},
				"config": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
					"key1": &types.AttributeValueMemberS{Value: "value1"},
					"key2": &types.AttributeValueMemberN{Value: "42"},
				}},
			})

			Expect(iotMock.PublishCalls).To(HaveLen(1))
			publishInput := iotMock.PublishCalls[0]
			Expect(*publishInput.Topic).To(Equal("rainmaker/nodes/test-thing/from_cloud"))

			var publishedData map[string]interface{}
			err = json.Unmarshal(publishInput.Payload, &publishedData)
			Expect(err).To(BeNil())

			expectedData := map[string]interface{}{
				"event": []interface{}{"setNodeConfig"},
				"setNodeConfig": map[string]interface{}{
					"status": "success",
				},
			}
			Expect(publishedData).To(Equal(expectedData))
		})

		It("should handle invalid setNodeConfig input", func() {
			inputJSON := `{
				"thing_name": "test-thing",
				"data": {
					"event": ["setNodeConfig"],
					"setNodeConfig": "invalid"
				}
			}`

			var event node.PublishInputEvent
			err := json.Unmarshal([]byte(inputJSON), &event)
			Expect(err).To(BeNil())

			err = handlePublishInputEvent(ctx, event)
			Expect(err).To(BeNil())

			Expect(iotMock.PublishCalls).To(HaveLen(0))
		})

		It("should handle multiple events including setNodeConfig", func() {
			inputJSON := `{
				"thing_name": "test-thing",
				"data": {
					"event": ["getGroupInfo", "setNodeConfig", "hello"],
					"setNodeConfig": {
						"key1": "value1",
						"key2": 42
					},
					"hello": {
						"id": "test-hello-id"
					}
				}
			}`

			var event node.PublishInputEvent
			err := json.Unmarshal([]byte(inputJSON), &event)
			Expect(err).To(BeNil())

			err = handlePublishInputEvent(ctx, event)
			Expect(err).To(BeNil())

			Expect(iotMock.PublishCalls).To(HaveLen(1))
			publishInput := iotMock.PublishCalls[0]
			Expect(*publishInput.Topic).To(Equal("rainmaker/nodes/test-thing/from_cloud"))

			var publishedData map[string]interface{}
			err = json.Unmarshal(publishInput.Payload, &publishedData)
			Expect(err).To(BeNil())

			expectedData := map[string]interface{}{
				"event": []interface{}{"getGroupInfo", "setNodeConfig", "hello"},
				"getGroupInfo": map[string]interface{}{
					"pgrp": groupID,
				},
				"setNodeConfig": map[string]interface{}{
					"status": "success",
				},
				"hello": map[string]interface{}{
					"id": "test-hello-id",
				},
			}
			Expect(publishedData).To(BeEquivalentTo(expectedData))

			// Verify that the configuration was stored in the database
			test_utils.AssertRowInDB(node_details_db.NodeDetailsTable, map[string]types.AttributeValue{
				"node_id": &types.AttributeValueMemberS{Value: "test-thing"},
				"config": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
					"key1": &types.AttributeValueMemberS{Value: "value1"},
					"key2": &types.AttributeValueMemberN{Value: "42"},
				}},
			})
		})
	})

	It("should handle multiple events including schedule events", func() {
		n := node.NewNode(thingName)
		rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
		scheduleData := map[string]interface{}{
			"key1": "value1",
		}
		err := nodeDetailsDB.UpdateServiceDataWithVersion(thingName, "schedule", scheduleData)
		Expect(err).To(BeNil())

		event := node.PublishInputEvent{
			ThingName: thingName,
			Data: map[string]interface{}{
				"event": []interface{}{"getSchedVer", "getSchedDetails", "getAlexaEn", "getGVAEn"},
			},
		}

		err = handlePublishInputEvent(ctx, event)
		Expect(err).To(BeNil())

		Expect(iotMock.PublishCalls).To(HaveLen(1))
		publishInput := iotMock.PublishCalls[0]
		Expect(*publishInput.Topic).To(Equal("rainmaker/nodes/" + thingName + "/from_cloud"))

		var publishedData map[string]interface{}
		err = json.Unmarshal(publishInput.Payload, &publishedData)
		Expect(err).To(BeNil())

		Expect(publishedData["event"]).To(Equal([]interface{}{"getSchedVer", "getSchedDetails", "getAlexaEn", "getGVAEn"}))

		getSchedVer, ok := publishedData["getSchedVer"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(getSchedVer).To(HaveKey("version"))

		getSchedDetails, ok := publishedData["getSchedDetails"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(getSchedDetails).To(HaveKey("version"))
		for k, v := range scheduleData {
			Expect(getSchedDetails[k]).To(Equal(v))
		}

		Expect(publishedData["getAlexaEn"]).To(Equal(map[string]interface{}{
			"enabled": false,
		}))
		Expect(publishedData["getGVAEn"]).To(Equal(map[string]interface{}{
			"enabled": false,
		}))
	})

	Describe("parsePublishInputEvent", func() {
		It("should parse a valid publish input event JSON", func() {
			body := `{
				"thing_name": "test-node",
				"data": {
					"event": ["getGroupInfo"]
				}
			}`

			event, err := parsePublishInputEvent(body)
			Expect(err).To(BeNil())
			Expect(event.ThingName).To(Equal("test-node"))
			Expect(event.Data).To(HaveKey("event"))
		})

		It("should fail on invalid JSON", func() {
			_, err := parsePublishInputEvent(`not valid json`)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal"))
		})

		It("should fail when thing_name is missing", func() {
			_, err := parsePublishInputEvent(`{"data": {"event": ["getGroupInfo"]}}`)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("thing_name"))
		})

		It("should fail when data is missing", func() {
			_, err := parsePublishInputEvent(`{"thing_name": "test-node"}`)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("data"))
		})
	})

	Describe("handleSQSBatch", func() {
		It("should process a batch of publish input events", func() {
			body, _ := json.Marshal(node.PublishInputEvent{
				ThingName: thingName,
				Data: map[string]interface{}{
					"event": []interface{}{"getGroupInfo"},
				},
			})

			sqsEvent := events.SQSEvent{
				Records: []events.SQSMessage{
					{MessageId: "msg-1", Body: string(body)},
				},
			}

			response, err := handleSQSBatch(ctx, sqsEvent)
			Expect(err).To(BeNil())
			Expect(response.BatchItemFailures).To(BeEmpty())

			Expect(iotMock.PublishCalls).To(HaveLen(1))
		})

		It("should report partial batch failures for invalid messages", func() {
			validBody, _ := json.Marshal(node.PublishInputEvent{
				ThingName: thingName,
				Data: map[string]interface{}{
					"event": []interface{}{"getGroupInfo"},
				},
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
			thingName2 := "test-thing-2"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, thingName2)

			body1, _ := json.Marshal(node.PublishInputEvent{
				ThingName: thingName,
				Data: map[string]interface{}{
					"event": []interface{}{"getGroupInfo"},
				},
			})
			body2, _ := json.Marshal(node.PublishInputEvent{
				ThingName: thingName2,
				Data: map[string]interface{}{
					"event": []interface{}{"getGroupInfo"},
				},
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

			Expect(iotMock.PublishCalls).To(HaveLen(2))
		})

		It("should handle hello events in batch", func() {
			body, _ := json.Marshal(node.PublishInputEvent{
				ThingName: thingName,
				Data: map[string]interface{}{
					"event": []interface{}{"hello"},
					"hello": map[string]interface{}{
						"id": "hello-id-sqs",
					},
				},
			})

			sqsEvent := events.SQSEvent{
				Records: []events.SQSMessage{
					{MessageId: "msg-hello", Body: string(body)},
				},
			}

			response, err := handleSQSBatch(ctx, sqsEvent)
			Expect(err).To(BeNil())
			Expect(response.BatchItemFailures).To(BeEmpty())

			Expect(iotMock.PublishCalls).To(HaveLen(1))
			var publishedData map[string]interface{}
			err = json.Unmarshal(iotMock.PublishCalls[0].Payload, &publishedData)
			Expect(err).To(BeNil())
			Expect(publishedData["event"]).To(ContainElement("hello"))
		})
	})
})

var _ = Describe("getServerConfig event", func() {
	var (
		ctx       context.Context
		iotMock   *mock.IoTDataPlaneMock
		s3Mock    *mock.S3ClientMock
		thingName string
		bucket    string
		key       string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		iotMock = mock.NewIoTDataPlaneMock()
		awscommon.SetIoTDataPlaneClient(iotMock)

		s3Mock = mock.NewS3ClientMock()
		awscommon.SetS3Client(s3Mock)

		service.Initialize()
		schedule.Register()

		thingName = "test-thing"
		bucket = "rmng-public-assets-" + awscommon.GetAccountId()
		key = awscommon.GetRmngRegion() + "/rmng-client-outputs.json"
		s3Mock.CreateBucketDirect(bucket)
	})

	seedServerConfig := func(body string) {
		err := s3util.PutObject(ctx, bucket, key, strings.NewReader(body))
		Expect(err).To(BeNil())
	}

	publishGetServerConfig := func() error {
		event := node.PublishInputEvent{
			ThingName: thingName,
			Data:      map[string]interface{}{"event": []interface{}{"getServerConfig"}},
		}
		return handlePublishInputEvent(ctx, event)
	}

	It("fetches the server config from the public-assets bucket and returns it from the cloud", func() {
		seedServerConfig(`{"foo": "bar", "count": 3}`)

		Expect(publishGetServerConfig()).To(BeNil())

		Expect(iotMock.PublishCalls).To(HaveLen(1))
		Expect(*iotMock.PublishCalls[0].Topic).To(Equal("rainmaker/nodes/" + thingName + "/from_cloud"))

		var publishedData map[string]interface{}
		Expect(json.Unmarshal(iotMock.PublishCalls[0].Payload, &publishedData)).To(BeNil())

		Expect(publishedData["event"]).To(ContainElement("getServerConfig"))
		outputs, ok := publishedData["getServerConfig"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(outputs["foo"]).To(Equal("bar"))
		Expect(outputs["count"]).To(BeNumerically("==", 3))
	})

	It("does not publish a from_cloud response when the server config object is missing", func() {
		// Nothing seeded — the S3 GetObject fails, the handler logs and skips.
		Expect(publishGetServerConfig()).To(BeNil())
		Expect(iotMock.PublishCalls).To(BeEmpty())
	})

	It("does not publish a from_cloud response when the stored config is malformed JSON", func() {
		seedServerConfig(`{not valid json`)

		Expect(publishGetServerConfig()).To(BeNil())
		Expect(iotMock.PublishCalls).To(BeEmpty())
	})
})

func TestPublishInputEventHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PublishInputEventHandler Suite")
}

// The reply to getSchedVer/getTriggerVer carries the version the firmware compares against its
// local set. A nil node_details row with NO error legitimately means "no config yet" and version 0
// is correct. A nil row because the READ FAILED means we do not know — and answering "version 0,
// empty" makes a device holding real schedules conclude the cloud has none and drop them.
var _ = Describe("node_to_cloud when the node_details read fails", func() {
	var (
		ctx       context.Context
		iotMock   *mock.IoTDataPlaneMock
		dbMock    *mock.DynamoDBMock
		thingName string
	)

	// publishedEvents returns the event names in the single from_cloud reply, or nil if silent.
	publishedEvents := func() []interface{} {
		if len(iotMock.PublishCalls) == 0 {
			return nil
		}
		var payload map[string]interface{}
		Expect(json.Unmarshal(iotMock.PublishCalls[0].Payload, &payload)).To(Succeed())
		evs, _ := payload["event"].([]interface{})
		return evs
	}

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		iotMock = mock.NewIoTDataPlaneMock()
		awscommon.SetIoTDataPlaneClient(iotMock)
		dbMock = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

		service.Initialize()
		schedule.Register()

		testUser := user.NewUser("test-user-id")
		grp, err := group.CreateGroupForUser(rmngctx.NewRmngContext(testUser), "Test Group")
		Expect(err).To(BeNil())

		thingName = "test-thing"
		test_utils.ManuallyAddNodeToGroup(ctx, grp.GroupID, thingName)

		// Give the node real schedule data, so "answered as empty" is distinguishable from
		// "genuinely empty" — without this the test cannot tell the bug from correct behaviour.
		n := node.NewNode(thingName)
		detailsDB := node_details_db.NewNodeDetailsDB(rmngctx.NewRmngContextWithCtx(ctx, n))
		Expect(detailsDB.UpdateServiceDataWithVersion(thingName, "schedule",
			map[string]interface{}{"Schedules": []interface{}{map[string]interface{}{"id": "1"}}})).To(Succeed())
	})

	It("leaves the details-dependent events unanswered instead of reporting an empty config", func() {
		dbMock.NextGetItemError = errors.New("ProvisionedThroughputExceededException: throttled")

		err := handlePublishInputEvent(ctx, node.PublishInputEvent{
			ThingName: thingName,
			Data: map[string]interface{}{
				"event": []interface{}{"getSchedVer", "getSchedDetails", "getGroupInfo"},
			},
		})

		Expect(err).To(HaveOccurred(), "a failed node_details read must not be reported as success")

		evs := publishedEvents()
		Expect(evs).ToNot(ContainElement("getSchedVer"),
			"answering getSchedVer after a failed read tells the device version 0, and it drops its schedules")
		Expect(evs).ToNot(ContainElement("getSchedDetails"),
			"answering getSchedDetails after a failed read sends an empty schedule set")
		Expect(evs).To(ContainElement("getGroupInfo"),
			"events that do not depend on node details should still be answered")
	})

	It("still answers version 0 when the node genuinely has no config", func() {
		// Fresh node, no node_details row, and no injected failure.
		err := handlePublishInputEvent(ctx, node.PublishInputEvent{
			ThingName: "brand-new-thing",
			Data:      map[string]interface{}{"event": []interface{}{"getSchedVer"}},
		})

		Expect(err).ToNot(HaveOccurred(), "an absent row is not an error")
		Expect(publishedEvents()).To(ContainElement("getSchedVer"),
			"a node with no config must still get an answer, or it will keep asking forever")
	})

	It("answers normally when the read succeeds", func() {
		err := handlePublishInputEvent(ctx, node.PublishInputEvent{
			ThingName: thingName,
			Data:      map[string]interface{}{"event": []interface{}{"getSchedVer", "getSchedDetails"}},
		})

		Expect(err).ToNot(HaveOccurred())
		evs := publishedEvents()
		Expect(evs).To(ContainElement("getSchedVer"))
		Expect(evs).To(ContainElement("getSchedDetails"))
	})
})
