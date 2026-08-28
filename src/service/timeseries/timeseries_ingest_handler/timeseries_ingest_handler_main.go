package main

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// TimeseriesIngestEvent is produced by node_ts_batch_rule for messages on the
// dedicated rainmaker/nodes/{nodeID}/ts/{groupInfo}/batch topic.
type TimeseriesIngestEvent struct {
	NodeID    string          `json:"node_id"`
	TopicName string          `json:"topic_name"`
	Payload   json.RawMessage `json:"payload"`
}

func decodeTimeseriesPayload(payload json.RawMessage) (*timeseries.TimeseriesBatchPayload, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil, rmerror.NewRMError(nil, "missing payload")
	}

	var batch timeseries.TimeseriesBatchPayload
	if err := json.Unmarshal(payload, &batch); err != nil {
		return nil, rmerror.NewRMError(err, "invalid batch payload")
	}
	return &batch, nil
}

func handleTimeseriesIngest(ctx context.Context, event TimeseriesIngestEvent) error {
	payload, err := decodeTimeseriesPayload(event.Payload)
	if err != nil {
		return err
	}
	payload.TopicName = event.TopicName

	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
	return timeseries.NewTimeseriesService().Put(rmngCtx, event.NodeID, payload)
}

// handle accepts both the direct IoT-rule payload and an SQS event so the
// rule action can be changed at runtime without redeploying the function.
func handle(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	if awscommon.IsSQSEvent(raw) {
		var sqsEvent events.SQSEvent
		if err := json.Unmarshal(raw, &sqsEvent); err != nil {
			return nil, rmerror.NewRMError(err, "failed to unmarshal SQS event")
		}
		return handleSQSBatch(ctx, sqsEvent)
	}

	var event TimeseriesIngestEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal timeseries ingest event")
	}
	return nil, handleTimeseriesIngest(ctx, event)
}

// handleSQSBatch processes each MQTT batch report independently and returns
// partial failures so Lambda retries only the failed SQS messages.
func handleSQSBatch(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	var failures []events.SQSBatchItemFailure
	for _, record := range sqsEvent.Records {
		var event TimeseriesIngestEvent
		if err := json.Unmarshal([]byte(record.Body), &event); err == nil {
			err = handleTimeseriesIngest(ctx, event)
			if err == nil {
				continue
			}
			rlog.Error(ctx).Err(err).Str("messageId", record.MessageId).Msg("Failed to process timeseries batch")
		} else {
			rlog.Error(ctx).Err(err).Str("messageId", record.MessageId).Msg("Failed to parse timeseries batch from SQS message")
		}
		failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func main() {
	lambda.Start(handle)
}
