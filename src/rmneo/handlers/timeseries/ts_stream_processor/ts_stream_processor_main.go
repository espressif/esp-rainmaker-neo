// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type TSStreamProcessor struct {
	timeseriesDB *timeseries_db.TimeseriesDB
	processor    *timeseries.TimeseriesProcessor
	rmngCtx      *rmngctx.RmngContext
}

type ProcessingMetrics struct {
	ProcessingTimeMs  int64  `json:"processing_time_ms"`
	ConversionTimeMs  int64  `json:"conversion_time_ms"`
	AggregationTimeMs int64  `json:"aggregation_time_ms"`
	BatchSize         int    `json:"batch_size,omitempty"`
	BatchTimeMs       int64  `json:"batch_time_ms,omitempty"`
	EventType         string `json:"event_type"`
}

func NewTSStreamProcessor() *TSStreamProcessor {
	// Create system user context for database operations
	systemActor := utils.NewSystemActor()
	rmngCtx := rmngctx.NewRmngContext(systemActor)

	return &TSStreamProcessor{
		timeseriesDB: timeseries_db.NewTimeseriesDB(rmngCtx),
		processor:    timeseries.NewTimeseriesProcessor(rmngCtx),
		rmngCtx:      rmngCtx,
	}
}

func (p *TSStreamProcessor) logMetrics(metrics ProcessingMetrics) {
	jsonMetrics, err := json.Marshal(metrics)
	if err != nil {
		rlog.Error(p.rmngCtx).Err(err).Msg("Error marshaling metrics")
		return
	}
	rlog.Info(p.rmngCtx).Msgf("METRICS: %s", jsonMetrics)
}

func (p *TSStreamProcessor) ProcessRecord(record events.DynamoDBEventRecord) error {
	startTime := time.Now()
	metrics := ProcessingMetrics{
		EventType: record.EventName,
	}
	defer func() {
		metrics.ProcessingTimeMs = time.Since(startTime).Milliseconds()
		p.logMetrics(metrics)
	}()

	// Note: Event filtering is now handled at the infrastructure level in stack.py
	// Only INSERT events will reach this function due to the DynamoDB stream event filter

	// Extract the new image (current state)
	newImage := record.Change.NewImage
	if newImage == nil {
		return nil
	}

	// Convert DynamoDB attribute values to a timeseries entry
	conversionStart := time.Now()
	rawEntry, err := p.timeseriesDB.UnMarshalToTimeseriesEntry(newImage)
	if err != nil {
		return rmerror.NewRMError(err, "failed to convert DynamoDB record to timeseries entry")
	}
	metrics.ConversionTimeMs = time.Since(conversionStart).Milliseconds()

	// Process all window aggregations using the processor
	aggregationStart := time.Now()
	if err := p.processor.ProcessTimeseriesEntry(rawEntry); err != nil {
		return rmerror.NewRMError(err, "failed to process window aggregations")
	}
	metrics.AggregationTimeMs = time.Since(aggregationStart).Milliseconds()

	return nil
}

func (p *TSStreamProcessor) ProcessRecords(records []events.DynamoDBEventRecord) error {
	batchStartTime := time.Now()
	metrics := ProcessingMetrics{
		BatchSize: len(records),
	}
	defer func() {
		metrics.BatchTimeMs = time.Since(batchStartTime).Milliseconds()
		p.logMetrics(metrics)
	}()

	for _, record := range records {
		if err := p.ProcessRecord(record); err != nil {
			return err
		}
	}
	return nil
}

func HandleRequest(ctx context.Context, event events.DynamoDBEvent) error {
	processor := NewTSStreamProcessor()
	return processor.ProcessRecords(event.Records)
}

func main() {
	lambda.Start(HandleRequest)
}
