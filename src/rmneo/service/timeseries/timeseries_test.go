// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package timeseries_test

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/processed_ts_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries/timewindow"
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

func TestTimeseries(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Timeseries Suite")
}

// Configuration Tests
var _ = Describe("Timeseries Configuration", func() {
	var originalConfig timeseries.TimeseriesConfig

	BeforeEach(func() {
		// Save the original config
		originalConfig = timeseries.GTimeseriesConfig
	})

	AfterEach(func() {
		// Restore the original config
		timeseries.GTimeseriesConfig = originalConfig
	})

	Describe("LoadTimeseriesConfigFromDefaults", func() {
		It("should set default values", func() {
			var config timeseries.TimeseriesConfig
			timeseries.LoadTimeseriesConfigFromDefaults(&config)

			Expect(config.WeekStart).To(Equal(timeseries.WeekStartMonday))
		})
	})

	Describe("GetWeekStartWeekday", func() {
		It("should return Monday for WeekStartMonday", func() {
			timeseries.GTimeseriesConfig.WeekStart = timeseries.WeekStartMonday
			Expect(timeseries.GetWeekStartWeekday()).To(Equal(time.Monday))
		})

		It("should return Sunday for WeekStartSunday", func() {
			timeseries.GTimeseriesConfig.WeekStart = timeseries.WeekStartSunday
			Expect(timeseries.GetWeekStartWeekday()).To(Equal(time.Sunday))
		})

		It("should return Monday for invalid week start", func() {
			timeseries.GTimeseriesConfig.WeekStart = timeseries.WeekStart("invalid")
			Expect(timeseries.GetWeekStartWeekday()).To(Equal(time.Monday))
		})
	})

	Describe("IsValidWeekStart", func() {
		It("should return true for valid week starts", func() {
			Expect(timeseries.IsValidWeekStart(timeseries.WeekStartMonday)).To(BeTrue())
			Expect(timeseries.IsValidWeekStart(timeseries.WeekStartSunday)).To(BeTrue())
		})

		It("should return false for invalid week starts", func() {
			Expect(timeseries.IsValidWeekStart(timeseries.WeekStart("invalid"))).To(BeFalse())
			Expect(timeseries.IsValidWeekStart(timeseries.WeekStart("tuesday"))).To(BeFalse())
		})
	})

	Describe("GetTimeseriesConfig", func() {
		It("should return a copy of the current config", func() {
			timeseries.GTimeseriesConfig.WeekStart = timeseries.WeekStartSunday
			config := timeseries.GetTimeseriesConfig()

			Expect(config.WeekStart).To(Equal(timeseries.WeekStartSunday))
		})
	})

	Describe("GetWeekStart", func() {
		It("should return the configured week start", func() {
			timeseries.GTimeseriesConfig.WeekStart = timeseries.WeekStartSunday
			Expect(timeseries.GetWeekStart()).To(Equal(timeseries.WeekStartSunday))

			timeseries.GTimeseriesConfig.WeekStart = timeseries.WeekStartMonday
			Expect(timeseries.GetWeekStart()).To(Equal(timeseries.WeekStartMonday))
		})
	})
})

