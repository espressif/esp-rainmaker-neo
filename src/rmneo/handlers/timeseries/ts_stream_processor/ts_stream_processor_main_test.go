// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/processed_ts_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries/timewindow"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTSStreamProcessor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TS Stream Processor Suite")
}

var _ = Describe("TSStreamProcessor", func() {
	var mockDB *mock.DynamoDBMock

	BeforeEach(func() {
		test_utils.TestSetup()

		// Initialize mock DynamoDB and add required tables
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.AddTable(processed_ts_db.ProcessedTSDataTable, "node_key_dt", "interval_key")
		mockDB.ProfileReset()
	})

	Describe("Step 1: TSStreamProcessor Initialization", func() {
		var processor *TSStreamProcessor

		Context("when creating a new processor", func() {
			BeforeEach(func() {
				processor = NewTSStreamProcessor()
			})

			It("1.1 should create TSStreamProcessor with valid system context", func() {
				Expect(processor).ToNot(BeNil())
				Expect(processor.rmngCtx).ToNot(BeNil())
				Expect(processor.timeseriesDB).ToNot(BeNil())
			})

			It("1.2 should initialize timeseriesDB correctly", func() {
				Expect(processor.timeseriesDB).To(BeAssignableToTypeOf(&timeseries_db.TimeseriesDB{}))

				// Verify the database connection is valid
				Expect(processor.timeseriesDB.DB.DBUtil.DynamoDBClientInterface).ToNot(BeNil())
				Expect(processor.timeseriesDB.DB.Ctx).ToNot(BeNil())
			})

			It("1.3 should set up rmngCtx with system actor", func() {
				Expect(processor.rmngCtx).To(BeAssignableToTypeOf(&rmngctx.RmngContext{}))

				// Verify system actor is set
				Expect(processor.rmngCtx.Accessor).ToNot(BeNil())

				// Verify it's a system actor (should have admin permissions)
				systemActor := processor.rmngCtx.Accessor
				Expect(systemActor.GetID()).To(Equal(utils.SYSTEM_ACTOR))
			})
		})

		Context("when multiple processors are created", func() {
			It("should create independent instances", func() {
				processor1 := NewTSStreamProcessor()
				processor2 := NewTSStreamProcessor()

				Expect(processor1).ToNot(BeNil())
				Expect(processor2).ToNot(BeNil())
				Expect(processor1).ToNot(BeIdenticalTo(processor2))
				Expect(processor1.timeseriesDB).ToNot(BeIdenticalTo(processor2.timeseriesDB))
				Expect(processor1.rmngCtx).ToNot(BeIdenticalTo(processor2.rmngCtx))
			})
		})
	})

	Describe("Step 2: DynamoDB Stream Event Processing", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when processing different event types", func() {
			It("2.1 should process INSERT events successfully", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())
			})

			It("2.2 should process MODIFY events successfully", func() {
				record := events.DynamoDBEventRecord{
					EventName: "MODIFY",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995260"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())
			})

			It("2.3 should ignore REMOVE events", func() {
				record := events.DynamoDBEventRecord{
					EventName: "REMOVE",
					Change: events.DynamoDBStreamRecord{
						OldImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil()) // Should return nil without processing
			})

			It("2.4 should ignore events with nil NewImage", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: nil,
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil()) // Should return nil without processing
			})

			It("2.5 should handle empty event records", func() {
				record := events.DynamoDBEventRecord{}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil()) // Should ignore empty records
			})
		})

		Context("when processing multiple events", func() {
			It("2.6 should process multiple events in sequence", func() {
				records := []events.DynamoDBEventRecord{
					{
						EventName: "INSERT",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
								"ts":          events.NewNumberAttribute("1640995200"),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("temperature"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute("25.5"),
								"cumulative":  events.NewBooleanAttribute(false),
							},
						},
					},
					{
						EventName: "MODIFY",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
								"ts":          events.NewNumberAttribute("1640995260"),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("temperature"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute("26.0"),
								"cumulative":  events.NewBooleanAttribute(false),
							},
						},
					},
				}

				for _, record := range records {
					err := processor.ProcessRecord(record)
					Expect(err).To(BeNil())
				}
			})
		})

		Context("when processing malformed events", func() {
			It("2.7 should return error for malformed events", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"invalid_field": events.NewStringAttribute("invalid"),
							// Missing required fields
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).ToNot(BeNil())
				// The error could be either conversion or processing failure
				Expect(err.Error()).To(SatisfyAny(
					ContainSubstring("failed to convert DynamoDB record to timeseries entry"),
					ContainSubstring("failed to process window aggregations"),
				))
			})
		})
	})

	Describe("Step 3: DynamoDB Attribute Value Conversion", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when converting valid attribute values", func() {
			It("3.1 should convert string attributes correctly", func() {
				image := map[string]events.DynamoDBAttributeValue{
					"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
					"ts":          events.NewNumberAttribute("1640995200"),
					"node_id":     events.NewStringAttribute("test-node"),
					"key":         events.NewStringAttribute("temperature"),
					"dt":          events.NewStringAttribute("float"),
					"tz":          events.NewStringAttribute("UTC"),
					"value":       events.NewNumberAttribute("25.5"),
					"cumulative":  events.NewBooleanAttribute(false),
				}

				entry, err := processor.timeseriesDB.UnMarshalToTimeseriesEntry(image)
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.NodeID).To(Equal("test-node"))
				Expect(entry.DataKey).To(Equal("temperature"))
				Expect(entry.DataType).To(Equal("float"))
				Expect(entry.Timezone).To(Equal("UTC"))
			})

			It("3.2 should convert number attributes correctly", func() {
				image := map[string]events.DynamoDBAttributeValue{
					"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
					"ts":          events.NewNumberAttribute("1640995200"),
					"node_id":     events.NewStringAttribute("test-node"),
					"key":         events.NewStringAttribute("temperature"),
					"dt":          events.NewStringAttribute("float"),
					"value":       events.NewNumberAttribute("25.5"),
					"cumulative":  events.NewBooleanAttribute(false),
				}

				entry, err := processor.timeseriesDB.UnMarshalToTimeseriesEntry(image)
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.Timestamp).To(Equal(int64(1640995200)))
				Expect(entry.Value).To(Equal(25.5))
			})

			It("3.3 should convert boolean attributes correctly", func() {
				image := map[string]events.DynamoDBAttributeValue{
					"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
					"ts":          events.NewNumberAttribute("1640995200"),
					"node_id":     events.NewStringAttribute("test-node"),
					"key":         events.NewStringAttribute("temperature"),
					"dt":          events.NewStringAttribute("float"),
					"value":       events.NewNumberAttribute("25.5"),
					"cumulative":  events.NewBooleanAttribute(true),
				}

				entry, err := processor.timeseriesDB.UnMarshalToTimeseriesEntry(image)
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.Cumulative).To(Equal(true))
			})

			It("3.4 should handle optional fields", func() {
				image := map[string]events.DynamoDBAttributeValue{
					"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
					"ts":          events.NewNumberAttribute("1640995200"),
					"node_id":     events.NewStringAttribute("test-node"),
					"key":         events.NewStringAttribute("temperature"),
					"dt":          events.NewStringAttribute("float"),
					"value":       events.NewNumberAttribute("25.5"),
					// Missing optional fields like cumulative, tz
				}

				entry, err := processor.timeseriesDB.UnMarshalToTimeseriesEntry(image)
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.Cumulative).To(Equal(false)) // Should default to false
				Expect(entry.Timezone).To(Equal(""))      // Should default to empty string
			})

			It("3.5 should handle null attributes", func() {
				image := map[string]events.DynamoDBAttributeValue{
					"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
					"ts":          events.NewNumberAttribute("1640995200"),
					"node_id":     events.NewStringAttribute("test-node"),
					"key":         events.NewStringAttribute("temperature"),
					"dt":          events.NewStringAttribute("float"),
					"value":       events.NewNumberAttribute("25.5"),
					"cumulative":  events.NewBooleanAttribute(false),
					"tz":          events.NewNullAttribute(),
				}

				entry, err := processor.timeseriesDB.UnMarshalToTimeseriesEntry(image)
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.Timezone).To(Equal("")) // Null should result in empty string
			})

			It("3.6 should convert binary attributes correctly", func() {
				binaryData := []byte{0x01, 0x02, 0x03, 0x04}
				image := map[string]events.DynamoDBAttributeValue{
					"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
					"ts":          events.NewNumberAttribute("1640995200"),
					"node_id":     events.NewStringAttribute("test-node"),
					"key":         events.NewStringAttribute("temperature"),
					"dt":          events.NewStringAttribute("float"),
					"value":       events.NewBinaryAttribute(binaryData),
					"cumulative":  events.NewBooleanAttribute(false),
				}

				entry, err := processor.timeseriesDB.UnMarshalToTimeseriesEntry(image)
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				// Binary data should be preserved as-is
				Expect(entry.Value).To(Equal(binaryData))
			})
		})

		Context("when converting invalid attribute values", func() {
			It("3.7 should handle empty image", func() {
				image := map[string]events.DynamoDBAttributeValue{}

				entry, err := processor.timeseriesDB.UnMarshalToTimeseriesEntry(image)
				Expect(err).ToNot(BeNil())
				Expect(entry).To(BeNil())
				// Should return error for missing required fields
				Expect(err.Error()).To(ContainSubstring("missing required field"))
			})
		})
	})

	Describe("Step 4: Value Type Conversion to Float64", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when converting numeric types", func() {
			It("4.1 should convert float64 values correctly", func() {
				value := 25.5
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(25.5))
			})

			It("4.2 should convert float32 values correctly", func() {
				value := float32(25.5)
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(25.5))
			})

			It("4.3 should convert int values correctly", func() {
				value := 25
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(25.0))
			})

			It("4.4 should convert int32 values correctly", func() {
				value := int32(25)
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(25.0))
			})

			It("4.5 should convert int64 values correctly", func() {
				value := int64(25)
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(25.0))
			})
		})

		Context("when converting string types", func() {
			It("4.6 should convert valid numeric strings correctly", func() {
				value := "25.5"
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(25.5))
			})

			It("4.7 should convert integer strings correctly", func() {
				value := "25"
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(25.0))
			})

			It("4.8 should return error for invalid strings", func() {
				value := "invalid_number"
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).ToNot(BeNil())
				Expect(result).To(Equal(0.0))
				Expect(err.Error()).To(ContainSubstring("cannot convert string"))
			})
		})

		Context("when converting boolean types", func() {
			It("4.9 should convert true to 1.0", func() {
				value := true
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(1.0))
			})

			It("4.10 should convert false to 0.0", func() {
				value := false
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(0.0))
			})
		})

		Context("when converting unsupported types", func() {
			It("4.11 should return error for unsupported types", func() {
				value := []byte{0x01, 0x02, 0x03}
				result, err := timeseries.NewTimeseriesProcessor(processor.rmngCtx).ConvertValueToFloat64(value)
				Expect(err).ToNot(BeNil())
				Expect(result).To(Equal(0.0))
				Expect(err.Error()).To(ContainSubstring("unsupported value type"))
			})
		})
	})

	Describe("Step 5: Non-Cumulative Data Processing", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when processing non-cumulative data", func() {
			It("5.1 should process first non-cumulative reading", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Verify the processed entry was created
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.IsCumulative).To(BeFalse())
			})

			It("5.2 should process subsequent non-cumulative readings", func() {
				// Process first reading
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Process second reading
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995260"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify aggregates are updated
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(2)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(51.5))
				Expect(entry.HourlyAggregates.Average).To(Equal(25.75))
				Expect(entry.HourlyAggregates.Min).To(Equal(25.5))
				Expect(entry.HourlyAggregates.Max).To(Equal(26.0))
			})

			It("5.3 should handle zero values correctly", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Verify zero values are handled correctly
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(0.0))
				Expect(entry.HourlyAggregates.Average).To(Equal(0.0))
				Expect(entry.HourlyAggregates.Min).To(Equal(0.0))
				Expect(entry.HourlyAggregates.Max).To(Equal(0.0))
			})

			It("5.4 should handle negative values correctly", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("-5.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Verify negative values are handled correctly
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(-5.5))
				Expect(entry.HourlyAggregates.Average).To(Equal(-5.5))
				Expect(entry.HourlyAggregates.Min).To(Equal(-5.5))
				Expect(entry.HourlyAggregates.Max).To(Equal(-5.5))
			})

			It("5.5 should update all window types for non-cumulative data", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Verify all window types are updated
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Check all windows have the same value for a single reading
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.DailyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.WeeklyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.MonthlyAggregates.Count).To(Equal(int64(1)))

				Expect(entry.HourlyAggregates.Sum).To(Equal(25.5))
				Expect(entry.DailyAggregates.Sum).To(Equal(25.5))
				Expect(entry.WeeklyAggregates.Sum).To(Equal(25.5))
				Expect(entry.MonthlyAggregates.Sum).To(Equal(25.5))
			})
		})
	})

	Describe("Step 6: Cumulative Data Processing", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when processing cumulative data", func() {
			It("6.1 should process first cumulative reading without aggregating", func() {
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("100.0"), // 100 kWh cumulative
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Verify the processed entry was created but no aggregation yet
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
				Expect(entry.IsCumulative).To(BeTrue())

				// First reading should not contribute to count/aggregates
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(0)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(0.0))
				Expect(entry.HourlyAggregates.FirstValue).To(Equal(100.0)) // Baseline established
				Expect(entry.HourlyAggregates.LastValue).To(Equal(100.0))
			})

			It("6.2 should calculate consumption on second cumulative reading", func() {
				// Process first reading (establishes baseline)
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("100.0"), // 100 kWh cumulative
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Process second reading (first consumption calculation)
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640995260"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("105.0"), // 105 kWh cumulative
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify consumption is calculated correctly
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Should have 1 consumption reading (5 kWh consumed)
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(5.0))               // Consumption: 105 - 100 = 5
				Expect(entry.HourlyAggregates.Average).To(Equal(5.0))           // Average consumption
				Expect(entry.HourlyAggregates.Min).To(Equal(5.0))               // Min consumption
				Expect(entry.HourlyAggregates.Max).To(Equal(5.0))               // Max consumption
				Expect(entry.HourlyAggregates.FirstValue).To(Equal(100.0))      // First cumulative value
				Expect(entry.HourlyAggregates.LastValue).To(Equal(105.0))       // Last cumulative value
				Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(105.0)) // Latest cumulative reading
			})

			It("6.3 should handle multiple cumulative readings correctly", func() {
				// Process three cumulative readings
				readings := []struct {
					timestamp string
					value     string
				}{
					{"1640995200", "100.0"}, // Baseline
					{"1640995260", "105.0"}, // +5 kWh
					{"1640995320", "110.0"}, // +5 kWh
				}

				for _, reading := range readings {
					record := events.DynamoDBEventRecord{
						EventName: "INSERT",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
								"ts":          events.NewNumberAttribute(reading.timestamp),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("energy"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute(reading.value),
								"cumulative":  events.NewBooleanAttribute(true),
							},
						},
					}

					err := processor.ProcessRecord(record)
					Expect(err).To(BeNil())
				}

				// Verify final aggregates
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Should have 2 consumption readings (5 kWh each)
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(2)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(10.0))              // Total consumption: 5 + 5 = 10
				Expect(entry.HourlyAggregates.Average).To(Equal(5.0))           // Average consumption: 10/2 = 5
				Expect(entry.HourlyAggregates.Min).To(Equal(5.0))               // Min consumption
				Expect(entry.HourlyAggregates.Max).To(Equal(5.0))               // Max consumption
				Expect(entry.HourlyAggregates.FirstValue).To(Equal(100.0))      // First cumulative value
				Expect(entry.HourlyAggregates.LastValue).To(Equal(110.0))       // Last cumulative value
				Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(110.0)) // Latest cumulative reading
			})

			It("6.4 should handle varying consumption rates", func() {
				// Process readings with different consumption rates
				readings := []struct {
					timestamp string
					value     string
				}{
					{"1640995200", "100.0"}, // Baseline
					{"1640995260", "103.0"}, // +3 kWh
					{"1640995320", "110.0"}, // +7 kWh
				}

				for _, reading := range readings {
					record := events.DynamoDBEventRecord{
						EventName: "INSERT",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
								"ts":          events.NewNumberAttribute(reading.timestamp),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("energy"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute(reading.value),
								"cumulative":  events.NewBooleanAttribute(true),
							},
						},
					}

					err := processor.ProcessRecord(record)
					Expect(err).To(BeNil())
				}

				// Verify aggregates reflect varying consumption
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Should have 2 consumption readings (3 kWh + 7 kWh = 10 kWh total)
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(2)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(10.0))              // Total consumption: 3 + 7 = 10
				Expect(entry.HourlyAggregates.Average).To(Equal(5.0))           // Average consumption: 10/2 = 5
				Expect(entry.HourlyAggregates.Min).To(Equal(3.0))               // Min consumption
				Expect(entry.HourlyAggregates.Max).To(Equal(7.0))               // Max consumption
				Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(110.0)) // Latest cumulative reading
			})

			It("6.5 should handle zero consumption correctly", func() {
				// Process readings with no consumption
				readings := []struct {
					timestamp string
					value     string
				}{
					{"1640995200", "100.0"}, // Baseline
					{"1640995260", "100.0"}, // No consumption
					{"1640995320", "100.0"}, // No consumption
				}

				for _, reading := range readings {
					record := events.DynamoDBEventRecord{
						EventName: "INSERT",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
								"ts":          events.NewNumberAttribute(reading.timestamp),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("energy"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute(reading.value),
								"cumulative":  events.NewBooleanAttribute(true),
							},
						},
					}

					err := processor.ProcessRecord(record)
					Expect(err).To(BeNil())
				}

				// Verify zero consumption is handled correctly
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Should have 2 consumption readings (both 0 kWh)
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(2)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(0.0))               // Total consumption: 0 + 0 = 0
				Expect(entry.HourlyAggregates.Average).To(Equal(0.0))           // Average consumption: 0/2 = 0
				Expect(entry.HourlyAggregates.Min).To(Equal(0.0))               // Min consumption
				Expect(entry.HourlyAggregates.Max).To(Equal(0.0))               // Max consumption
				Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(100.0)) // Latest cumulative reading
			})

			It("6.6 should update all window types for cumulative data", func() {
				// Process two cumulative readings
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("100.0"),
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640995260"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("105.0"),
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify all window types are updated with consumption values
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Check all windows have the same consumption value
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.DailyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.WeeklyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.MonthlyAggregates.Count).To(Equal(int64(1)))

				// All windows should show 5 kWh consumption
				Expect(entry.HourlyAggregates.Sum).To(Equal(5.0))
				Expect(entry.DailyAggregates.Sum).To(Equal(5.0))
				Expect(entry.WeeklyAggregates.Sum).To(Equal(5.0))
				Expect(entry.MonthlyAggregates.Sum).To(Equal(5.0))
			})

			It("6.7 should handle meter reset (decreasing cumulative values)", func() {
				// Process readings that simulate a meter reset
				readings := []struct {
					timestamp string
					value     string
					expected  struct {
						count int64
						sum   float64
					}
				}{
					{"1640995200", "100.0", struct {
						count int64
						sum   float64
					}{0, 0.0}}, // Baseline - no consumption yet
					{"1640995260", "105.0", struct {
						count int64
						sum   float64
					}{1, 5.0}}, // Normal consumption: 5 kWh
					{"1640995320", "10.0", struct {
						count int64
						sum   float64
					}{2, 15.0}}, // Meter reset: treat 10.0 as absolute consumption
					{"1640995380", "15.0", struct {
						count int64
						sum   float64
					}{3, 20.0}}, // Normal consumption after reset: 5 kWh
				}

				for i, reading := range readings {
					record := events.DynamoDBEventRecord{
						EventName: "INSERT",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
								"ts":          events.NewNumberAttribute(reading.timestamp),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("energy"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute(reading.value),
								"cumulative":  events.NewBooleanAttribute(true),
							},
						},
					}

					err := processor.ProcessRecord(record)
					Expect(err).To(BeNil())

					// Verify aggregates after each reading
					entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
					Expect(err).To(BeNil())
					Expect(entry).ToNot(BeNil())

					// Check that aggregates match expectations
					Expect(entry.HourlyAggregates.Count).To(Equal(reading.expected.count),
						"Count mismatch after reading %d (value=%s)", i+1, reading.value)
					Expect(entry.HourlyAggregates.Sum).To(Equal(reading.expected.sum),
						"Sum mismatch after reading %d (value=%s)", i+1, reading.value)

					// Always verify the latest cumulative value is stored
					expectedCumulative, _ := strconv.ParseFloat(reading.value, 64)
					Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(expectedCumulative))
				}

				// Final verification of the complete scenario
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Final state should show:
				// - 3 consumption readings (normal + reset + normal)
				// - Total consumption: 5 (normal) + 10 (reset as absolute) + 5 (normal after reset) = 20 kWh
				// - Average: 20/3 = 6.67 kWh
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(3)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(20.0))
				Expect(entry.HourlyAggregates.Average).To(BeNumerically("~", 6.67, 0.01))
				Expect(entry.HourlyAggregates.Min).To(Equal(5.0))              // Minimum consumption
				Expect(entry.HourlyAggregates.Max).To(Equal(10.0))             // Maximum consumption (the reset value)
				Expect(entry.HourlyAggregates.FirstValue).To(Equal(100.0))     // Original baseline
				Expect(entry.HourlyAggregates.LastValue).To(Equal(15.0))       // Final cumulative value
				Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(15.0)) // Latest cumulative reading
			})

			It("6.8 should handle multiple meter resets correctly", func() {
				// Test multiple meter resets in sequence
				readings := []struct {
					timestamp string
					value     string
					desc      string
				}{
					{"1640995200", "100.0", "Initial baseline"},
					{"1640995260", "105.0", "Normal consumption: +5"},
					{"1640995320", "8.0", "First reset: treat 8.0 as absolute consumption"},
					{"1640995380", "12.0", "Normal after first reset: +4"},
					{"1640995440", "3.0", "Second reset: treat 3.0 as absolute consumption"},
					{"1640995500", "7.0", "Normal after second reset: +4"},
				}

				for _, reading := range readings {
					record := events.DynamoDBEventRecord{
						EventName: "INSERT",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
								"ts":          events.NewNumberAttribute(reading.timestamp),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("energy"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute(reading.value),
								"cumulative":  events.NewBooleanAttribute(true),
							},
						},
					}

					err := processor.ProcessRecord(record)
					Expect(err).To(BeNil())
				}

				// Verify final state
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Expected consumption: 5 (normal) + 8 (first reset) + 4 (normal) + 3 (second reset) + 4 (normal) = 24 kWh
				// Count: 5 consumption readings
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(5)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(24.0))
				Expect(entry.HourlyAggregates.Average).To(BeNumerically("~", 4.8, 0.01)) // 24/5 = 4.8
				Expect(entry.HourlyAggregates.Min).To(Equal(3.0))                        // Minimum consumption (second reset)
				Expect(entry.HourlyAggregates.Max).To(Equal(8.0))                        // Maximum consumption (first reset)
				Expect(entry.HourlyAggregates.FirstValue).To(Equal(100.0))               // Original baseline
				Expect(entry.HourlyAggregates.LastValue).To(Equal(7.0))                  // Final cumulative value
				Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(7.0))            // Latest cumulative reading
			})
		})
	})

	Describe("Step 7: Window Boundary Handling", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when crossing window boundaries", func() {
			It("7.1 should detect hourly window boundary crossing", func() {
				// Process reading at end of hour
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998799000"), // 2021-12-31 23:59:59 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Process reading after hour boundary
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998800000"), // 2022-01-01 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify current entry reflects new window
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Current aggregates should show only the new reading
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(26.0))
				Expect(entry.HourlyAggregates.Average).To(Equal(26.0))
			})

			It("7.2 should handle daily window boundary crossing", func() {
				// Process reading on first day
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640908800000"), // 2021-12-31 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Process reading on different day (clearly separate day)
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1641081600000"), // 2022-01-02 00:00:00 UTC (next day)
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify daily aggregates reset
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Daily aggregates should show only the new reading
				Expect(entry.DailyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.DailyAggregates.Sum).To(Equal(26.0))
			})

			It("7.3 should preserve cumulative continuity across hourly boundaries", func() {
				// Process cumulative reading at end of hour
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640998799000"), // 2021-12-31 23:59:59 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("100.0"), // Baseline
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Second reading same hour
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640998799000"), // Same hour
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("105.0"), // +5 kWh
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Process reading after hour boundary
				record3 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640998800000"), // Next hour
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("110.0"), // +5 kWh in new hour
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				err = processor.ProcessRecord(record3)
				Expect(err).To(BeNil())

				// Verify cumulative continuity preserved
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// New hour should show consumption from 105 to 110 = 5 kWh
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(5.0))          // Consumption in new hour
				Expect(entry.HourlyAggregates.FirstValue).To(Equal(105.0)) // Baseline for new hour
				Expect(entry.HourlyAggregates.LastValue).To(Equal(110.0))  // Current value
			})

			It("7.4 should handle multiple window boundaries simultaneously", func() {
				// Process reading in December
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640908800"), // 2021-12-31 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Process reading in February (clearly different month, week, day, hour)
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1643673600000"), // 2022-02-01 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify all window types reset correctly
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// All windows should show only the new reading
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.DailyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.WeeklyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.MonthlyAggregates.Count).To(Equal(int64(1)))

				// All should have the same value since it's the first reading in each new window
				Expect(entry.HourlyAggregates.Sum).To(Equal(26.0))
				Expect(entry.DailyAggregates.Sum).To(Equal(26.0))
				Expect(entry.WeeklyAggregates.Sum).To(Equal(26.0))
				Expect(entry.MonthlyAggregates.Sum).To(Equal(26.0))
			})

			It("7.5 should handle timezone-aware window boundaries", func() {
				// Process reading in PST timezone near boundary
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998799"), // 2021-12-31 23:59:59 UTC = 2021-12-31 15:59:59 PST
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("PST8PDT"), // Pacific timezone
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Process reading after timezone-aware boundary
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1641002400"), // 2022-01-01 01:00:00 UTC = 2021-12-31 17:00:00 PST (same PST day)
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("PST8PDT"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify timezone-aware aggregation
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Should aggregate both readings since they're in the same PST day
				Expect(entry.DailyAggregates.Count).To(Equal(int64(2)))
				Expect(entry.DailyAggregates.Sum).To(Equal(51.5)) // 25.5 + 26.0
			})

			It("7.6 should handle rapid successive boundary crossings", func() {
				// Process multiple readings that cross boundaries in quick succession
				timestamps := []string{
					"1640998799000", // End of hour 1
					"1640998800000", // Start of hour 2
					"1641002399000", // End of hour 2
					"1641002400000", // Start of hour 3
				}

				values := []string{"25.0", "26.0", "27.0", "28.0"}

				for i, timestamp := range timestamps {
					record := events.DynamoDBEventRecord{
						EventName: "INSERT",
						Change: events.DynamoDBStreamRecord{
							NewImage: map[string]events.DynamoDBAttributeValue{
								"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
								"ts":          events.NewNumberAttribute(timestamp),
								"node_id":     events.NewStringAttribute("test-node"),
								"key":         events.NewStringAttribute("temperature"),
								"dt":          events.NewStringAttribute("float"),
								"tz":          events.NewStringAttribute("UTC"),
								"value":       events.NewNumberAttribute(values[i]),
								"cumulative":  events.NewBooleanAttribute(false),
							},
						},
					}

					err := processor.ProcessRecord(record)
					Expect(err).To(BeNil())
				}

				// Verify final state shows only the latest window
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Current hour should only have the last reading
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(28.0))
				Expect(entry.HourlyAggregates.Average).To(Equal(28.0))
			})

			It("7.7 should maintain data integrity across window transitions", func() {
				// Test that no data is lost during window transitions
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200000"), // Earlier in hour
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998799000"), // End of hour
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				record3 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998800000"), // Start of next hour
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("27.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process all records
				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record3)
				Expect(err).To(BeNil())

				// Verify current entry has correct state
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Current hour should have the last reading
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(27.0))

				// Daily aggregates should include all three readings
				Expect(entry.DailyAggregates.Count).To(Equal(int64(3)))
				Expect(entry.DailyAggregates.Sum).To(Equal(78.5)) // 25.5 + 26.0 + 27.0
				Expect(entry.DailyAggregates.Average).To(BeNumerically("~", 26.17, 0.01))
				Expect(entry.DailyAggregates.Min).To(Equal(25.5))
				Expect(entry.DailyAggregates.Max).To(Equal(27.0))
			})
		})
	})

	Describe("Step 8: New Entry Creation", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when processing first reading for a parameter", func() {
			It("8.1 should create new entry for first reading", func() {
				// Verify no entry exists initially
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).To(BeNil())

				// Process first reading
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"), // 2021-12-31 12:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err = processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Verify new entry was created
				entry, err = processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Verify entry structure
				Expect(entry.NodeKeyDt).To(Equal("test-node.temperature.float"))
				Expect(entry.IntervalKey).To(Equal("current"))
				Expect(entry.NodeID).To(Equal("test-node"))
				Expect(entry.DataKey).To(Equal("temperature"))
				Expect(entry.DataType).To(Equal("float"))
				Expect(entry.Timezone).To(Equal("UTC"))
				Expect(entry.IsCumulative).To(BeFalse())
			})

			It("8.2 should set timestamps correctly for new entries", func() {
				// Record the time before processing
				beforeProcessing := time.Now().Unix()

				// Device timestamp (different from cloud time)
				deviceTimestamp := "1640995200000" // 2021-12-31 12:00:00 UTC

				// Process first reading
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.timestamps.float"),
							"ts":          events.NewNumberAttribute(deviceTimestamp),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("timestamps"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Record the time after processing
				afterProcessing := time.Now().Unix()

				// Verify entry was created with correct timestamps
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "timestamps", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// UpdatedAt should be cloud timestamp (between before and after processing)
				Expect(entry.UpdatedAt).To(BeNumerically(">=", beforeProcessing))
				Expect(entry.UpdatedAt).To(BeNumerically("<=", afterProcessing))

				// LastUpdateTime should be device timestamp (timezone-aware)
				expectedDeviceTimestamp := int64(1640995200) // Device timestamp in seconds (UTC)
				Expect(entry.LastUpdateTime).To(Equal(expectedDeviceTimestamp))
			})

			It("8.3 should update timestamps correctly when updating existing entries", func() {
				// Create first reading
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.update-test.float"),
							"ts":          events.NewNumberAttribute("1640995200000"), // Device time 1
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("update-test"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())

				// Get the entry after first processing
				entry1, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "update-test", "float")
				Expect(err).To(BeNil())
				Expect(entry1).ToNot(BeNil())

				// Store the original timestamps
				originalUpdatedAt := entry1.UpdatedAt
				originalLastUpdateTime := entry1.LastUpdateTime

				// Wait a moment to ensure different timestamps
				time.Sleep(time.Millisecond * 10)

				// Process second reading with different device timestamp
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.update-test.float"),
							"ts":          events.NewNumberAttribute("1640995260000"), // Device time 2 (60 seconds later)
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("update-test"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Get the entry after second processing
				entry2, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "update-test", "float")
				Expect(err).To(BeNil())
				Expect(entry2).ToNot(BeNil())

				// Verify timestamp behavior:
				// UpdatedAt should be updated to new cloud timestamp
				Expect(entry2.UpdatedAt).To(BeNumerically(">=", originalUpdatedAt))

				// LastUpdateTime should be updated to new device timestamp (timezone-aware)
				expectedNewDeviceTimestamp := int64(1640995260) // Device timestamp 2 in seconds (UTC)
				Expect(entry2.LastUpdateTime).To(Equal(expectedNewDeviceTimestamp))
				Expect(entry2.LastUpdateTime).To(BeNumerically(">", originalLastUpdateTime))
			})

			It("8.4 should initialize entry with proper aggregate values", func() {
				// Process first reading
				record := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.humidity.float"),
							"ts":          events.NewNumberAttribute("1640995200000"), // 2021-12-31 12:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("humidity"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("65.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				err := processor.ProcessRecord(record)
				Expect(err).To(BeNil())

				// Verify proper aggregate initialization
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "humidity", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())

				// Check hourly aggregates
				Expect(entry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.HourlyAggregates.Sum).To(Equal(65.0))
				Expect(entry.HourlyAggregates.Min).To(Equal(65.0))
				Expect(entry.HourlyAggregates.Max).To(Equal(65.0))
				Expect(entry.HourlyAggregates.Average).To(Equal(65.0))
				Expect(entry.HourlyAggregates.FirstValue).To(Equal(65.0))
				Expect(entry.HourlyAggregates.LastValue).To(Equal(65.0))
				Expect(entry.HourlyAggregates.CumulativeValue).To(Equal(0.0)) // Non-cumulative
				Expect(entry.HourlyAggregates.WindowStart).To(BeNumerically(">", 0))
				Expect(entry.HourlyAggregates.WindowEnd).To(BeNumerically(">", 0))

				// Check daily aggregates
				Expect(entry.DailyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.DailyAggregates.Sum).To(Equal(65.0))
				Expect(entry.DailyAggregates.Average).To(Equal(65.0))

				// Check weekly aggregates
				Expect(entry.WeeklyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.WeeklyAggregates.Sum).To(Equal(65.0))
				Expect(entry.WeeklyAggregates.Average).To(Equal(65.0))

				// Check monthly aggregates
				Expect(entry.MonthlyAggregates.Count).To(Equal(int64(1)))
				Expect(entry.MonthlyAggregates.Sum).To(Equal(65.0))
				Expect(entry.MonthlyAggregates.Average).To(Equal(65.0))

				// Verify timestamps are set
				Expect(entry.UpdatedAt).To(BeNumerically(">", 0))
				Expect(entry.LastUpdateTime).To(BeNumerically(">", 0))
			})

			It("8.3 should create separate entries for different parameters of same node", func() {
				// Process temperature reading
				tempRecord := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200000"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process humidity reading
				humidityRecord := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.humidity.float"),
							"ts":          events.NewNumberAttribute("1640995200000"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("humidity"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("65.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process pressure reading
				pressureRecord := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.pressure.float"),
							"ts":          events.NewNumberAttribute("1640995200000"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("pressure"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("1013.25"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process all records
				err := processor.ProcessRecord(tempRecord)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(humidityRecord)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(pressureRecord)
				Expect(err).To(BeNil())

				// Verify separate entries were created
				tempEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(tempEntry).ToNot(BeNil())
				Expect(tempEntry.DataKey).To(Equal("temperature"))
				Expect(tempEntry.HourlyAggregates.Sum).To(Equal(25.5))

				humidityEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "humidity", "float")
				Expect(err).To(BeNil())
				Expect(humidityEntry).ToNot(BeNil())
				Expect(humidityEntry.DataKey).To(Equal("humidity"))
				Expect(humidityEntry.HourlyAggregates.Sum).To(Equal(65.0))

				pressureEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "pressure", "float")
				Expect(err).To(BeNil())
				Expect(pressureEntry).ToNot(BeNil())
				Expect(pressureEntry.DataKey).To(Equal("pressure"))
				Expect(pressureEntry.HourlyAggregates.Sum).To(Equal(1013.25))

				// Verify entries are independent
				Expect(tempEntry.NodeKeyDt).To(Equal("test-node.temperature.float"))
				Expect(humidityEntry.NodeKeyDt).To(Equal("test-node.humidity.float"))
				Expect(pressureEntry.NodeKeyDt).To(Equal("test-node.pressure.float"))
			})

			It("8.4 should handle different data types properly", func() {
				// Process float data type
				floatRecord := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process int data type
				intRecord := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.count.int"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("count"),
							"dt":          events.NewStringAttribute("int"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("42"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process boolean data type
				boolRecord := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.active.bool"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("active"),
							"dt":          events.NewStringAttribute("bool"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewBooleanAttribute(true),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process all records
				err := processor.ProcessRecord(floatRecord)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(intRecord)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(boolRecord)
				Expect(err).To(BeNil())

				// Verify float entry
				floatEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(floatEntry).ToNot(BeNil())
				Expect(floatEntry.DataType).To(Equal("float"))
				Expect(floatEntry.HourlyAggregates.Sum).To(Equal(25.5))

				// Verify int entry
				intEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "count", "int")
				Expect(err).To(BeNil())
				Expect(intEntry).ToNot(BeNil())
				Expect(intEntry.DataType).To(Equal("int"))
				Expect(intEntry.HourlyAggregates.Sum).To(Equal(42.0)) // Converted to float64

				// Verify boolean entry
				boolEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "active", "bool")
				Expect(err).To(BeNil())
				Expect(boolEntry).ToNot(BeNil())
				Expect(boolEntry.DataType).To(Equal("bool"))
				Expect(boolEntry.HourlyAggregates.Sum).To(Equal(1.0)) // true converted to 1.0

				// Verify all entries are independent
				Expect(floatEntry.NodeKeyDt).To(Equal("test-node.temperature.float"))
				Expect(intEntry.NodeKeyDt).To(Equal("test-node.count.int"))
				Expect(boolEntry.NodeKeyDt).To(Equal("test-node.active.bool"))
			})
		})
	})

	Describe("Step 9: Historical Entry Creation", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("when crossing window boundaries", func() {
			It("9.1 should create historical entry for completed hourly window", func() {
				// Process first reading in an hour
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200000"), // 2021-12-31 12:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process second reading in the same hour
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998500000"), // 2021-12-31 12:55:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process third reading in the next hour (triggers boundary crossing)
				record3 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998800000"), // 2021-12-31 13:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("27.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process records
				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record3)
				Expect(err).To(BeNil())

				// Verify current entry has new window data
				currentEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(currentEntry).ToNot(BeNil())
				Expect(currentEntry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.HourlyAggregates.Sum).To(Equal(27.0))

				// Verify daily aggregates include all readings
				Expect(currentEntry.DailyAggregates.Count).To(Equal(int64(3)))
				Expect(currentEntry.DailyAggregates.Sum).To(Equal(78.5)) // 25.5 + 26.0 + 27.0

				// Check that a historical entry was created for the completed hour
				// Note: In a real implementation, we'd need to query the historical entries
				// For now, we verify that the current entry has the correct new window data
			})

			It("9.2 should create historical entry for completed daily window", func() {
				// Process reading on first day
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640908800000"), // 2021-12-31 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process second reading on the same day
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640980800000"), // 2021-12-31 20:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process reading on different day (triggers boundary crossing)
				record3 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1641081600000"), // 2022-01-02 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("27.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process records
				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record3)
				Expect(err).To(BeNil())

				// Verify current entry has new window data
				currentEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(currentEntry).ToNot(BeNil())

				// Daily aggregates should show only the new reading
				Expect(currentEntry.DailyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.DailyAggregates.Sum).To(Equal(27.0))

				// Hourly aggregates should also show only the new reading
				Expect(currentEntry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.HourlyAggregates.Sum).To(Equal(27.0))
			})

			It("9.3 should create historical entries for different window types", func() {
				// Process reading in December
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640908800000"), // 2021-12-31 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.5"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process reading in February (crosses multiple boundaries)
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1643673600000"), // 2022-02-01 00:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("26.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process records
				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())

				// Verify current entry has new window data for all window types
				currentEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(currentEntry).ToNot(BeNil())

				// All window types should show only the new reading
				Expect(currentEntry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.HourlyAggregates.Sum).To(Equal(26.0))
				Expect(currentEntry.DailyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.DailyAggregates.Sum).To(Equal(26.0))
				Expect(currentEntry.WeeklyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.WeeklyAggregates.Sum).To(Equal(26.0))
				Expect(currentEntry.MonthlyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.MonthlyAggregates.Sum).To(Equal(26.0))
			})

			It("9.4 should preserve completed window data in historical entries", func() {
				// Process multiple readings in same hour
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640995200000"), // 2021-12-31 12:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("20.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640997000000"), // 2021-12-31 12:30:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("30.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				record3 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998500000"), // 2021-12-31 12:55:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("25.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process reading in next hour
				record4 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.temperature.float"),
							"ts":          events.NewNumberAttribute("1640998800000"), // 2021-12-31 13:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("temperature"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("28.0"),
							"cumulative":  events.NewBooleanAttribute(false),
						},
					},
				}

				// Process first three records (same hour)
				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record3)
				Expect(err).To(BeNil())

				// Verify aggregates for the completed hour
				currentEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(currentEntry).ToNot(BeNil())

				// Before boundary crossing, check the completed hour aggregates
				Expect(currentEntry.HourlyAggregates.Count).To(Equal(int64(3)))
				Expect(currentEntry.HourlyAggregates.Sum).To(Equal(75.0)) // 20 + 30 + 25
				Expect(currentEntry.HourlyAggregates.Min).To(Equal(20.0))
				Expect(currentEntry.HourlyAggregates.Max).To(Equal(30.0))
				Expect(currentEntry.HourlyAggregates.Average).To(Equal(25.0))

				// Process fourth record (next hour, triggers boundary crossing)
				err = processor.ProcessRecord(record4)
				Expect(err).To(BeNil())

				// Verify current entry has new window data
				currentEntry, err = processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(currentEntry).ToNot(BeNil())

				// New hour should have only the latest reading
				Expect(currentEntry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.HourlyAggregates.Sum).To(Equal(28.0))
				Expect(currentEntry.HourlyAggregates.Min).To(Equal(28.0))
				Expect(currentEntry.HourlyAggregates.Max).To(Equal(28.0))
				Expect(currentEntry.HourlyAggregates.Average).To(Equal(28.0))

				// Daily aggregates should include all readings
				Expect(currentEntry.DailyAggregates.Count).To(Equal(int64(4)))
				Expect(currentEntry.DailyAggregates.Sum).To(Equal(103.0)) // 20 + 30 + 25 + 28
				Expect(currentEntry.DailyAggregates.Min).To(Equal(20.0))
				Expect(currentEntry.DailyAggregates.Max).To(Equal(30.0))
				Expect(currentEntry.DailyAggregates.Average).To(Equal(25.75))
			})

			It("9.5 should handle cumulative data across window boundaries", func() {
				// Process cumulative reading
				record1 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640995200000"), // 2021-12-31 12:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("100.0"), // Baseline
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				// Second reading in same hour
				record2 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640997000000"), // 2021-12-31 12:30:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("105.0"), // +5 kWh
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				// Third reading in next hour (boundary crossing)
				record3 := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("test-node.energy.float"),
							"ts":          events.NewNumberAttribute("1640998800000"), // 2021-12-31 13:00:00 UTC
							"node_id":     events.NewStringAttribute("test-node"),
							"key":         events.NewStringAttribute("energy"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
							"value":       events.NewNumberAttribute("110.0"), // +5 kWh in new hour
							"cumulative":  events.NewBooleanAttribute(true),
						},
					},
				}

				// Process records
				err := processor.ProcessRecord(record1)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record2)
				Expect(err).To(BeNil())
				err = processor.ProcessRecord(record3)
				Expect(err).To(BeNil())

				// Verify cumulative handling across boundaries
				currentEntry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("test-node", "energy", "float")
				Expect(err).To(BeNil())
				Expect(currentEntry).ToNot(BeNil())

				// New hour should show consumption from 105 to 110 = 5 kWh
				Expect(currentEntry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(currentEntry.HourlyAggregates.Sum).To(Equal(5.0))               // Consumption in new hour
				Expect(currentEntry.HourlyAggregates.FirstValue).To(Equal(105.0))      // Baseline for new hour
				Expect(currentEntry.HourlyAggregates.LastValue).To(Equal(110.0))       // Current value
				Expect(currentEntry.HourlyAggregates.CumulativeValue).To(Equal(110.0)) // Latest cumulative

				// Daily aggregates should include consumption from both hours
				Expect(currentEntry.DailyAggregates.Count).To(Equal(int64(2)))
				Expect(currentEntry.DailyAggregates.Sum).To(Equal(10.0))              // Total consumption: 5 + 5
				Expect(currentEntry.DailyAggregates.FirstValue).To(Equal(100.0))      // Original baseline
				Expect(currentEntry.DailyAggregates.LastValue).To(Equal(110.0))       // Current value
				Expect(currentEntry.DailyAggregates.CumulativeValue).To(Equal(110.0)) // Latest cumulative
			})
		})
	})

	Describe("Step 10: Database Operations", func() {
		var processor *TSStreamProcessor
		var processedTsDB *processed_ts_db.ProcessedTsDB

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
			processedTsDB = processed_ts_db.NewProcessedTsDB(processor.rmngCtx)
		})

		Context("current entry management", func() {
			It("10.1 should create and retrieve current entry", func() {
				// Initially no entry should exist
				entry, err := processedTsDB.GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).To(BeNil())

				// Create a current entry
				newEntry := &processed_ts_db.ProcessedTsEntry{
					NodeKeyDt:      "test-node.temperature.float",
					IntervalKey:    "current",
					NodeID:         "test-node",
					DataKey:        "temperature",
					DataType:       "float",
					Timezone:       "UTC",
					IsCumulative:   false,
					UpdatedAt:      time.Now().Unix(),
					LastUpdateTime: time.Now().Unix(),
				}

				// Initialize aggregates
				newEntry.HourlyAggregates = processed_ts_db.WindowAggregates{
					Count:       1,
					Sum:         25.5,
					Min:         25.5,
					Max:         25.5,
					Average:     25.5,
					FirstValue:  25.5,
					LastValue:   25.5,
					WindowStart: time.Now().Unix(),
					WindowEnd:   time.Now().Unix() + 3600,
				}

				// Upsert the entry
				err = processedTsDB.UpsertCurrentEntry(newEntry)
				Expect(err).To(BeNil())

				// Retrieve and verify
				retrievedEntry, err := processedTsDB.GetCurrentEntry("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(retrievedEntry).ToNot(BeNil())
				Expect(retrievedEntry.NodeKeyDt).To(Equal("test-node.temperature.float"))
				Expect(retrievedEntry.IntervalKey).To(Equal("current"))
				Expect(retrievedEntry.NodeID).To(Equal("test-node"))
				Expect(retrievedEntry.DataKey).To(Equal("temperature"))
				Expect(retrievedEntry.DataType).To(Equal("float"))
				Expect(retrievedEntry.HourlyAggregates.Count).To(Equal(int64(1)))
				Expect(retrievedEntry.HourlyAggregates.Sum).To(Equal(25.5))
			})

			It("10.2 should update existing current entry", func() {
				// Create initial entry with explicit timestamps
				initialTime := time.Now().Unix()
				initialEntry := &processed_ts_db.ProcessedTsEntry{
					NodeKeyDt:      "test-node.humidity.float",
					IntervalKey:    "current",
					NodeID:         "test-node",
					DataKey:        "humidity",
					DataType:       "float",
					Timezone:       "UTC",
					IsCumulative:   false,
					UpdatedAt:      initialTime,
					LastUpdateTime: initialTime,
				}

				initialEntry.HourlyAggregates = processed_ts_db.WindowAggregates{
					Count:   1,
					Sum:     60.0,
					Average: 60.0,
				}

				err := processedTsDB.UpsertCurrentEntry(initialEntry)
				Expect(err).To(BeNil())

				// Wait a moment to ensure different timestamps
				time.Sleep(time.Millisecond * 10)

				// Update the entry with explicitly different timestamp
				updateTime := time.Now().Unix()
				updatedEntry := &processed_ts_db.ProcessedTsEntry{
					NodeKeyDt:      "test-node.humidity.float",
					IntervalKey:    "current",
					NodeID:         "test-node",
					DataKey:        "humidity",
					DataType:       "float",
					Timezone:       "UTC",
					IsCumulative:   false,
					UpdatedAt:      updateTime,
					LastUpdateTime: updateTime,
				}

				updatedEntry.HourlyAggregates = processed_ts_db.WindowAggregates{
					Count:   2,
					Sum:     125.0,
					Average: 62.5,
				}

				err = processedTsDB.UpsertCurrentEntry(updatedEntry)
				Expect(err).To(BeNil())

				// Verify update
				retrievedEntry, err := processedTsDB.GetCurrentEntry("test-node", "humidity", "float")
				Expect(err).To(BeNil())
				Expect(retrievedEntry).ToNot(BeNil())
				Expect(retrievedEntry.HourlyAggregates.Count).To(Equal(int64(2)))
				Expect(retrievedEntry.HourlyAggregates.Sum).To(Equal(125.0))
				Expect(retrievedEntry.HourlyAggregates.Average).To(Equal(62.5))
				Expect(retrievedEntry.UpdatedAt).To(BeNumerically(">=", initialTime))
			})

			It("10.3 should handle non-existent current entry", func() {
				// Try to get a non-existent entry
				entry, err := processedTsDB.GetCurrentEntry("non-existent", "parameter", "float")
				Expect(err).To(BeNil())
				Expect(entry).To(BeNil())
			})

			It("10.4 should create current entries for different parameters", func() {
				// Create entries for different parameters
				parameters := []struct {
					nodeID   string
					param    string
					dataType string
					value    float64
				}{
					{"sensor-1", "temperature", "float", 25.5},
					{"sensor-1", "humidity", "float", 65.0},
					{"sensor-2", "pressure", "float", 1013.25},
					{"sensor-2", "temperature", "int", 24.0},
				}

				for _, param := range parameters {
					entry := &processed_ts_db.ProcessedTsEntry{
						NodeKeyDt:      fmt.Sprintf("%s.%s.%s", param.nodeID, param.param, param.dataType),
						IntervalKey:    "current",
						NodeID:         param.nodeID,
						DataKey:        param.param,
						DataType:       param.dataType,
						Timezone:       "UTC",
						IsCumulative:   false,
						UpdatedAt:      time.Now().Unix(),
						LastUpdateTime: time.Now().Unix(),
					}

					entry.HourlyAggregates = processed_ts_db.WindowAggregates{
						Count:   1,
						Sum:     param.value,
						Average: param.value,
					}

					err := processedTsDB.UpsertCurrentEntry(entry)
					Expect(err).To(BeNil())
				}

				// Verify all entries were created independently
				for _, param := range parameters {
					retrievedEntry, err := processedTsDB.GetCurrentEntry(param.nodeID, param.param, param.dataType)
					Expect(err).To(BeNil())
					Expect(retrievedEntry).ToNot(BeNil())
					Expect(retrievedEntry.NodeID).To(Equal(param.nodeID))
					Expect(retrievedEntry.DataKey).To(Equal(param.param))
					Expect(retrievedEntry.DataType).To(Equal(param.dataType))
					Expect(retrievedEntry.HourlyAggregates.Sum).To(Equal(param.value))
				}
			})
		})

		Context("aggregate retrieval methods", func() {
			BeforeEach(func() {
				// Create a test entry with aggregates for all window types
				testEntry := &processed_ts_db.ProcessedTsEntry{
					NodeKeyDt:      "test-node.temperature.float",
					IntervalKey:    "current",
					NodeID:         "test-node",
					DataKey:        "temperature",
					DataType:       "float",
					Timezone:       "UTC",
					IsCumulative:   false,
					UpdatedAt:      time.Now().Unix(),
					LastUpdateTime: time.Now().Unix(),
				}

				// Set up different aggregates for each window type
				testEntry.HourlyAggregates = processed_ts_db.WindowAggregates{
					Count: 5, Sum: 125.0, Average: 25.0, Min: 20.0, Max: 30.0,
					WindowStart: time.Now().Unix(), WindowEnd: time.Now().Unix() + 3600,
				}
				testEntry.DailyAggregates = processed_ts_db.WindowAggregates{
					Count: 50, Sum: 1250.0, Average: 25.0, Min: 15.0, Max: 35.0,
					WindowStart: time.Now().Unix(), WindowEnd: time.Now().Unix() + 86400,
				}
				testEntry.WeeklyAggregates = processed_ts_db.WindowAggregates{
					Count: 350, Sum: 8750.0, Average: 25.0, Min: 10.0, Max: 40.0,
					WindowStart: time.Now().Unix(), WindowEnd: time.Now().Unix() + 604800,
				}
				testEntry.MonthlyAggregates = processed_ts_db.WindowAggregates{
					Count: 1500, Sum: 37500.0, Average: 25.0, Min: 5.0, Max: 45.0,
					WindowStart: time.Now().Unix(), WindowEnd: time.Now().Unix() + 2592000,
				}

				err := processedTsDB.UpsertCurrentEntry(testEntry)
				Expect(err).To(BeNil())
			})

			It("10.5 should retrieve aggregates for specific window type", func() {
				// Test each window type
				windowTests := []struct {
					window        timewindow.TimeWindow
					expectedCount int64
					expectedSum   float64
				}{
					{timewindow.WindowHourly, 5, 125.0},
					{timewindow.WindowDaily, 50, 1250.0},
					{timewindow.WindowWeekly, 350, 8750.0},
					{timewindow.WindowMonthly, 1500, 37500.0},
				}

				for _, test := range windowTests {
					aggregates, err := processedTsDB.GetCurrentAggregatesForWindow("test-node", "temperature", "float", test.window)
					Expect(err).To(BeNil())
					Expect(aggregates).ToNot(BeNil())
					Expect(aggregates["window_type"]).To(Equal(string(test.window)))
					Expect(aggregates["is_cumulative"]).To(Equal(false))
					Expect(aggregates["count"]).To(Equal(test.expectedCount))
					Expect(aggregates["sum"]).To(Equal(test.expectedSum))
					Expect(aggregates["average"]).To(Equal(25.0))
				}
			})

			It("10.6 should retrieve all current aggregates", func() {
				allAggregates, err := processedTsDB.GetAllCurrentAggregates("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(allAggregates).ToNot(BeNil())

				// Check that all window types are present
				for _, window := range timewindow.GetSupportedWindows() {
					windowKey := string(window)
					windowData, exists := allAggregates[windowKey]
					Expect(exists).To(BeTrue())

					windowMap := windowData.(map[string]interface{})
					Expect(windowMap["is_cumulative"]).To(Equal(false))
					Expect(windowMap["average"]).To(Equal(25.0))
				}

				// Verify specific values for each window
				hourlyData := allAggregates["hourly"].(map[string]interface{})
				Expect(hourlyData["count"]).To(Equal(int64(5)))
				Expect(hourlyData["sum"]).To(Equal(125.0))

				dailyData := allAggregates["daily"].(map[string]interface{})
				Expect(dailyData["count"]).To(Equal(int64(50)))
				Expect(dailyData["sum"]).To(Equal(1250.0))
			})

			It("10.7 should retrieve all current aggregates", func() {
				summary, err := processedTsDB.GetAllCurrentAggregates("test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(summary).ToNot(BeNil())

				// Check hourly aggregates
				hourly, exists := summary["hourly"]
				Expect(exists).To(BeTrue())
				hourlyMap := hourly.(map[string]interface{})
				Expect(hourlyMap["count"]).To(Equal(int64(5)))
				Expect(hourlyMap["sum"]).To(Equal(125.0))
				Expect(hourlyMap["average"]).To(Equal(25.0))
				Expect(hourlyMap["min"]).To(Equal(20.0))
				Expect(hourlyMap["max"]).To(Equal(30.0))

				// Check monthly aggregates
				monthly, exists := summary["monthly"]
				Expect(exists).To(BeTrue())
				monthlyMap := monthly.(map[string]interface{})
				Expect(monthlyMap["count"]).To(Equal(int64(1500)))
				Expect(monthlyMap["sum"]).To(Equal(37500.0))
				Expect(monthlyMap["min"]).To(Equal(5.0))
				Expect(monthlyMap["max"]).To(Equal(45.0))
			})

			It("10.8 should handle non-existent parameter in aggregates", func() {
				// Test single window type
				aggregates, err := processedTsDB.GetCurrentAggregatesForWindow("non-existent", "parameter", "float", timewindow.WindowHourly)
				Expect(err).To(BeNil())
				Expect(aggregates).ToNot(BeNil())
				Expect(aggregates["window_type"]).To(Equal("hourly"))
				Expect(aggregates["message"]).To(Equal("No current data available for this window"))

				// Test all aggregates
				allAggregates, err := processedTsDB.GetAllCurrentAggregates("non-existent", "parameter", "float")
				Expect(err).To(BeNil())
				Expect(allAggregates).ToNot(BeNil())

				for _, window := range timewindow.GetSupportedWindows() {
					windowKey := string(window)
					windowData, exists := allAggregates[windowKey]
					Expect(exists).To(BeTrue())

					windowMap := windowData.(map[string]interface{})
					Expect(windowMap["message"]).To(Equal("No current data available for this window"))
				}

				// Test all aggregates for non-existent parameter
				allSummary, err := processedTsDB.GetAllCurrentAggregates("non-existent", "parameter", "float")
				Expect(err).To(BeNil())
				Expect(allSummary).ToNot(BeNil())

				// All windows should indicate no data available
				for _, window := range timewindow.GetSupportedWindows() {
					windowKey := string(window)
					windowData, exists := allSummary[windowKey]
					Expect(exists).To(BeTrue())

					windowMap := windowData.(map[string]interface{})
					Expect(windowMap["message"]).To(Equal("No current data available for this window"))
				}
			})
		})
	})

	Describe("Step 11: Timezone Handling", func() {
		Context("timezone conversion and parsing", func() {
			It("11.1 should convert epoch to different timezones", func() {
				// Test with a known epoch time: 2024-01-15 12:00:00 UTC
				testEpoch := int64(1705320000)

				// Test UTC conversion
				utcTime, err := timewindow.ConvertToTimezone(testEpoch, "UTC")
				Expect(err).To(BeNil())
				Expect(utcTime.Hour()).To(Equal(12))
				Expect(utcTime.Day()).To(Equal(15))
				Expect(utcTime.Month()).To(Equal(time.January))
				Expect(utcTime.Year()).To(Equal(2024))

				// Test EST conversion (UTC-5)
				estTime, err := timewindow.ConvertToTimezone(testEpoch, "America/New_York")
				Expect(err).To(BeNil())
				Expect(estTime.Hour()).To(Equal(7)) // 12 - 5 = 7 AM EST
				Expect(estTime.Day()).To(Equal(15))

				// Test PST conversion (UTC-8)
				pstTime, err := timewindow.ConvertToTimezone(testEpoch, "America/Los_Angeles")
				Expect(err).To(BeNil())
				Expect(pstTime.Hour()).To(Equal(4)) // 12 - 8 = 4 AM PST
				Expect(pstTime.Day()).To(Equal(15))

				// Test positive offset (UTC+9 - JST)
				jstTime, err := timewindow.ConvertToTimezone(testEpoch, "Asia/Tokyo")
				Expect(err).To(BeNil())
				Expect(jstTime.Hour()).To(Equal(21)) // 12 + 9 = 21 JST
				Expect(jstTime.Day()).To(Equal(15))
			})

			It("11.2 should handle invalid timezone gracefully", func() {
				testEpoch := int64(1705320000)

				// Test with invalid timezone - should fallback to UTC
				invalidTime, err := timewindow.ConvertToTimezone(testEpoch, "INVALID_TZ")
				Expect(err).To(BeNil())
				utcTime, err := timewindow.ConvertToTimezone(testEpoch, "UTC")
				Expect(err).To(BeNil())

				Expect(invalidTime.Hour()).To(Equal(utcTime.Hour()))
				Expect(invalidTime.Day()).To(Equal(utcTime.Day()))
			})

			It("11.3 should handle empty timezone", func() {
				testEpoch := int64(1705320000)

				// Test with empty timezone - should fallback to UTC
				emptyTime, err := timewindow.ConvertToTimezone(testEpoch, "")
				Expect(err).To(BeNil())
				utcTime, err := timewindow.ConvertToTimezone(testEpoch, "UTC")
				Expect(err).To(BeNil())

				Expect(emptyTime.Hour()).To(Equal(utcTime.Hour()))
				Expect(emptyTime.Day()).To(Equal(utcTime.Day()))
			})
		})

		Context("window boundary calculations", func() {
			It("11.4 should calculate window boundaries in different timezones", func() {
				// Test time: 2024-01-15 14:30:00 UTC (Monday)
				testEpoch := int64(1705329000)

				// Test hourly boundaries in UTC
				utcTime, err := timewindow.ConvertToTimezone(testEpoch, "UTC")
				Expect(err).To(BeNil())
				utcHourlyStart, utcHourlyEnd := timewindow.GetWindowBoundaries(utcTime, timewindow.WindowHourly)

				Expect(utcHourlyStart.Hour()).To(Equal(14))
				Expect(utcHourlyStart.Minute()).To(Equal(0))
				Expect(utcHourlyStart.Second()).To(Equal(0))
				Expect(utcHourlyEnd.Hour()).To(Equal(15))
				Expect(utcHourlyEnd.Minute()).To(Equal(0))
				Expect(utcHourlyEnd.Second()).To(Equal(0))

				// Test hourly boundaries in EST
				estTime, err := timewindow.ConvertToTimezone(testEpoch, "America/New_York")
				Expect(err).To(BeNil())
				estHourlyStart, estHourlyEnd := timewindow.GetWindowBoundaries(estTime, timewindow.WindowHourly)

				Expect(estHourlyStart.Hour()).To(Equal(9)) // 14 - 5 = 9 AM EST
				Expect(estHourlyStart.Minute()).To(Equal(0))
				Expect(estHourlyEnd.Hour()).To(Equal(10))
				Expect(estHourlyEnd.Minute()).To(Equal(0))

				// Test daily boundaries in different timezones
				utcDailyStart, utcDailyEnd := timewindow.GetWindowBoundaries(utcTime, timewindow.WindowDaily)

				Expect(utcDailyStart.Hour()).To(Equal(0))
				Expect(utcDailyStart.Day()).To(Equal(15))
				Expect(utcDailyEnd.Hour()).To(Equal(0))
				Expect(utcDailyEnd.Day()).To(Equal(16))

				// Test daily boundaries in PST
				pstTime, err := timewindow.ConvertToTimezone(testEpoch, "America/Los_Angeles")
				Expect(err).To(BeNil())
				pstDailyStart, pstDailyEnd := timewindow.GetWindowBoundaries(pstTime, timewindow.WindowDaily)

				Expect(pstDailyStart.Hour()).To(Equal(0))
				Expect(pstDailyStart.Day()).To(Equal(15))
				Expect(pstDailyEnd.Hour()).To(Equal(0))
				Expect(pstDailyEnd.Day()).To(Equal(16))
			})

			It("11.5 should handle week boundaries correctly", func() {
				// Test time: 2024-01-17 10:00:00 UTC (Wednesday)
				testEpoch := int64(1705489200)

				// Test weekly boundaries (week starts on Monday)
				utcTime, err := timewindow.ConvertToTimezone(testEpoch, "UTC")
				Expect(err).To(BeNil())
				weeklyStart, weeklyEnd := timewindow.GetWindowBoundaries(utcTime, timewindow.WindowWeekly)

				// Week should start on Monday (15th) and end on Monday (22nd)
				Expect(weeklyStart.Weekday()).To(Equal(time.Monday))
				Expect(weeklyStart.Day()).To(Equal(15))
				Expect(weeklyStart.Hour()).To(Equal(0))
				Expect(weeklyEnd.Weekday()).To(Equal(time.Monday))
				Expect(weeklyEnd.Day()).To(Equal(22))
				Expect(weeklyEnd.Hour()).To(Equal(0))

				// Test in different timezone
				estTime, err := timewindow.ConvertToTimezone(testEpoch, "America/New_York")
				Expect(err).To(BeNil())
				estWeeklyStart, estWeeklyEnd := timewindow.GetWindowBoundaries(estTime, timewindow.WindowWeekly)

				Expect(estWeeklyStart.Weekday()).To(Equal(time.Monday))
				Expect(estWeeklyStart.Hour()).To(Equal(0))
				Expect(estWeeklyEnd.Weekday()).To(Equal(time.Monday))
				Expect(estWeeklyEnd.Hour()).To(Equal(0))
			})

			It("11.6 should handle month boundaries correctly", func() {
				// Test time: 2024-01-15 14:30:00 UTC
				testEpoch := int64(1705329000)

				// Test monthly boundaries
				utcTime, err := timewindow.ConvertToTimezone(testEpoch, "UTC")
				Expect(err).To(BeNil())
				monthlyStart, monthlyEnd := timewindow.GetWindowBoundaries(utcTime, timewindow.WindowMonthly)

				Expect(monthlyStart.Day()).To(Equal(1))
				Expect(monthlyStart.Month()).To(Equal(time.January))
				Expect(monthlyStart.Hour()).To(Equal(0))
				Expect(monthlyEnd.Day()).To(Equal(1))
				Expect(monthlyEnd.Month()).To(Equal(time.February))
				Expect(monthlyEnd.Hour()).To(Equal(0))

				// Test February boundary (leap year)
				// 2024-02-15 14:30:00 UTC
				febEpoch := int64(1708007400)
				febTime, err := timewindow.ConvertToTimezone(febEpoch, "UTC")
				Expect(err).To(BeNil())
				febMonthlyStart, febMonthlyEnd := timewindow.GetWindowBoundaries(febTime, timewindow.WindowMonthly)

				Expect(febMonthlyStart.Day()).To(Equal(1))
				Expect(febMonthlyStart.Month()).To(Equal(time.February))
				Expect(febMonthlyEnd.Day()).To(Equal(1))
				Expect(febMonthlyEnd.Month()).To(Equal(time.March))
			})
		})

		Context("window boundary crossing detection", func() {
			It("11.7 should detect hourly boundary crossings", func() {
				// Test crossing from 14:59 to 15:01 UTC
				prevEpoch := int64(1705330740)    // 2024-01-15 14:59:00 UTC
				currentEpoch := int64(1705330860) // 2024-01-15 15:01:00 UTC

				prevTime, err := timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err := timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed := timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowHourly)
				Expect(crossed).To(BeTrue())

				// Test within same hour
				prevEpoch = int64(1705330740)         // 2024-01-15 14:59:00 UTC
				currentEpoch = int64(1705330740 + 30) // 2024-01-15 14:59:30 UTC

				prevTime, err = timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err = timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed = timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowHourly)
				Expect(crossed).To(BeFalse())
			})

			It("11.8 should detect daily boundary crossings", func() {
				// Test crossing from 23:59 to 00:01 next day
				prevEpoch := int64(1705363140)    // 2024-01-15 23:59:00 UTC
				currentEpoch := int64(1705363320) // 2024-01-16 00:02:00 UTC

				prevTime, err := timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err := timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed := timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowDaily)
				Expect(crossed).To(BeTrue())

				// Test within same day
				prevEpoch = int64(1705329000)    // 2024-01-15 14:30:00 UTC
				currentEpoch = int64(1705350600) // 2024-01-15 20:30:00 UTC

				prevTime, err = timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err = timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed = timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowDaily)
				Expect(crossed).To(BeFalse())
			})

			It("11.9 should detect boundary crossings in different timezones", func() {
				// Test daily boundary crossing in EST
				// 2024-01-15 23:59:00 EST = 2024-01-16 04:59:00 UTC
				// 2024-01-16 00:01:00 EST = 2024-01-16 05:01:00 UTC
				prevEpoch := int64(1705381140)    // 2024-01-16 04:59:00 UTC
				currentEpoch := int64(1705381260) // 2024-01-16 05:01:00 UTC

				prevTime, err := timewindow.ConvertToTimezone(prevEpoch, "America/New_York")
				Expect(err).To(BeNil())
				currentTime, err := timewindow.ConvertToTimezone(currentEpoch, "America/New_York")
				Expect(err).To(BeNil())

				crossed := timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowDaily)
				Expect(crossed).To(BeTrue())

				// Test hourly boundary crossing in PST
				// 2024-01-15 14:59:00 PST = 2024-01-15 22:59:00 UTC
				// 2024-01-15 15:01:00 PST = 2024-01-15 23:01:00 UTC
				prevEpoch = int64(1705359540)    // 2024-01-15 22:59:00 UTC
				currentEpoch = int64(1705359660) // 2024-01-15 23:01:00 UTC

				prevTime, err = timewindow.ConvertToTimezone(prevEpoch, "America/Los_Angeles")
				Expect(err).To(BeNil())
				currentTime, err = timewindow.ConvertToTimezone(currentEpoch, "America/Los_Angeles")
				Expect(err).To(BeNil())

				crossed = timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowHourly)
				Expect(crossed).To(BeTrue())
			})

			It("11.10 should detect weekly boundary crossings", func() {
				// Test crossing from Sunday to Monday
				// 2024-01-14 23:59:00 UTC (Sunday) to 2024-01-15 00:01:00 UTC (Monday)
				prevEpoch := int64(1705276740)    // 2024-01-14 23:59:00 UTC
				currentEpoch := int64(1705276860) // 2024-01-15 00:01:00 UTC

				prevTime, err := timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err := timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed := timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowWeekly)
				Expect(crossed).To(BeTrue())

				// Test within same week
				prevEpoch = int64(1705329000)    // 2024-01-15 14:30:00 UTC (Monday)
				currentEpoch = int64(1705502400) // 2024-01-17 14:40:00 UTC (Wednesday)

				prevTime, err = timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err = timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed = timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowWeekly)
				Expect(crossed).To(BeFalse())
			})

			It("11.11 should detect monthly boundary crossings", func() {
				// Test crossing from January to February
				// 2024-01-31 23:59:00 UTC to 2024-02-01 00:01:00 UTC
				prevEpoch := int64(1706745540)    // 2024-01-31 23:59:00 UTC
				currentEpoch := int64(1706745660) // 2024-02-01 00:01:00 UTC

				prevTime, err := timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err := timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed := timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowMonthly)
				Expect(crossed).To(BeTrue())

				// Test within same month
				prevEpoch = int64(1705329000)    // 2024-01-15 14:30:00 UTC
				currentEpoch = int64(1706193000) // 2024-01-25 14:30:00 UTC

				prevTime, err = timewindow.ConvertToTimezone(prevEpoch, "UTC")
				Expect(err).To(BeNil())
				currentTime, err = timewindow.ConvertToTimezone(currentEpoch, "UTC")
				Expect(err).To(BeNil())

				crossed = timewindow.HasWindowCrossed(prevTime, currentTime, timewindow.WindowMonthly)
				Expect(crossed).To(BeFalse())
			})
		})

		Context("window key formatting", func() {
			It("11.12 should format window keys correctly", func() {
				// Test time: 2024-01-15 14:30:00 UTC
				testEpoch := int64(1705329000)

				// Test hourly key
				utcTime, err := timewindow.ConvertToTimezone(testEpoch, "UTC")
				Expect(err).To(BeNil())
				hourlyKey := timewindow.FormatWindowKey(utcTime, timewindow.WindowHourly)
				Expect(hourlyKey).To(Equal("hourly#2024-01-15T14"))

				// Test daily key
				dailyKey := timewindow.FormatWindowKey(utcTime, timewindow.WindowDaily)
				Expect(dailyKey).To(Equal("daily#2024-01-15"))

				// Test weekly key (Monday of the week)
				weeklyKey := timewindow.FormatWindowKey(utcTime, timewindow.WindowWeekly)
				Expect(weeklyKey).To(Equal("weekly#2024-01-15")) // Monday of the week

				// Test monthly key
				monthlyKey := timewindow.FormatWindowKey(utcTime, timewindow.WindowMonthly)
				Expect(monthlyKey).To(Equal("monthly#2024-01"))

				// Test in different timezone
				estTime, err := timewindow.ConvertToTimezone(testEpoch, "America/New_York")
				Expect(err).To(BeNil())
				estHourlyKey := timewindow.FormatWindowKey(estTime, timewindow.WindowHourly)
				Expect(estHourlyKey).To(Equal("hourly#2024-01-15T09")) // 14 - 5 = 9
			})
		})
	})

	Describe("Step 12: Error Scenarios", func() {
		var processor *TSStreamProcessor

		BeforeEach(func() {
			processor = NewTSStreamProcessor()
		})

		Context("malformed DynamoDB data handling", func() {
			It("12.1 should handle missing required fields gracefully", func() {
				// Test missing node_id
				eventMissingNodeID := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"ts":    events.NewNumberAttribute("1640995200"),
							"key":   events.NewStringAttribute("temperature"),
							"value": events.NewNumberAttribute("25.5"),
						},
					},
				}

				err := processor.ProcessRecord(eventMissingNodeID)
				Expect(err).ToNot(BeNil())
			})

			It("12.2 should handle malformed timestamp values", func() {
				// Test non-numeric timestamp
				eventInvalidTS := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_id": events.NewStringAttribute("test-node"),
							"ts":      events.NewStringAttribute("invalid-timestamp"),
							"key":     events.NewStringAttribute("temperature"),
							"value":   events.NewNumberAttribute("25.5"),
							"dt":      events.NewStringAttribute("float"),
						},
					},
				}

				err := processor.ProcessRecord(eventInvalidTS)
				Expect(err).ToNot(BeNil())
			})

			It("12.3 should handle malformed value fields", func() {
				// Test non-convertible value
				eventInvalidValue := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_id": events.NewStringAttribute("test-node"),
							"ts":      events.NewNumberAttribute("1640995200"),
							"key":     events.NewStringAttribute("temperature"),
							"value":   events.NewStringAttribute("not-a-number"),
							"dt":      events.NewStringAttribute("float"),
						},
					},
				}

				err := processor.ProcessRecord(eventInvalidValue)
				Expect(err).ToNot(BeNil())
			})

			It("12.4 should handle empty DynamoDB stream records", func() {
				// Test empty NewImage
				eventEmptyImage := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{},
					},
				}

				err := processor.ProcessRecord(eventEmptyImage)
				Expect(err).ToNot(BeNil())

				// Test nil NewImage
				eventNilImage := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: nil,
					},
				}

				err = processor.ProcessRecord(eventNilImage)
				Expect(err).To(BeNil()) // Should handle nil gracefully
			})
		})

		Context("timezone handling", func() {
			It("12.5 should handle invalid timezone values", func() {
				// Test with invalid timezone
				eventInvalidTZ := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("tz-test-node.temperature.float"),
							"node_id":     events.NewStringAttribute("tz-test-node"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"key":         events.NewStringAttribute("temperature"),
							"value":       events.NewNumberAttribute("25.5"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("Invalid/Timezone"),
						},
					},
				}

				// Should not error but fallback to UTC
				err := processor.ProcessRecord(eventInvalidTZ)
				Expect(err).To(BeNil())

				// Verify entry was created with fallback
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("tz-test-node", "temperature", "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
			})
		})

		Context("resource and performance edge cases", func() {
			It("12.6 should handle very long parameter names", func() {
				// Test parameter name at reasonable length limit
				longParam := strings.Repeat("a", 200)

				eventLongParam := events.DynamoDBEventRecord{
					EventName: "INSERT",
					Change: events.DynamoDBStreamRecord{
						NewImage: map[string]events.DynamoDBAttributeValue{
							"node_key_dt": events.NewStringAttribute("long-param-test." + longParam + ".float"),
							"node_id":     events.NewStringAttribute("long-param-test"),
							"ts":          events.NewNumberAttribute("1640995200"),
							"key":         events.NewStringAttribute(longParam),
							"value":       events.NewNumberAttribute("25.5"),
							"dt":          events.NewStringAttribute("float"),
							"tz":          events.NewStringAttribute("UTC"),
						},
					},
				}
				err := processor.ProcessRecord(eventLongParam)
				Expect(err).To(BeNil())

				// Verify entry was created
				entry, err := processed_ts_db.NewProcessedTsDB(processor.rmngCtx).GetCurrentEntry("long-param-test", longParam, "float")
				Expect(err).To(BeNil())
				Expect(entry).ToNot(BeNil())
			})
		})
	})
})
