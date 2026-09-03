package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	db "github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestTimeseriesIngestHandler(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Timeseries Ingest Suite")
}

var _ = ginkgo.Describe("Timeseries ingestion", func() {
	ginkgo.BeforeEach(func() {
		test_utils.TestSetup()
	})

	ginkgo.It("fans out a batch payload into independently queryable points", func() {
		event := TimeseriesIngestEvent{
			NodeID: "node-1",
			Payload: json.RawMessage(`{"data":[
				{"k":"temperature","dt":"float","t":1743656583,"v":25.5,"tz":"UTC"},
				{"k":"humidity","dt":"int","t":1743656583,"v":60},
				{"k":"online","dt":"bool","t":1743656583,"v":true},
				{"k":"mode","dt":"string","t":1743656583,"v":"auto"}
			]}`),
		}

		gomega.Expect(handleTimeseriesIngest(context.Background(), event)).To(gomega.Succeed())
		temperature := getStoredPoint("node-1", "temperature", "float")
		gomega.Expect(temperature.Value).To(gomega.Equal(25.5))
		gomega.Expect(temperature.Timestamp).To(gomega.Equal(int64(1743656583)))
		gomega.Expect(getStoredPoint("node-1", "humidity", "int").Value).To(gomega.Equal(float64(60)))
		gomega.Expect(getStoredPoint("node-1", "online", "bool").Value).To(gomega.Equal(true))
		gomega.Expect(getStoredPoint("node-1", "mode", "string").Value).To(gomega.Equal("auto"))
	})

	ginkgo.It("validates the entire batch before writing any points", func() {
		event := TimeseriesIngestEvent{
			NodeID: "node-1",
			Payload: json.RawMessage(`{"data":[
				{"k":"temperature","dt":"float","t":1743656583,"v":25.5},
				{"k":"humidity","dt":"int","t":1743656583,"v":"invalid"}
			]}`),
		}

		err := handleTimeseriesIngest(context.Background(), event)
		gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("invalid data point at index 1")))
		gomega.Expect(queryStoredPoint("node-1", "temperature", "float")).To(gomega.BeNil())
	})

	ginkgo.It("rejects an empty batch", func() {
		err := handleTimeseriesIngest(context.Background(), TimeseriesIngestEvent{
			NodeID:  "node-empty",
			Payload: json.RawMessage(`{"data":[]}`),
		})
		gomega.Expect(err).To(gomega.MatchError("batch data must contain at least one point"))
	})

	ginkgo.It("accepts batches larger than the firmware-side recommendation", func() {
		points := make([]map[string]interface{}, 101)
		for i := range points {
			points[i] = map[string]interface{}{"k": fmt.Sprintf("point-%d", i), "dt": "int", "t": i + 1, "v": i}
		}
		payload, err := json.Marshal(map[string]interface{}{"data": points})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(handleTimeseriesIngest(context.Background(), TimeseriesIngestEvent{
			NodeID:  "node-large-batch",
			Payload: payload,
		})).To(gomega.Succeed())
		gomega.Expect(getStoredPoint("node-large-batch", "point-100", "int").Value).To(gomega.Equal(float64(100)))
	})

	ginkgo.It("rejects a single-point payload on the batch topic", func() {
		err := handleTimeseriesIngest(context.Background(), TimeseriesIngestEvent{
			NodeID:  "node-single",
			Payload: json.RawMessage(`{"k":"temperature","dt":"float","t":1743656583,"v":25.5}`),
		})
		gomega.Expect(err).To(gomega.MatchError("batch data must contain at least one point"))
	})

	ginkgo.It("processes SQS records and reports only failed messages", func() {
		validEvent := TimeseriesIngestEvent{
			NodeID:  "node-sqs",
			Payload: json.RawMessage(`{"data":[{"k":"temperature","dt":"float","t":1743656583,"v":25.5}]}`),
		}
		validBody, err := json.Marshal(validEvent)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		response, err := handleSQSBatch(context.Background(), events.SQSEvent{
			Records: []events.SQSMessage{
				{MessageId: "valid", Body: string(validBody)},
				{MessageId: "invalid", Body: `{"node_id":"node-sqs","payload":{"data":[]}}`},
			},
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(response.BatchItemFailures).To(gomega.Equal([]events.SQSBatchItemFailure{
			{ItemIdentifier: "invalid"},
		}))
		gomega.Expect(getStoredPoint("node-sqs", "temperature", "float").Value).To(gomega.Equal(25.5))
	})

	ginkgo.It("dispatches direct and SQS payload shapes", func() {
		direct, err := json.Marshal(TimeseriesIngestEvent{
			NodeID:  "node-direct",
			Payload: json.RawMessage(`{"data":[{"k":"power","dt":"int","t":1743656583,"v":10}]}`),
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		_, err = handle(context.Background(), direct)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		sqsRaw, err := json.Marshal(events.SQSEvent{Records: []events.SQSMessage{}})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		response, err := handle(context.Background(), sqsRaw)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(response).To(gomega.Equal(events.SQSEventResponse{}))
	})
})

func queryStoredPoint(nodeID, key, dataType string) *db.TimeseriesEntry {
	rmngCtx := rmngctx.NewRmngContext(utils.NewSystemActor())
	entry, err := db.NewTimeseriesDB(rmngCtx).GetLatestTimeseriesData(nodeID, key, dataType)
	if err != nil {
		return nil
	}
	return entry
}

func getStoredPoint(nodeID, key, dataType string) *db.TimeseriesEntry {
	entry := queryStoredPoint(nodeID, key, dataType)
	gomega.Expect(entry).ToNot(gomega.BeNil())
	return entry
}
