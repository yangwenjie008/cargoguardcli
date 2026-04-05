package main

import (
	"context"
	"testing"
)

// ==================== Telemetry Tests ====================

func TestInitTelemetry(t *testing.T) {
	cfg := TelemetryConfig{
		Enabled:     true,
		ServiceName: "test-service",
		Env:         "test",
	}

	err := InitTelemetry(cfg)
	if err != nil {
		t.Fatalf("InitTelemetry() error = %v", err)
	}

	if telemetry == nil {
		t.Error("Global telemetry is nil after InitTelemetry")
	}
}

func TestInitTelemetryDefaults(t *testing.T) {
	// Test with empty config - should use defaults
	cfg := TelemetryConfig{}

	err := InitTelemetry(cfg)
	if err != nil {
		t.Fatalf("InitTelemetry() error = %v", err)
	}

	if telemetry == nil {
		t.Error("Global telemetry is nil after InitTelemetry")
	}
}

func TestStartSpan(t *testing.T) {
	cfg := TelemetryConfig{
		Enabled: true,
	}
	InitTelemetry(cfg)

	ctx, span := StartSpan(context.Background(), "test-span")
	if span == nil {
		t.Error("StartSpan() returned nil span")
	}

	// Verify span is added to context
	if ctx == nil {
		t.Error("StartSpan() returned nil context")
	}

	span.End(nil)
}

func TestSetSpanAttributes(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	ctx, span := StartSpan(context.Background(), "test-span")
	SetSpanAttributes(ctx, map[string]string{
		"key1": "value1",
		"key2": "value2",
	})
	span.End(nil)

	// Verify spans are recorded
	spans := GetSpans()
	if len(spans) < 1 {
		t.Error("Expected at least 1 span to be recorded")
	}
}

func TestAddSpanEvent(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	ctx, span := StartSpan(context.Background(), "test-span")
	AddSpanEvent(ctx, "test-event", map[string]string{"attr": "value"})
	span.End(nil)

	spans := GetSpans()
	if len(spans) > 0 && len(spans[len(spans)-1].Events) == 0 {
		t.Error("Expected event to be recorded in span")
	}
}

func TestRecordError(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	ctx, span := StartSpan(context.Background(), "test-span")
	RecordError(ctx, context.DeadlineExceeded)
	span.End(nil)
}

func TestGenerateIDs(t *testing.T) {
	traceID := generateTraceID()
	if len(traceID) != 16 {
		t.Errorf("TraceID length = %d, want 16", len(traceID))
	}

	spanID := generateSpanID()
	if len(spanID) != 8 {
		t.Errorf("SpanID length = %d, want 8", len(spanID))
	}
}

func TestExportTraces(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	_, span := StartSpan(context.Background(), "export-test")
	span.End(nil)

	data, err := ExportTraces()
	if err != nil {
		t.Errorf("ExportTraces() error = %v", err)
	}
	if data == nil {
		t.Error("ExportTraces() returned nil data")
	}
}

func TestExportMetrics(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	data, err := ExportMetrics()
	if err != nil {
		t.Errorf("ExportMetrics() error = %v", err)
	}
	if data == nil {
		t.Error("ExportMetrics() returned nil data")
	}
}

// ==================== ScanMetrics Tests ====================

func TestNewScanMetrics(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	metrics, err := NewScanMetrics(context.Background())
	if err != nil {
		t.Fatalf("NewScanMetrics() error = %v", err)
	}

	if metrics == nil {
		t.Error("NewScanMetrics() returned nil")
	}
}

func TestRecordScan(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	metrics, _ := NewScanMetrics(context.Background())

	// Should not panic
	metrics.RecordScan(context.Background(), 1.5, 100, 5, "full")
	metrics.RecordScan(context.Background(), 2.0, 50, 0, "quick")
}

func TestRecordErrorMetric(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	metrics, _ := NewScanMetrics(context.Background())

	// Should not panic
	metrics.RecordErrorMetric(context.Background(), "io_error")
	metrics.RecordErrorMetric(context.Background(), "timeout")
}

// ==================== Span Hierarchy Tests ====================

func TestSpanHierarchy(t *testing.T) {
	cfg := TelemetryConfig{Enabled: true}
	InitTelemetry(cfg)

	// Parent span
	ctx1, parent := StartSpan(context.Background(), "parent-span")
	parent.End(nil)

	// Child span
	_, child := StartSpan(ctx1, "child-span")
	child.End(nil)
}
