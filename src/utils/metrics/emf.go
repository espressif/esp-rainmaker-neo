// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package metrics provides CloudWatch Embedded Metrics Format (EMF) support
// for Lambda functions to emit custom metrics without API call overhead.
//
// EMF metrics are written to stdout as structured JSON and automatically
// picked up by CloudWatch Logs and converted to CloudWatch Metrics.
//
// See: https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format.html
package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// MetricUnit represents CloudWatch metric units
type MetricUnit string

const (
	UnitSeconds      MetricUnit = "Seconds"
	UnitMilliseconds MetricUnit = "Milliseconds"
	UnitMicroseconds MetricUnit = "Microseconds"
	UnitCount        MetricUnit = "Count"
	UnitPercent      MetricUnit = "Percent"
	UnitBytesPerSec  MetricUnit = "Bytes/Second"
	UnitNone         MetricUnit = "None"
)

// Namespace constants for RainMaker metrics
const (
	NamespaceRainMakerLambda = "RainMaker/Lambda"
)

// EMFMetrics collects metrics and emits them in CloudWatch EMF format
type EMFMetrics struct {
	namespace  string
	dimensions map[string]string
	metrics    []metricDefinition
	properties map[string]interface{}
	timestamp  int64
	mu         sync.Mutex
}

type metricDefinition struct {
	name  string
	value float64
	unit  MetricUnit
}

// NewEMFMetrics creates a new EMF metrics emitter
func NewEMFMetrics(namespace string) *EMFMetrics {
	return &EMFMetrics{
		namespace:  namespace,
		dimensions: make(map[string]string),
		metrics:    make([]metricDefinition, 0),
		properties: make(map[string]interface{}),
		timestamp:  time.Now().UnixMilli(),
	}
}

// SetDimension adds a dimension to the metrics
// Dimensions are used to filter/group metrics in CloudWatch
func (e *EMFMetrics) SetDimension(key, value string) *EMFMetrics {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dimensions[key] = value
	return e
}

// SetProperty adds a property (non-metric metadata) to the EMF output
// Properties are logged but not aggregated as metrics
func (e *EMFMetrics) SetProperty(key string, value interface{}) *EMFMetrics {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.properties[key] = value
	return e
}

// PutMetric adds a metric value
func (e *EMFMetrics) PutMetric(name string, value float64, unit MetricUnit) *EMFMetrics {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics = append(e.metrics, metricDefinition{
		name:  name,
		value: value,
		unit:  unit,
	})
	return e
}

// PutCount is a convenience method for count metrics
func (e *EMFMetrics) PutCount(name string, value float64) *EMFMetrics {
	return e.PutMetric(name, value, UnitCount)
}

// PutDuration is a convenience method for duration metrics in milliseconds
func (e *EMFMetrics) PutDuration(name string, value time.Duration) *EMFMetrics {
	return e.PutMetric(name, float64(value.Milliseconds()), UnitMilliseconds)
}

// Emit writes the metrics to stdout in EMF format
// Call this once at the end of your Lambda function
func (e *EMFMetrics) Emit() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.metrics) == 0 {
		return
	}

	// Build dimension names for the metric definition
	dimensionNames := make([]string, 0, len(e.dimensions))
	for name := range e.dimensions {
		dimensionNames = append(dimensionNames, name)
	}

	// Group metrics by name so repeated samples for the same metric (e.g. per-record
	// latency in a batch) emit as an array value with a single metric definition,
	// matching the EMF multi-value spec.
	metricOrder := make([]string, 0, len(e.metrics))
	metricUnits := make(map[string]MetricUnit, len(e.metrics))
	metricValues := make(map[string][]float64, len(e.metrics))
	for _, m := range e.metrics {
		if _, seen := metricValues[m.name]; !seen {
			metricOrder = append(metricOrder, m.name)
			metricUnits[m.name] = m.unit
		}
		metricValues[m.name] = append(metricValues[m.name], m.value)
	}

	metricDefs := make([]map[string]interface{}, 0, len(metricOrder))
	for _, name := range metricOrder {
		metricDefs = append(metricDefs, map[string]interface{}{
			"Name": name,
			"Unit": string(metricUnits[name]),
		})
	}

	// Build the EMF structure
	emf := map[string]interface{}{
		"_aws": map[string]interface{}{
			"Timestamp": e.timestamp,
			"CloudWatchMetrics": []map[string]interface{}{
				{
					"Namespace":  e.namespace,
					"Dimensions": [][]string{dimensionNames},
					"Metrics":    metricDefs,
				},
			},
		},
	}

	// Add dimensions as top-level properties
	for name, value := range e.dimensions {
		emf[name] = value
	}

	// Add metric values as top-level properties (scalar for single sample, array for multi)
	for _, name := range metricOrder {
		values := metricValues[name]
		if len(values) == 1 {
			emf[name] = values[0]
		} else {
			emf[name] = values
		}
	}

	// Add custom properties
	for name, value := range e.properties {
		emf[name] = value
	}

	// Emit to stdout (CloudWatch Logs will pick this up)
	output, err := json.Marshal(emf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal EMF metrics: %v\n", err)
		return
	}
	fmt.Println(string(output))
}

// LambdaMetrics provides pre-configured metrics for Lambda handlers
type LambdaMetrics struct {
	*EMFMetrics
	startTime time.Time
}

// NewLambdaMetrics creates metrics for a Lambda function with common dimensions
func NewLambdaMetrics(functionName, eventType string) *LambdaMetrics {
	m := &LambdaMetrics{
		EMFMetrics: NewEMFMetrics(NamespaceRainMakerLambda),
		startTime:  time.Now(),
	}
	m.SetDimension("FunctionName", functionName)
	m.SetDimension("EventType", eventType)
	return m
}

// RecordEventsProcessed records the number of SQS records processed in a batch
func (l *LambdaMetrics) RecordEventsProcessed(count int) *LambdaMetrics {
	l.PutCount("EventsProcessed", float64(count))
	return l
}

// RecordFailedEvents records the number of failed records in a batch
func (l *LambdaMetrics) RecordFailedEvents(count int) *LambdaMetrics {
	l.PutCount("FailedEvents", float64(count))
	return l
}

// RecordProcessingTime records the total processing duration
func (l *LambdaMetrics) RecordProcessingTime() *LambdaMetrics {
	l.PutDuration("ProcessingTime", time.Since(l.startTime))
	return l
}

// EmitWithDuration emits all metrics including the processing time
func (l *LambdaMetrics) EmitWithDuration() {
	l.RecordProcessingTime()
	l.Emit()
}
