// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

func TestNewEMFMetrics(t *testing.T) {
	m := NewEMFMetrics("TestNamespace")
	if m == nil {
		t.Fatal("Expected non-nil EMFMetrics")
	}
	if m.namespace != "TestNamespace" {
		t.Errorf("Expected namespace TestNamespace, got %s", m.namespace)
	}
}

func TestSetDimension(t *testing.T) {
	m := NewEMFMetrics("TestNamespace")
	m.SetDimension("Environment", "test")
	m.SetDimension("Service", "handler")

	if m.dimensions["Environment"] != "test" {
		t.Errorf("Expected Environment dimension to be 'test', got '%s'", m.dimensions["Environment"])
	}
	if m.dimensions["Service"] != "handler" {
		t.Errorf("Expected Service dimension to be 'handler', got '%s'", m.dimensions["Service"])
	}
}

func TestPutMetric(t *testing.T) {
	m := NewEMFMetrics("TestNamespace")
	m.PutMetric("TestMetric", 42.0, UnitCount)

	if len(m.metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(m.metrics))
	}
	if m.metrics[0].name != "TestMetric" {
		t.Errorf("Expected metric name 'TestMetric', got '%s'", m.metrics[0].name)
	}
	if m.metrics[0].value != 42.0 {
		t.Errorf("Expected metric value 42.0, got %f", m.metrics[0].value)
	}
	if m.metrics[0].unit != UnitCount {
		t.Errorf("Expected metric unit 'Count', got '%s'", m.metrics[0].unit)
	}
}

func TestPutCount(t *testing.T) {
	m := NewEMFMetrics("TestNamespace")
	m.PutCount("RequestCount", 10)

	if len(m.metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(m.metrics))
	}
	if m.metrics[0].unit != UnitCount {
		t.Errorf("Expected unit 'Count', got '%s'", m.metrics[0].unit)
	}
}

func TestPutDuration(t *testing.T) {
	m := NewEMFMetrics("TestNamespace")
	m.PutDuration("ProcessingTime", 100*time.Millisecond)

	if len(m.metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(m.metrics))
	}
	if m.metrics[0].unit != UnitMilliseconds {
		t.Errorf("Expected unit 'Milliseconds', got '%s'", m.metrics[0].unit)
	}
	if m.metrics[0].value != 100 {
		t.Errorf("Expected value 100, got %f", m.metrics[0].value)
	}
}

func TestSetProperty(t *testing.T) {
	m := NewEMFMetrics("TestNamespace")
	m.SetProperty("RequestId", "abc123")
	m.SetProperty("BatchSize", 5)

	if m.properties["RequestId"] != "abc123" {
		t.Errorf("Expected RequestId property to be 'abc123'")
	}
	if m.properties["BatchSize"] != 5 {
		t.Errorf("Expected BatchSize property to be 5")
	}
}

func TestEmit(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	m := NewEMFMetrics("TestNamespace")
	m.SetDimension("FunctionName", "testHandler")
	m.PutCount("Success", 1)
	m.SetProperty("RequestId", "test-123")
	m.Emit()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Parse the output as JSON
	var emf map[string]interface{}
	if err := json.Unmarshal([]byte(output), &emf); err != nil {
		t.Fatalf("Failed to parse EMF output as JSON: %v", err)
	}

	// Verify EMF structure
	aws, ok := emf["_aws"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing or invalid _aws field")
	}

	cwMetrics, ok := aws["CloudWatchMetrics"].([]interface{})
	if !ok || len(cwMetrics) == 0 {
		t.Fatal("Missing or invalid CloudWatchMetrics field")
	}

	// Verify dimension value is present
	if emf["FunctionName"] != "testHandler" {
		t.Errorf("Expected FunctionName dimension value 'testHandler', got '%v'", emf["FunctionName"])
	}

	// Verify metric value is present
	if emf["Success"] != float64(1) {
		t.Errorf("Expected Success metric value 1, got '%v'", emf["Success"])
	}

	// Verify property is present
	if emf["RequestId"] != "test-123" {
		t.Errorf("Expected RequestId property 'test-123', got '%v'", emf["RequestId"])
	}
}

func TestEmitNoMetrics(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	m := NewEMFMetrics("TestNamespace")
	m.Emit() // Should not output anything

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if output != "" {
		t.Errorf("Expected no output for empty metrics, got '%s'", output)
	}
}

func TestEmitMultiValue(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	m := NewEMFMetrics("TestNamespace")
	m.SetDimension("FunctionName", "testHandler")
	m.PutDuration("RecordLatency", 10*time.Millisecond)
	m.PutDuration("RecordLatency", 20*time.Millisecond)
	m.PutDuration("RecordLatency", 30*time.Millisecond)
	m.PutCount("EventsProcessed", 3)
	m.Emit()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)

	var emf map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &emf); err != nil {
		t.Fatalf("Failed to parse EMF output: %v", err)
	}

	latency, ok := emf["RecordLatency"].([]interface{})
	if !ok {
		t.Fatalf("Expected RecordLatency to be an array, got %T", emf["RecordLatency"])
	}
	if len(latency) != 3 {
		t.Errorf("Expected 3 latency samples, got %d", len(latency))
	}

	if emf["EventsProcessed"] != float64(3) {
		t.Errorf("Expected EventsProcessed to be scalar 3, got %v", emf["EventsProcessed"])
	}

	aws := emf["_aws"].(map[string]interface{})
	cwMetrics := aws["CloudWatchMetrics"].([]interface{})
	defs := cwMetrics[0].(map[string]interface{})["Metrics"].([]interface{})
	if len(defs) != 2 {
		t.Errorf("Expected 2 unique metric definitions, got %d", len(defs))
	}
}

func TestLambdaMetrics(t *testing.T) {
	m := NewLambdaMetrics("testFunction", "testEvent")

	if m.EMFMetrics.dimensions["FunctionName"] != "testFunction" {
		t.Errorf("Expected FunctionName dimension")
	}
	if m.EMFMetrics.dimensions["EventType"] != "testEvent" {
		t.Errorf("Expected EventType dimension")
	}
}

func TestLambdaMetricsRecordMethods(t *testing.T) {
	m := NewLambdaMetrics("testFunction", "testEvent")

	m.RecordEventsProcessed(10)
	m.RecordFailedEvents(2)

	if len(m.metrics) != 2 {
		t.Errorf("Expected 2 metrics, got %d", len(m.metrics))
	}
}

func TestChainedCalls(t *testing.T) {
	m := NewEMFMetrics("TestNamespace")
	result := m.SetDimension("A", "1").SetDimension("B", "2").PutCount("C", 3)

	if result != m {
		t.Error("Expected chained calls to return the same instance")
	}
	if len(m.dimensions) != 2 {
		t.Errorf("Expected 2 dimensions, got %d", len(m.dimensions))
	}
	if len(m.metrics) != 1 {
		t.Errorf("Expected 1 metric, got %d", len(m.metrics))
	}
}