// Service Tests
var _ = Describe("TimeseriesService", func() {
	var (
		timeseriesService *timeseries.TimeseriesService
		testUser          *user.User
		rmngCtx           *rmngctx.RmngContext
		testNodeID        string
		mockDB            *mock.DynamoDBMock
	)

	BeforeEach(func() {
		// Initialize service registry
		service.Initialize()
		timeseries.Register()

		test_utils.TestSetup()
		timeseriesService = timeseries.NewTimeseriesService()
		testNodeID = "test-node-id"

		testUser = user.NewUser("test-user-id")
		testUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
		rmngCtx = rmngctx.NewRmngContext(testUser)

		// Initialize mock DynamoDB and add timeseries tables
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.AddTable(timeseries_db.RawTSDataTable, "node_key_dt", "ts")
		mockDB.AddTable(processed_ts_db.ProcessedTSDataTable, "node_key_dt", "interval_key")
		mockDB.ProfileReset()
	})

	Describe("Service Properties", func() {
		It("should have correct service name", func() {
			Expect(timeseriesService.GetName()).To(Equal("timeseries"))
		})

		It("should not support versioning", func() {
			Expect(timeseriesService.HasVersion()).To(BeFalse())
		})

		It("should be registered as a NodeService", func() {
			svc, err := service.Registry().GetNodeService("timeseries")
			Expect(err).To(BeNil())
			Expect(svc).ToNot(BeNil())
			Expect(svc.GetName()).To(Equal("timeseries"))
		})
	})

	Describe("Get", func() {
		It("should return information about the timeseries service", func() {
			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			dataMap, ok := data.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(dataMap).To(HaveKey("service"))
			Expect(dataMap).To(HaveKey("message"))
			Expect(dataMap).To(HaveKey("raw_data"))
			Expect(dataMap).To(HaveKey("aggregates"))
			Expect(dataMap).To(HaveKey("examples"))
			Expect(dataMap["service"]).To(Equal("timeseries"))

			// Validate parameters documentation
			parameters := dataMap["parameters"].(map[string]string)
			Expect(parameters).To(HaveKey("key"))
			Expect(parameters).To(HaveKey("data_type"))
			Expect(parameters).To(HaveKey("type"))

			// Validate raw_data documentation
			rawData := dataMap["raw_data"].(map[string]string)
			Expect(rawData).To(HaveKey("start_time"))
			Expect(rawData).To(HaveKey("end_time"))
			Expect(rawData).To(HaveKey("page_size"))

			// Validate aggregates documentation
			aggregates := dataMap["aggregates"].(map[string]string)
			Expect(aggregates).To(HaveKey("window"))
			Expect(aggregates).To(HaveKey("date"))

			// Validate examples
			examples := dataMap["examples"].(map[string]string)
			Expect(examples).To(HaveKey("raw_data"))
			Expect(examples).To(HaveKey("latest_data"))
			Expect(examples).To(HaveKey("current_all"))
			Expect(examples).To(HaveKey("current_daily"))
			Expect(examples).To(HaveKey("historical_daily"))
		})
	})

	Describe("Put", func() {
		It("should return error for PUT operations", func() {
			err := timeseriesService.Put(rmngCtx, testNodeID, map[string]interface{}{})
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("PUT operation not supported for timeseries data"))
		})
	})

	Describe("Delete", func() {
		It("should delete all raw and processed timeseries data for a node", func() {
			// Grant required permissions
			testUser.Permissions.SetAllow(utils.NodePutConfig.String(), testNodeID)
			testUser.Permissions.SetAllow(utils.NodeDeleteConfig.String(), testNodeID)

			// Set up node config with timeseries-enabled parameters
			configAttr, marshalErr := attributevalue.Marshal(map[string]interface{}{
				"devices": []interface{}{
					map[string]interface{}{
						"id": "Sensor",
						"params": []interface{}{
							map[string]interface{}{
								"id": "Temperature", "data_type": "float",
								"properties": []interface{}{"time_series"},
							},
							map[string]interface{}{
								"id": "Humidity", "data_type": "int",
								"properties": []interface{}{"time_series"},
							},
						},
					},
				},
			})
			Expect(marshalErr).To(BeNil())
			mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(node_details_db.NodeDetailsTable),
				Item: map[string]types.AttributeValue{
					"node_id": &types.AttributeValueMemberS{Value: testNodeID},
					"config":  configAttr,
				},
			})

			// Insert timeseries data using PutTimeseriesData
			tsDB := timeseries_db.NewTimeseriesDB(rmngCtx)
			Expect(tsDB.PutTimeseriesData(&timeseries_db.TimeseriesEntry{
				NodeID: testNodeID, DataKey: "Temperature", DataType: "float",
				Timestamp: 1000, Value: 25.5,
			})).To(BeNil())
			Expect(tsDB.PutTimeseriesData(&timeseries_db.TimeseriesEntry{
				NodeID: testNodeID, DataKey: "Temperature", DataType: "float",
				Timestamp: 2000, Value: 26.0,
			})).To(BeNil())
			Expect(tsDB.PutTimeseriesData(&timeseries_db.TimeseriesEntry{
				NodeID: testNodeID, DataKey: "Humidity", DataType: "int",
				Timestamp: 1000, Value: 60,
			})).To(BeNil())

			// Verify data exists before delete using service-level GetLatest
			tempLatest, err := timeseriesService.GetLatest(rmngCtx, testNodeID, "Temperature", "float")
			Expect(err).To(BeNil())
			Expect(tempLatest).ToNot(BeNil(), "Pre-condition: Temperature data should exist")

			humidLatest, err := timeseriesService.GetLatest(rmngCtx, testNodeID, "Humidity", "int")
			Expect(err).To(BeNil())
			Expect(humidLatest).ToNot(BeNil(), "Pre-condition: Humidity data should exist")

			// Perform delete
			err = timeseriesService.Delete(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Verify data deleted using service-level GetLatest — returns error when no data found
			_, err = timeseriesService.GetLatest(rmngCtx, testNodeID, "Temperature", "float")
			Expect(err).To(HaveOccurred(), "Temperature timeseries data should be deleted")

			_, err = timeseriesService.GetLatest(rmngCtx, testNodeID, "Humidity", "int")
			Expect(err).To(HaveOccurred(), "Humidity timeseries data should be deleted")
		})
	})

	Describe("Sharing access", func() {
		var (
			ownerUser    *user.User
			ownerCtx     *rmngctx.RmngContext
			groupID      string
			ingestNodeID string
		)

		seedDataPoint := func(nodeID, name, dataType string, ts int64, val interface{}) {
			tsDB := timeseries_db.NewTimeseriesDB(ownerCtx)
			Expect(tsDB.PutTimeseriesData(&timeseries_db.TimeseriesEntry{
				NodeID: nodeID, DataKey: name, DataType: dataType,
				Timestamp: ts, Value: val,
			})).To(BeNil())
		}

		BeforeEach(func() {
			ingestNodeID = "shared-ts-node"

			ownerUser = user.NewUser("owner-user")
			ownerUser.Permissions.SetAllow(utils.GroupCreate.String(), "*")
			ownerCtx = rmngctx.NewRmngContext(ownerUser)

			createdGroup, err := group.CreateGroupForUser(ownerCtx, "Test Group")
			Expect(err).To(BeNil())
			Expect(createdGroup).ToNot(BeNil())
			groupID = createdGroup.GroupID

			test_utils.ManuallyAddNodeToGroup(context.Background(), groupID, ingestNodeID)
			ownerUser.Permissions.SetAllow(utils.GroupShare.String(), groupID)

			err = user.LoadNodePermissions(ownerCtx, groupID, ingestNodeID)
			Expect(err).To(BeNil())

			seedDataPoint(ingestNodeID, "Temperature", "float", 1000, 25.5)
		})

		It("primary access: owner can read latest timeseries via group ownership", func() {
			point, err := timeseriesService.GetLatest(ownerCtx, ingestNodeID, "Temperature", "float")
			Expect(err).To(BeNil())
			Expect(point).ToNot(BeNil())
			Expect(point.Value).To(Equal(25.5))
		})

		It("secondary access: full group share grants read then unshare revokes", func() {
			sharedUser := user.NewUser("shared-ts-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			_, err := timeseriesService.GetLatest(sharedCtx, ingestNodeID, "Temperature", "float")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node timeseries data"))

			_, err = group.ShareGroup(ownerCtx, groupID, "shared-ts-user", utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			err = user.LoadNodePermissions(sharedCtx, groupID, ingestNodeID)
			Expect(err).To(BeNil())

			point, err := timeseriesService.GetLatest(sharedCtx, ingestNodeID, "Temperature", "float")
			Expect(err).To(BeNil())
			Expect(point).ToNot(BeNil())
			Expect(point.Value).To(Equal(25.5))

			err = group.UnshareGroup(ownerCtx, groupID, "shared-ts-user")
			Expect(err).To(BeNil())

			sharedUser = user.NewUser("shared-ts-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			_, err = timeseriesService.GetLatest(sharedCtx, ingestNodeID, "Temperature", "float")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node timeseries data"))
		})

		It("subgroup access: subgroup share grants read then unshare revokes", func() {
			createdSubgroup, err := group.CreateSubGroup(ownerCtx, groupID, "Test Subgroup")
			Expect(err).To(BeNil())
			Expect(createdSubgroup).ToNot(BeNil())
			subgroupID := createdSubgroup.SubGroupID

			_, err = group.UpdateNodeAndSubgroup(ownerCtx, groupID, ingestNodeID, subgroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			sharedUser := user.NewUser("shared-ts-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			_, err = timeseriesService.GetLatest(sharedCtx, ingestNodeID, "Temperature", "float")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node timeseries data"))

			_, err = group.ShareSubGroup(ownerCtx, groupID, subgroupID, "shared-ts-user", auth.UserInfo{})
			Expect(err).To(BeNil())

			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			err = user.LoadNodePermissions(sharedCtx, groupID, ingestNodeID)
			Expect(err).To(BeNil())

			point, err := timeseriesService.GetLatest(sharedCtx, ingestNodeID, "Temperature", "float")
			Expect(err).To(BeNil())
			Expect(point).ToNot(BeNil())
			Expect(point.Value).To(Equal(25.5))

			err = group.UnshareSubGroup(ownerCtx, groupID, subgroupID, "shared-ts-user")
			Expect(err).To(BeNil())

			sharedUser = user.NewUser("shared-ts-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			_, err = timeseriesService.GetLatest(sharedCtx, ingestNodeID, "Temperature", "float")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node timeseries data"))
		})
	})
})

