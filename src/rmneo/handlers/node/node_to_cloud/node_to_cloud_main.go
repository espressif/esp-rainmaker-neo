// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/metrics"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// handle dispatches a Lambda invocation to either the SQS-batch path or the
// direct IoT-rule path based on the shape of the incoming JSON. The IoT rule
// action can be flipped between Lambda-direct and SQS at runtime, so the
// binary must accept either payload shape.
func handle(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	if awscommon.IsSQSEvent(raw) {
		var sqsEvent events.SQSEvent
		if err := json.Unmarshal(raw, &sqsEvent); err != nil {
			return nil, rmerror.NewRMError(err, "failed to unmarshal SQS event")
		}
		return handleSQSBatch(ctx, sqsEvent)
	}

	var event node.PublishInputEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal publish input event")
	}
	return nil, handlePublishInputEvent(ctx, event)
}

// handleSQSBatch processes a batch of SQS messages containing publish input
// events and reports per-record failures via SQSEventResponse.BatchItemFailures.
// Records arrive without ordering guarantees and are processed independently.
func handleSQSBatch(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	m := metrics.NewLambdaMetrics(os.Getenv("AWS_LAMBDA_FUNCTION_NAME"), "sqs_batch")
	defer m.EmitWithDuration()

	var failures []events.SQSBatchItemFailure
	batchSize := len(sqsEvent.Records)

	for _, record := range sqsEvent.Records {
		event, err := parsePublishInputEvent(record.Body)
		if err != nil {
			rlog.Error(ctx).Err(err).Str("messageId", record.MessageId).Msg("Failed to parse publish input event from SQS message")
			failures = append(failures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
			continue
		}

		recordStart := time.Now()
		err = handlePublishInputEvent(ctx, event)
		m.PutDuration("RecordLatency", time.Since(recordStart))
		if err != nil {
			rlog.Error(ctx).Err(err).Str("messageId", record.MessageId).Str("thingName", event.ThingName).Msg("Failed to process publish input event")
			failures = append(failures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
		}
	}

	m.RecordEventsProcessed(batchSize)
	m.RecordFailedEvents(len(failures))

	return events.SQSEventResponse{
		BatchItemFailures: failures,
	}, nil
}

func main() {
	lambda.Start(handle)
}
