// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/metrics"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// presenceOfflineDelay is the grace period applied before reading the session
// on a disconnected event, so a concurrent reconnect's DynamoDB PutItem can
// land first and the stale disconnect can be dropped.
//
// Direct path: this delay is applied here in handle(), as a time.Sleep.
// SQS path: the queue's DelaySeconds attribute applies the same wait before the
// message becomes visible, so no in-lambda sleep is needed for the batch path.
var (
	presenceOfflineDelay                     = 10 * time.Second
	presenceOfflineSleep func(time.Duration) = time.Sleep
)

// handle dispatches a Lambda invocation to either the SQS-batch path or the
// direct IoT-rule path based on the shape of the incoming JSON. The IoT rule
// action can be flipped between Lambda-direct and SQS at runtime, so the
// binary must accept either payload shape.
func handle(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	if awscommon.IsSQSEvent(raw) {
		// SQS path: the queue's delivery_delay already held the message for
		// presenceOfflineDelay before making it visible; no in-lambda sleep needed.
		var sqsEvent events.SQSEvent
		if err := json.Unmarshal(raw, &sqsEvent); err != nil {
			return nil, fmt.Errorf("failed to unmarshal SQS event: %w", err)
		}
		return handleSQSBatch(ctx, sqsEvent)
	}

	var event node.PresenceEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal presence event: %w", err)
	}
	if event.EventType == "disconnected" {
		// Direct path: sleep so the reconnect's DDB write can land before we read.
		presenceOfflineSleep(presenceOfflineDelay)
	}
	return nil, handlePresenceEvent(ctx, event)
}

// handleSQSBatch processes a batch of SQS messages containing presence events
// and reports per-record failures via SQSEventResponse.BatchItemFailures.
// Records arrive without ordering guarantees and are processed independently.
func handleSQSBatch(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	m := metrics.NewLambdaMetrics("presence_event_handler", "sqs_batch")
	defer m.EmitWithDuration()

	var failures []events.SQSBatchItemFailure
	batchSize := len(sqsEvent.Records)

	for _, record := range sqsEvent.Records {
		event, err := parsePresenceEvent(record.Body)
		if err != nil {
			rlog.Error(ctx).Err(err).Str("messageId", record.MessageId).Msg("Failed to parse presence event from SQS message")
			failures = append(failures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
			continue
		}

		recordStart := time.Now()
		err = handlePresenceEvent(ctx, event)
		m.PutDuration("RecordLatency", time.Since(recordStart))
		if err != nil {
			rlog.Error(ctx).Err(err).Str("messageId", record.MessageId).Str("clientId", event.ClientID).Msg("Failed to process presence event")
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