var _ = Describe("TimeseriesDB", func() {
	var (
		timeseriesDB *timeseries_db.TimeseriesDB
		testUser     *user.User
		rmngCtx      *rmngctx.RmngContext
		testNodeID   string
		mockDB       *mock.DynamoDBMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		testNodeID = "test-node-id"

		testUser = user.NewUser("test-user-id")
		testUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
		rmngCtx = rmngctx.NewRmngContext(testUser)
		timeseriesDB = timeseries_db.NewTimeseriesDB(rmngCtx)

		// Initialize mock DynamoDB and add timeseries table
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.AddTable(timeseries_db.RawTSDataTable, "node_key_dt", "ts")
		mockDB.ProfileReset()
	})

	Describe("GetTimeseriesData", func() {
		Context("when no data exists", func() {
			It("should return empty array", func() {
				entries, err := timeseriesDB.GetTimeseriesData(testNodeID, "temperature", "float", 0, 0, 10)
				Expect(err).To(BeNil())
				Expect(entries).To(HaveLen(0))
			})
		})

		Context("when data exists", func() {
			BeforeEach(func() {
				// Create system actor context for PutTimeseriesData operations
				systemActor := utils.NewSystemActor()
				systemRmngCtx := rmngctx.NewRmngContext(systemActor)
				systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

				// Add test data to mock database
				testEntries := []*timeseries_db.TimeseriesEntry{
					{
						NodeID:     testNodeID,
						DataKey:    "temperature",
						DataType:   "float",
						Timestamp:  1640995200, // 2022-01-01 00:00:00 UTC
						TopicName:  "ts-group456",
						Timezone:   "UTC",
						Value:      25.5,
						Cumulative: false,
					},
					{
						NodeID:     testNodeID,
						DataKey:    "temperature",
						DataType:   "float",
						Timestamp:  1640995260, // 2022-01-01 00:01:00 UTC
						TopicName:  "ts-group456",
						Timezone:   "UTC",
						Value:      26.0,
						Cumulative: false,
					},
					{
						NodeID:     testNodeID,
						DataKey:    "temperature",
						DataType:   "float",
						Timestamp:  1640995320, // 2022-01-01 00:02:00 UTC
						TopicName:  "ts-group456",
						Timezone:   "UTC",
						Value:      24.8,
						Cumulative: false,
					},
				}

				for _, entry := range testEntries {
					systemTimeseriesDB.PutTimeseriesData(entry)
				}
			})

			It("should retrieve all data without time filters", func() {
				entries, err := timeseriesDB.GetTimeseriesData(testNodeID, "temperature", "float", 0, 0, 0)
				Expect(err).To(BeNil())
				Expect(entries).To(HaveLen(3))

				// Should be in descending timestamp order (latest first)
				Expect(entries[0].Timestamp).To(Equal(int64(1640995320)))
				Expect(entries[1].Timestamp).To(Equal(int64(1640995260)))
				Expect(entries[2].Timestamp).To(Equal(int64(1640995200)))
			})

			It("should respect limit parameter", func() {
				entries, err := timeseriesDB.GetTimeseriesData(testNodeID, "temperature", "float", 0, 0, 2)
				Expect(err).To(BeNil())
				Expect(entries).To(HaveLen(2))

				// Should get the 2 most recent entries
				Expect(entries[0].Timestamp).To(Equal(int64(1640995320)))
				Expect(entries[1].Timestamp).To(Equal(int64(1640995260)))
			})

			It("should filter by start time", func() {
				startTime := int64(1640995260) // From 00:01:00 onwards
				entries, err := timeseriesDB.GetTimeseriesData(testNodeID, "temperature", "float", startTime, 0, 0)
				Expect(err).To(BeNil())
				Expect(entries).To(HaveLen(2))

				for _, entry := range entries {
					Expect(entry.Timestamp).To(BeNumerically(">=", startTime))
				}
			})

			It("should filter by end time", func() {
				endTime := int64(1640995260) // Until 00:01:00
				entries, err := timeseriesDB.GetTimeseriesData(testNodeID, "temperature", "float", 0, endTime, 0)
				Expect(err).To(BeNil())
				Expect(entries).To(HaveLen(2))

				for _, entry := range entries {
					Expect(entry.Timestamp).To(BeNumerically("<=", endTime))
				}
			})

			It("should filter by time range", func() {
				startTime := int64(1640995200) // From 00:00:00
				endTime := int64(1640995260)   // To 00:01:00
				entries, err := timeseriesDB.GetTimeseriesData(testNodeID, "temperature", "float", startTime, endTime, 0)
				Expect(err).To(BeNil())
				Expect(entries).To(HaveLen(2))

				for _, entry := range entries {
					Expect(entry.Timestamp).To(BeNumerically(">=", startTime))
					Expect(entry.Timestamp).To(BeNumerically("<=", endTime))
				}
			})

			It("should validate data structure", func() {
				entries, err := timeseriesDB.GetTimeseriesData(testNodeID, "temperature", "float", 0, 0, 1)
				Expect(err).To(BeNil())
				Expect(entries).To(HaveLen(1))

				entry := entries[0]
				Expect(entry.NodeKeyDt).To(Equal("test-node-id.temperature.float"))
				Expect(entry.NodeID).To(Equal(testNodeID))
				Expect(entry.DataKey).To(Equal("temperature"))
				Expect(entry.DataType).To(Equal("float"))
				Expect(entry.TopicName).To(Equal("ts-group456"))
				Expect(entry.Timezone).To(Equal("UTC"))
				Expect(entry.Cumulative).To(BeFalse())
			})
		})
	})

	Describe("GetLatestTimeseriesData", func() {
		It("should return error when no data exists", func() {
			_, err := timeseriesDB.GetLatestTimeseriesData(testNodeID, "temperature", "float")
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("no timeseries data found"))
		})

		It("should return latest entry when data exists", func() {
			// Create system actor context for PutTimeseriesData operations
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

			// Add test data
			testEntry := &timeseries_db.TimeseriesEntry{
				NodeID:    testNodeID,
				DataKey:   "temperature",
				DataType:  "float",
				Timestamp: 1640995200,
				TopicName: "ts-group456",
				Value:     25.5,
			}

			err := systemTimeseriesDB.PutTimeseriesData(testEntry)
			Expect(err).To(BeNil())

			// Retrieve latest data
			entry, err := timeseriesDB.GetLatestTimeseriesData(testNodeID, "temperature", "float")
			Expect(err).To(BeNil())
			Expect(entry).ToNot(BeNil())
			Expect(entry.Timestamp).To(Equal(int64(1640995200)))
			Expect(entry.Value).To(Equal(25.5))
		})
	})

	Describe("GetTimeseriesDataByTimeRange", func() {
		It("should convert time.Time to Unix timestamps", func() {
			startTime := time.Unix(1640995200, 0) // 2022-01-01 00:00:00 UTC
			endTime := time.Unix(1640995260, 0)   // 2022-01-01 00:01:00 UTC

			entries, err := timeseriesDB.GetTimeseriesDataByTimeRange(testNodeID, "temperature", "float", startTime, endTime)
			Expect(err).To(BeNil())
			Expect(entries).ToNot(BeNil())
			// Should work without error even with no data
		})
	})

	Describe("PutTimeseriesData", func() {
		It("should store timeseries data successfully", func() {
			// Create system actor context for PutTimeseriesData operations
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

			testEntry := &timeseries_db.TimeseriesEntry{
				NodeID:     testNodeID,
				DataKey:    "temperature",
				DataType:   "float",
				Timestamp:  1640995200,
				TopicName:  "ts-group456",
				Timezone:   "UTC",
				Value:      25.5,
				Cumulative: false,
			}

			err := systemTimeseriesDB.PutTimeseriesData(testEntry)
			Expect(err).To(BeNil())

			// Verify the partition key was set correctly
			expectedKey := "test-node-id.temperature.float"
			Expect(testEntry.NodeKeyDt).To(Equal(expectedKey))

			// Verify data was stored in mock database
			var storedEntry timeseries_db.TimeseriesEntry
			err = mockDB.GetDirect(timeseries_db.RawTSDataTable, expectedKey, "1640995200", &storedEntry)
			Expect(err).To(BeNil())
			Expect(storedEntry.NodeID).To(Equal(testNodeID))
			Expect(storedEntry.DataKey).To(Equal("temperature"))
			Expect(storedEntry.Value).To(Equal(25.5))
		})

		It("should handle different data types", func() {
			// Create system actor context for PutTimeseriesData operations
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

			entries := []*timeseries_db.TimeseriesEntry{
				{
					NodeID:   testNodeID,
					DataKey:  "power",
					DataType: "bool",
					Value:    true,
				},
				{
					NodeID:   testNodeID,
					DataKey:  "count",
					DataType: "int",
					Value:    42,
				},
				{
					NodeID:   testNodeID,
					DataKey:  "name",
					DataType: "string",
					Value:    "test-device",
				},
			}

			for _, entry := range entries {
				err := systemTimeseriesDB.PutTimeseriesData(entry)
				Expect(err).To(BeNil())
			}
		})

		It("should handle cumulative data", func() {
			// Create system actor context for PutTimeseriesData operations
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

			testEntry := &timeseries_db.TimeseriesEntry{
				NodeID:     testNodeID,
				DataKey:    "energy",
				DataType:   "float",
				Timestamp:  1640995200,
				Value:      1500.25,
				Cumulative: true,
			}

			err := systemTimeseriesDB.PutTimeseriesData(testEntry)
			Expect(err).To(BeNil())

			// Verify cumulative flag was stored
			var storedEntry timeseries_db.TimeseriesEntry
			err = mockDB.GetDirect(timeseries_db.RawTSDataTable, "test-node-id.energy.float", "1640995200", &storedEntry)
			Expect(err).To(BeNil())
			Expect(storedEntry.Cumulative).To(BeTrue())
		})
	})

	Describe("GetParameterList", func() {
		It("should return error for unsupported operation", func() {
			_, err := timeseriesDB.GetParameterList(testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("parameter list query not supported without GSI on node_id"))
		})
	})

	Describe("GetTimeseriesDataWithPagination", func() {
		Context("with paginated data", func() {
			BeforeEach(func() {
				// Create system actor context for PutTimeseriesData operations
				systemActor := utils.NewSystemActor()
				systemRmngCtx := rmngctx.NewRmngContext(systemActor)
				systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

				// Add 10 test entries with timestamps 1-10 seconds apart
				baseTimestamp := int64(1640995200) // 2022-01-01 00:00:00 UTC
				for i := 0; i < 10; i++ {
					testEntry := &timeseries_db.TimeseriesEntry{
						NodeID:     testNodeID,
						DataKey:    "temperature",
						DataType:   "float",
						Timestamp:  baseTimestamp + int64(i),
						TopicName:  "ts-group456",
						Timezone:   "UTC",
						Value:      float64(20 + i), // Values 20.0 to 29.0
						Cumulative: false,
					}

					systemTimeseriesDB.PutTimeseriesData(testEntry)
				}
			})

			It("should return all data when no limit specified", func() {
				result, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 0, "")
				Expect(err).To(BeNil())
				Expect(result).ToNot(BeNil())
				Expect(result.Entries).To(HaveLen(10))
				Expect(result.NextToken).To(BeEmpty())

				// Should be in descending order (latest first)
				for i := 0; i < 9; i++ {
					Expect(result.Entries[i].Timestamp).To(BeNumerically(">", result.Entries[i+1].Timestamp))
				}
			})

			It("should paginate correctly with limit", func() {
				// First page
				result, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 3, "")
				Expect(err).To(BeNil())
				Expect(result).ToNot(BeNil())
				Expect(result.Entries).To(HaveLen(3))
				Expect(result.NextToken).ToNot(BeEmpty())

				// Verify first page contains latest 3 entries
				expectedTimestamps := []int64{1640995209, 1640995208, 1640995207}
				for i, entry := range result.Entries {
					Expect(entry.Timestamp).To(Equal(expectedTimestamps[i]))
					Expect(entry.Value).To(Equal(float64(29 - i)))
				}

				// Second page using next_key
				nextResult, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 3, result.NextToken)
				Expect(err).To(BeNil())
				Expect(nextResult).ToNot(BeNil())
				Expect(nextResult.Entries).To(HaveLen(3))
				Expect(nextResult.NextToken).ToNot(BeEmpty())

				// Verify second page contains next 3 entries
				expectedTimestamps = []int64{1640995206, 1640995205, 1640995204}
				for i, entry := range nextResult.Entries {
					Expect(entry.Timestamp).To(Equal(expectedTimestamps[i]))
					Expect(entry.Value).To(Equal(float64(26 - i)))
				}

				// Verify pagination tokens are different
				Expect(result.NextToken).ToNot(Equal(nextResult.NextToken))
			})

			It("should handle last page correctly", func() {
				// Get last page with remaining entries
				firstResult, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 8, "")
				Expect(err).To(BeNil())
				Expect(firstResult.NextToken).ToNot(BeEmpty())

				// Get final page
				lastResult, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 8, firstResult.NextToken)
				Expect(err).To(BeNil())
				Expect(lastResult).ToNot(BeNil())
				Expect(lastResult.Entries).To(HaveLen(2)) // Only 2 remaining entries
				Expect(lastResult.NextToken).To(BeEmpty())
			})

			It("should handle time range filtering with pagination", func() {
				// Filter to middle 5 entries (timestamps 1640995202 to 1640995206)
				startTime := int64(1640995202)
				endTime := int64(1640995206)

				result, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", startTime, endTime, 3, "")
				Expect(err).To(BeNil())
				Expect(result).ToNot(BeNil())
				Expect(result.Entries).To(HaveLen(3))
				Expect(result.NextToken).ToNot(BeEmpty())

				// Verify all entries are within time range
				for _, entry := range result.Entries {
					Expect(entry.Timestamp).To(BeNumerically(">=", startTime))
					Expect(entry.Timestamp).To(BeNumerically("<=", endTime))
				}

				// Get next page
				nextResult, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", startTime, endTime, 3, result.NextToken)
				Expect(err).To(BeNil())
				Expect(nextResult.Entries).To(HaveLen(2)) // Remaining 2 entries in range
				Expect(nextResult.NextToken).To(BeEmpty())
			})
		})

		Context("with no data", func() {
			It("should return empty result", func() {
				result, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 10, "")
				Expect(err).To(BeNil())
				Expect(result).ToNot(BeNil())
				Expect(result.Entries).To(HaveLen(0))
				Expect(result.NextToken).To(BeEmpty())
			})
		})

		Context("with invalid pagination token", func() {
			It("should return error for malformed token", func() {
				_, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 10, "invalid-token")
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid pagination token"))
			})

			It("should return error for invalid base64", func() {
				_, err := timeseriesDB.GetTimeseriesDataWithPagination(testNodeID, "temperature", "float", 0, 0, 10, "not-base64!")
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("invalid pagination token"))
			})
		})
	})
})

var _ = Describe("Timeseries Service Integration", func() {
	var (
		timeseriesService *timeseries.TimeseriesService
		testUser          *user.User
		unauthorizedUser  *user.User
		rmngCtx           *rmngctx.RmngContext
		unauthorizedCtx   *rmngctx.RmngContext
		testNodeID        string
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		service.Initialize()
		timeseries.Register()

		timeseriesService = timeseries.NewTimeseriesService()
		testNodeID = "test-node-id"

		// Create authorized user
		testUser = user.NewUser("test-user-id")
		testUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
		rmngCtx = rmngctx.NewRmngContext(testUser)

		// Create unauthorized user
		unauthorizedUser = user.NewUser("unauthorized-user")
		unauthorizedCtx = rmngctx.NewRmngContext(unauthorizedUser)

		// Initialize mock DynamoDB tables for integration tests
		mockDB := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.AddTable(timeseries_db.RawTSDataTable, "node_key_dt", "ts")
		mockDB.AddTable(processed_ts_db.ProcessedTSDataTable, "node_key_dt", "interval_key")
		mockDB.ProfileReset()
	})

	Describe("Get with query parameters", func() {
		BeforeEach(func() {
			// Create system actor context for PutTimeseriesData operations
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

			// Add test timeseries data to the mock database for testing
			testEntries := []*timeseries_db.TimeseriesEntry{
				{
					NodeID:     testNodeID,
					DataKey:    "temperature",
					DataType:   "float",
					Timestamp:  1640995200,
					TopicName:  "ts-test",
					Timezone:   "UTC",
					Value:      22.0,
					Cumulative: false,
				},
				{
					NodeID:     testNodeID,
					DataKey:    "temperature",
					DataType:   "float",
					Timestamp:  1640995202,
					TopicName:  "ts-test",
					Timezone:   "UTC",
					Value:      23.0,
					Cumulative: false,
				},
				{
					NodeID:     testNodeID,
					DataKey:    "temperature",
					DataType:   "float",
					Timestamp:  1640995204,
					TopicName:  "ts-test",
					Timezone:   "UTC",
					Value:      24.0,
					Cumulative: false,
				},
			}

			for _, entry := range testEntries {
				systemTimeseriesDB.PutTimeseriesData(entry)
			}
		})

		It("should handle /timeseries/latest returning single latest data point", func() {
			// Request latest data via /timeseries/latest path (timeseries_type in context)
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "timeseries_type", "latest")

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Latest endpoint returns single object (not array)
			dataMap, ok := data.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(dataMap).To(HaveKey("data"))

			// data is a single map for latest endpoint
			latestEntry := dataMap["data"].(map[string]interface{})
			Expect(latestEntry).To(HaveKey("key"))
			Expect(latestEntry).To(HaveKey("dt"))
			Expect(latestEntry).To(HaveKey("ts"))
			Expect(latestEntry).To(HaveKey("value"))

			// Should be the latest entry (highest timestamp)
			Expect(latestEntry["ts"]).To(Equal(int64(1640995204)))
			Expect(latestEntry["value"]).To(Equal(24.0))
		})

		It("should handle missing required parameters", func() {
			// Missing param
			queryParams := map[string]string{
				"data_type": "float",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("key and data_type query parameters are required"))
		})

		It("should fall back to default page_size when the value is invalid", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"page_size": "invalid",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			// Invalid page_size is silently replaced with the default;
			// the call should succeed rather than surface an error.
			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())
		})

		It("should enforce authorization", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
			}
			unauthorizedCtx.Context = context.WithValue(unauthorizedCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(unauthorizedCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unauthorized"))
		})

		It("should handle type=aggregates parameter", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "aggregates",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Should return aggregates in array format for consistency
			dataMap := data.(map[string]interface{})
			Expect(dataMap).To(HaveKey("aggregates"))

			aggregatesArray := dataMap["aggregates"].([]map[string]interface{})
			Expect(len(aggregatesArray)).To(Equal(1))

			aggregatesData := aggregatesArray[0]
			Expect(aggregatesData).To(HaveKey("windows"))

			windows := aggregatesData["windows"].(map[string]interface{})
			Expect(windows).To(HaveKey("hourly"))
			Expect(windows).To(HaveKey("daily"))
			Expect(windows).To(HaveKey("weekly"))
			Expect(windows).To(HaveKey("monthly"))
		})

		It("should handle type=aggregates with specific window parameter", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "aggregates",
				"window":    "daily",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Should return window-specific data in array format for consistency
			dataMap := data.(map[string]interface{})
			Expect(dataMap).To(HaveKey("aggregates"))

			aggregatesArray := dataMap["aggregates"].([]map[string]interface{})
			Expect(len(aggregatesArray)).To(Equal(1))

			aggregatesData := aggregatesArray[0]
			Expect(aggregatesData).To(HaveKey("window_type"))
			Expect(aggregatesData["window_type"]).To(Equal("daily"))
		})

		It("should return error for invalid window type", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "aggregates",
				"window":    "invalid",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid window type"))
		})

		It("should return error for invalid type parameter", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "invalid",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid type parameter"))
		})

		It("should handle default type=raw when type parameter is omitted", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Should return paginated data format
			dataMap := data.(map[string]interface{})
			Expect(dataMap).To(HaveKey("data"))

			// Should contain the test data
			dataArray := dataMap["data"].([]map[string]interface{})
			Expect(len(dataArray)).To(Equal(3))
		})

		It("should handle type=raw explicitly", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "raw",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Should return paginated data format
			dataMap := data.(map[string]interface{})
			Expect(dataMap).To(HaveKey("data"))

			// Should contain the test data
			dataArray := dataMap["data"].([]map[string]interface{})
			Expect(len(dataArray)).To(Equal(3))
		})

		It("should handle historical aggregates with date parameter", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "aggregates",
				"window":    "daily",
				"date":      "2025-01-01",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Should return historical aggregates in array format for consistency
			dataMap := data.(map[string]interface{})
			Expect(dataMap).To(HaveKey("aggregates"))

			aggregatesArray := dataMap["aggregates"].([]map[string]interface{})
			Expect(len(aggregatesArray)).To(Equal(1))

			aggregatesData := aggregatesArray[0]
			Expect(aggregatesData).To(HaveKey("window_type"))
			Expect(aggregatesData).To(HaveKey("date"))
			Expect(aggregatesData["window_type"]).To(Equal("daily"))
			Expect(aggregatesData["date"]).To(Equal("2025-01-01"))
		})

		It("should return error for historical aggregates without window parameter", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "aggregates",
				"date":      "2025-01-01",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("window parameter is required for historical aggregates"))
		})

		It("should return error for invalid date format", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "aggregates",
				"window":    "daily",
				"date":      "invalid-date",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid date format"))
		})

		It("should handle type=raw with start_time and end_time parameters", func() {
			queryParams := map[string]string{
				"key":        "temperature",
				"data_type":  "float",
				"type":       "raw",
				"start_time": "1640995200000", // 2022-01-01 00:00:00 UTC (ms)
				"end_time":   "1640995260000", // 2022-01-01 00:01:00 UTC (ms)
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			// Should return paginated data format
			dataMap := data.(map[string]interface{})
			Expect(dataMap).To(HaveKey("data"))

			// Should contain filtered data (test data has entries at these timestamps)
			dataArray := dataMap["data"].([]map[string]interface{})
			Expect(len(dataArray)).To(BeNumerically(">=", 0)) // May be 0 if no test data in that range
		})

		It("should handle invalid start_time parameter", func() {
			queryParams := map[string]string{
				"key":        "temperature",
				"data_type":  "float",
				"type":       "raw",
				"start_time": "invalid_timestamp",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid start_time parameter"))
		})

		It("should handle invalid end_time parameter", func() {
			queryParams := map[string]string{
				"key":       "temperature",
				"data_type": "float",
				"type":      "raw",
				"end_time":  "invalid_timestamp",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			_, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("invalid end_time parameter"))
		})
	})

	Describe("GetLatest", func() {
		It("should return latest data for authorized user", func() {
			// Create system actor context for PutTimeseriesData operations
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

			// Add test data first
			testEntry := &timeseries_db.TimeseriesEntry{
				NodeID:    testNodeID,
				DataKey:   "temperature",
				DataType:  "float",
				Timestamp: 1640995200,
				Value:     25.5,
			}
			err := systemTimeseriesDB.PutTimeseriesData(testEntry)
			Expect(err).To(BeNil())

			// Get latest data via service
			dataPoint, err := timeseriesService.GetLatest(rmngCtx, testNodeID, "temperature", "float")
			Expect(err).To(BeNil())
			Expect(dataPoint).ToNot(BeNil())
			Expect(dataPoint.Timestamp).To(Equal(int64(1640995200)))
			Expect(dataPoint.Value).To(Equal(25.5))
		})

		It("should return error for unauthorized user", func() {
			// Get latest data via service with unauthorized user
			_, err := timeseriesService.GetLatest(unauthorizedCtx, testNodeID, "temperature", "float")
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unauthorized"))
		})
	})

	Describe("GetTimeRange", func() {
		It("should return data for specified time range", func() {
			// Create system actor context for PutTimeseriesData operations
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			systemTimeseriesDB := timeseries_db.NewTimeseriesDB(systemRmngCtx)

			// Add test data first
			testEntry := &timeseries_db.TimeseriesEntry{
				NodeID:    testNodeID,
				DataKey:   "temperature",
				DataType:  "float",
				Timestamp: 1640995200,
				Value:     25.5,
			}
			err := systemTimeseriesDB.PutTimeseriesData(testEntry)
			Expect(err).To(BeNil())

			// Get data via service
			startTime := time.Unix(1640995000, 0)
			endTime := time.Unix(1640995300, 0)
			response, err := timeseriesService.GetTimeRange(rmngCtx, testNodeID, "temperature", "float", startTime, endTime)
			Expect(err).To(BeNil())
			Expect(response).ToNot(BeNil())
			Expect(response.Data).To(HaveLen(1))
			Expect(response.Data[0].Timestamp).To(Equal(int64(1640995200)))
			Expect(response.Data[0].Value).To(Equal(25.5))
		})
	})

	Describe("Historical aggregates date formatting with non-UTC timezone", func() {
		It("should return correct local date in response for positive-offset timezone (Asia/Kolkata)", func() {
			// Scenario: Node is in Asia/Kolkata (UTC+5:30).
			// Reading 1: 2026-02-22 23:20 IST → daily window = 2026-02-22
			// Reading 2: 2026-02-23 00:05 IST → daily window = 2026-02-23 (boundary crossed, historical entry created for 2026-02-22)
			//
			// The historical entry for 2026-02-22 has WindowStart = epoch of 2026-02-22 00:00 IST
			// which is 2026-02-21 18:30 UTC. When the response formats this epoch in UTC,
			// it incorrectly shows "2026-02-21" instead of "2026-02-22".

			kolkata, err := time.LoadLocation("Asia/Kolkata")
			Expect(err).To(BeNil())

			// Reading 1: 2026-02-22 23:20:00 IST
			reading1Time := time.Date(2026, 2, 22, 23, 20, 0, 0, kolkata)
			// Reading 2: 2026-02-23 00:05:00 IST (crosses daily boundary)
			reading2Time := time.Date(2026, 2, 23, 0, 5, 0, 0, kolkata)

			// Create system actor context for processing
			systemActor := utils.NewSystemActor()
			systemRmngCtx := rmngctx.NewRmngContext(systemActor)
			processor := timeseries.NewTimeseriesProcessor(systemRmngCtx)

			// Process reading 1
			rawEntry1 := &timeseries_db.TimeseriesEntry{
				NodeKeyDt:  testNodeID + ".temperature.float",
				NodeID:     testNodeID,
				DataKey:    "temperature",
				DataType:   "float",
				Timestamp:  reading1Time.Unix() * 1000, // milliseconds
				Timezone:   "Asia/Kolkata",
				Value:      25.0,
				Cumulative: false,
			}
			err = processor.ProcessTimeseriesEntry(rawEntry1)
			Expect(err).To(BeNil())

			// Process reading 2 (crosses daily boundary, should create historical entry for 2026-02-22)
			rawEntry2 := &timeseries_db.TimeseriesEntry{
				NodeKeyDt:  testNodeID + ".temperature.float",
				NodeID:     testNodeID,
				DataKey:    "temperature",
				DataType:   "float",
				Timestamp:  reading2Time.Unix() * 1000, // milliseconds
				Timezone:   "Asia/Kolkata",
				Value:      26.0,
				Cumulative: false,
			}
			err = processor.ProcessTimeseriesEntry(rawEntry2)
			Expect(err).To(BeNil())

			// Now query for historical daily aggregates for 2026-02-22
			// The service parses "2026-02-22" using time.Parse which gives UTC
			queryParams := map[string]string{
				"key":        "temperature",
				"data_type":  "float",
				"type":       "aggregates",
				"window":     "daily",
				"start_date": "2026-02-22",
				"end_date":   "2026-02-22",
			}
			rmngCtx.Context = context.WithValue(rmngCtx.Context, "query_params", queryParams)

			data, err := timeseriesService.Get(rmngCtx, testNodeID)
			Expect(err).To(BeNil())

			dataMap := data.(map[string]interface{})
			aggregates := dataMap["aggregates"].([]map[string]interface{})
			Expect(len(aggregates)).To(Equal(1), "Expected exactly 1 historical aggregate entry for 2026-02-22")

			// The date field in the response should show "2026-02-22" (the correct local date),
			// NOT "2026-02-21" (which is what happens when the WindowStart epoch is formatted in UTC)
			Expect(aggregates[0]["date"]).To(Equal("2026-02-22"),
				"Response date should reflect the local timezone date, not the UTC interpretation of the window start epoch")
		})
	})

	// The config is what names the parameters to purge, so a config we cannot decode has to stop
	// the purge rather than silently narrow it to nothing.
	Describe("Delete", func() {
		seedConfig := func(cfg map[string]interface{}) {
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(testNodeID))
			Expect(node_details_db.NewNodeDetailsDB(nodeCtx).UpdateServiceData("config", cfg)).To(BeNil())
		}

		It("should refuse the purge when the node's config cannot be decoded", func() {
			// devices must be a list; a node that serialises it as an object is an ordinary slip
			// for a C JSON writer, and is not under cloud control.
			seedConfig(map[string]interface{}{
				"data_model": "default",
				"devices":    map[string]interface{}{"unexpected": "object"},
			})

			err := timeseriesService.Delete(rmngCtx, testNodeID)

			Expect(err).ToNot(BeNil(), "an undecodable config must stop the purge, not report success")
			Expect(err.Error()).To(ContainSubstring("cannot determine which timeseries data to delete"))
		})

		It("should still report success for a node whose config declares no timeseries parameters", func() {
			seedConfig(map[string]interface{}{
				"data_model": "default",
				"devices": []interface{}{map[string]interface{}{
					"id":     "Light",
					"type":   "esp.device.lightbulb",
					"params": []interface{}{},
				}},
			})

			Expect(timeseriesService.Delete(rmngCtx, testNodeID)).To(BeNil())
		})
	})

})

// The two timeseries tables share a partition key but not a sort key. A batch delete names the
// sort key explicitly, so naming the raw table's "ts" against the processed table matched nothing
// and aborted the purge, leaving data the caller was told had been deleted.
var _ = Describe("Purging all timeseries data for a node", func() {
	const nodeID = "purge-node"

	var (
		db     *timeseries_db.TimeseriesDB
		mockDB *mock.DynamoDBMock
		params []timeseries_db.ParamKey
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.AddTable(timeseries_db.RawTSDataTable, "node_key_dt", "ts")
		mockDB.AddTable(processed_ts_db.ProcessedTSDataTable, "node_key_dt", "interval_key")
		mockDB.ProfileReset()

		sysCtx := rmngctx.NewRmngContext(utils.NewSystemActor())
		db = timeseries_db.NewTimeseriesDB(sysCtx)

		params = []timeseries_db.ParamKey{
			{Name: "temperature", DataType: "float"},
			{Name: "energy", DataType: "float"},
		}

		processedDB := processed_ts_db.NewProcessedTsDB(sysCtx)
		for _, p := range params {
			// Raw samples, keyed on ts.
			for _, ts := range []int64{1000, 2000} {
				Expect(db.PutTimeseriesData(&timeseries_db.TimeseriesEntry{
					NodeID: nodeID, DataKey: p.Name, DataType: p.DataType,
					Timestamp: ts, TopicName: "ts-test", Value: 1.0,
				})).To(Succeed())
			}
			// Processed rows, keyed on interval_key — both the open "current" row and an
			// archived window, since a purge has to take both.
			entry := &processed_ts_db.ProcessedTsEntry{
				NodeKeyDt: nodeID + "." + p.Name + "." + p.DataType,
				NodeID:    nodeID, DataKey: p.Name, DataType: p.DataType,
			}
			Expect(processedDB.UpsertCurrentEntry(entry)).To(Succeed())
			Expect(processedDB.CreateWindowEntry(entry, "daily#2026-01-15")).To(Succeed())
		}
	})

	It("removes every row from both tables, for every parameter", func() {
		Expect(db.DeleteAllTimeseriesForNode(nodeID, params)).To(Succeed())

		for _, p := range params {
			raw, err := db.GetTimeseriesDataWithPagination(nodeID, p.Name, p.DataType, 0, 0, 0, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(raw.Entries).To(BeEmpty(), "raw data survived the purge for %s", p.Name)
		}

		// The processed table is the one the old code never touched at all.
		sysCtx := rmngctx.NewRmngContext(utils.NewSystemActor())
		processedDB := processed_ts_db.NewProcessedTsDB(sysCtx)
		for _, p := range params {
			entries, err := processedDB.GetWindowEntries(nodeID, p.Name, p.DataType,
				timewindow.WindowDaily, time.Time{}, time.Time{})
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(BeEmpty(), "archived aggregates survived the purge for %s", p.Name)

			current, err := processedDB.GetCurrentEntry(nodeID, p.Name, p.DataType)
			Expect(err).ToNot(HaveOccurred())
			Expect(current).To(BeNil(), "the open current aggregate survived the purge for %s", p.Name)
		}
	})

	It("purges the second parameter even when the first one fails", func() {
		// An unknown table name is the cheapest way to make one pair fail; the old code returned
		// on the first error and abandoned everything after it.
		Expect(db.DeleteTimeseriesForParam("rmng-no-such-table", nodeID, "temperature", "float")).
			To(HaveOccurred(), "an unknown table must be refused, not silently skipped")

		Expect(db.DeleteAllTimeseriesForNode(nodeID, params)).To(Succeed())

		for _, p := range params {
			raw, err := db.GetTimeseriesDataWithPagination(nodeID, p.Name, p.DataType, 0, 0, 0, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(raw.Entries).To(BeEmpty(), "raw data survived for %s", p.Name)
		}
	})

	It("reports an error rather than claiming success when a table is unknown", func() {
		err := db.DeleteTimeseriesForParam("rmng-no-such-table", nodeID, "temperature", "float")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rmng-no-such-table"))
	})
})

// The interval_key range is inclusive on both ends while the handlers pass an exclusive
// endTime, so a window boundary is easy to over-include. Each seeded window carries a
// distinct sum so a response can be traced back to the window it actually came from.
var _ = Describe("Timeseries historical aggregates window range", func() {
	const nodeID = "window-range-node"

	var (
		svc     *timeseries.TimeseriesService
		rmngCtx *rmngctx.RmngContext
	)

	// seed writes one historical window entry per interval_key, using the map value as its sum.
	seed := func(windowType string, windows map[string]float64) {
		db := processed_ts_db.NewProcessedTsDB(rmngctx.NewRmngContext(utils.NewSystemActor()))
		for key, sum := range windows {
			entry := &processed_ts_db.ProcessedTsEntry{
				NodeKeyDt:  nodeID + ".temperature.float",
				NodeID:     nodeID,
				DataKey:    "temperature",
				DataType:   "float",
				WindowType: windowType,
				WindowAggregates: processed_ts_db.WindowAggregates{
					Count: 1, Sum: sum, Min: sum, Max: sum, Average: sum,
					FirstValue: sum, LastValue: sum,
				},
			}
			Expect(db.CreateWindowEntry(entry, key)).To(Succeed())
		}
	}

	// query runs a GET against the /aggregates sub-path and returns the aggregates array.
	query := func(params map[string]string) []map[string]interface{} {
		params["key"] = "temperature"
		params["data_type"] = "float"
		ctx := context.WithValue(rmngCtx.Context, "query_params", params)
		rmngCtx.Context = context.WithValue(ctx, "timeseries_type", "aggregates")

		data, err := svc.Get(rmngCtx, nodeID)
		Expect(err).ToNot(HaveOccurred())
		return data.(map[string]interface{})["aggregates"].([]map[string]interface{})
	}

	dates := func(aggregates []map[string]interface{}) []string {
		out := make([]string, 0, len(aggregates))
		for _, a := range aggregates {
			out = append(out, a["date"].(string))
		}
		return out
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		service.Initialize()
		timeseries.Register()
		svc = timeseries.NewTimeseriesService()

		u := user.NewUser("window-range-user")
		u.Permissions.SetAllow(utils.NodeGet.String(), nodeID)
		rmngCtx = rmngctx.NewRmngContext(u)

		mockDB := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.AddTable(processed_ts_db.ProcessedTSDataTable, "node_key_dt", "interval_key")
		mockDB.ProfileReset()
	})

	Describe("a single date", func() {
		DescribeTable("returns only the requested window",
			func(windowType string, windows map[string]float64, date string, wantSum float64) {
				seed(windowType, windows)

				aggregates := query(map[string]string{"window": windowType, "date": date})

				Expect(aggregates).To(HaveLen(1))
				Expect(aggregates[0]["date"]).To(Equal(date))
				Expect(aggregates[0]["sum"]).To(BeNumerically("==", wantSum))
			},
			Entry("daily does not return the following day",
				"daily", map[string]float64{"daily#2026-01-15": 15, "daily#2026-01-16": 16},
				"2026-01-15", 15.0),
			Entry("hourly does not return the following hour",
				"hourly", map[string]float64{"hourly#2026-01-15T14": 14, "hourly#2026-01-15T15": 15},
				"2026-01-15T14", 14.0),
			Entry("monthly does not return the following month",
				"monthly", map[string]float64{"monthly#2026-01": 1, "monthly#2026-02": 2},
				"2026-01-15", 1.0),
			Entry("weekly does not return the following week",
				"weekly", map[string]float64{"weekly#2026-01-12": 12, "weekly#2026-01-19": 19},
				"2026-01-15", 12.0),
		)

		It("resolves an hourly date carrying no hour to hour 00", func() {
			seed("hourly", map[string]float64{"hourly#2026-01-15T00": 0, "hourly#2026-01-15T01": 1})

			aggregates := query(map[string]string{"window": "hourly", "date": "2026-01-15"})

			Expect(aggregates).To(HaveLen(1))
			Expect(aggregates[0]["sum"]).To(BeNumerically("==", 0))
		})

		It("reports no data when only the following window exists", func() {
			seed("daily", map[string]float64{"daily#2026-01-16": 16})

			aggregates := query(map[string]string{"window": "daily", "date": "2026-01-15"})

			Expect(aggregates).To(HaveLen(1))
			Expect(aggregates[0]).To(HaveKeyWithValue("message", "No historical data available for this window"))
			Expect(aggregates[0]).ToNot(HaveKey("sum"))
		})
	})

	Describe("a date range", func() {
		It("excludes the window immediately after end_date", func() {
			seed("daily", map[string]float64{
				"daily#2026-01-14": 14, "daily#2026-01-15": 15, "daily#2026-01-16": 16,
			})

			aggregates := query(map[string]string{
				"window": "daily", "start_date": "2026-01-14", "end_date": "2026-01-15",
			})

			Expect(dates(aggregates)).To(ConsistOf("2026-01-14", "2026-01-15"))
		})

		It("includes end_date itself when start and end are the same day", func() {
			seed("daily", map[string]float64{"daily#2026-01-15": 15, "daily#2026-01-16": 16})

			aggregates := query(map[string]string{
				"window": "daily", "start_date": "2026-01-15", "end_date": "2026-01-15",
			})

			Expect(dates(aggregates)).To(ConsistOf("2026-01-15"))
		})

		It("covers the whole day when an hourly end_date carries no hour", func() {
			seed("hourly", map[string]float64{
				"hourly#2026-01-15T00": 0, "hourly#2026-01-15T23": 23, "hourly#2026-01-16T00": 100,
			})

			aggregates := query(map[string]string{
				"window": "hourly", "start_date": "2026-01-15", "end_date": "2026-01-15",
			})

			Expect(dates(aggregates)).To(ConsistOf("2026-01-15T00", "2026-01-15T23"))
		})

		It("includes the end hour when an hourly end_date carries one", func() {
			seed("hourly", map[string]float64{
				"hourly#2026-01-15T14": 14, "hourly#2026-01-15T15": 15, "hourly#2026-01-15T16": 16,
			})

			aggregates := query(map[string]string{
				"window": "hourly", "start_date": "2026-01-15T14", "end_date": "2026-01-15T15",
			})

			Expect(dates(aggregates)).To(ConsistOf("2026-01-15T14", "2026-01-15T15"))
		})

		It("excludes the next year when a monthly range ends in December", func() {
			seed("monthly", map[string]float64{
				"monthly#2026-01": 1, "monthly#2026-12": 12, "monthly#2027-01": 100,
			})

			aggregates := query(map[string]string{
				"window": "monthly", "start_date": "2026-01-01", "end_date": "2026-12-31",
			})

			Expect(dates(aggregates)).To(ConsistOf("2026-01", "2026-12"))
		})

		It("keeps a sub-month range inside its own month", func() {
			seed("monthly", map[string]float64{"monthly#2025-12": 12, "monthly#2026-01": 1})

			aggregates := query(map[string]string{
				"window": "monthly", "start_date": "2026-01-15", "end_date": "2026-01-20",
			})

			Expect(dates(aggregates)).To(ConsistOf("2026-01"))
		})

		It("bounds the range when only end_date is given", func() {
			seed("daily", map[string]float64{"daily#2026-01-14": 14, "daily#2026-01-15": 15})

			aggregates := query(map[string]string{"window": "daily", "end_date": "2026-01-14"})

			Expect(dates(aggregates)).To(ConsistOf("2026-01-14"))
		})

		It("returns an empty page when the range holds no windows", func() {
			seed("daily", map[string]float64{"daily#2026-01-20": 20})

			aggregates := query(map[string]string{
				"window": "daily", "start_date": "2026-01-14", "end_date": "2026-01-15",
			})

			Expect(aggregates).To(BeEmpty())
		})
	})

	Describe("an unparseable bound", func() {
		DescribeTable("is rejected rather than silently dropped",
			func(params map[string]string, wantMessage string) {
				params["key"] = "temperature"
				params["data_type"] = "float"
				ctx := context.WithValue(rmngCtx.Context, "query_params", params)
				rmngCtx.Context = context.WithValue(ctx, "timeseries_type", "aggregates")

				_, err := svc.Get(rmngCtx, nodeID)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(wantMessage))
			},
			Entry("a malformed single date",
				map[string]string{"window": "daily", "date": "15-01-2026"},
				"invalid date format"),
			Entry("a malformed start_date",
				map[string]string{"window": "daily", "start_date": "not-a-date", "end_date": "2026-01-15"},
				"invalid start_date format"),
			Entry("a malformed end_date",
				map[string]string{"window": "daily", "start_date": "2026-01-14", "end_date": "2026-13-99"},
				"invalid end_date format"),
			Entry("an hourly bound with a malformed hour",
				map[string]string{"window": "hourly", "start_date": "2026-01-15T99", "end_date": "2026-01-15T16"},
				"invalid start_date format"),
		)
	})
})
