package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ----------------------
// Span / Trace
// ----------------------

type SpanContext struct {
	TraceID string
	SpanID  string
	ParentID string
}

type SpanRecord struct {
	Name       string
	TraceID    string
	SpanID     string
	ParentID   string
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	Attributes map[string]string
	Events     []EventRecord
	Error      string
}

type EventRecord struct {
	Name      string
	Timestamp time.Time
	Attrs     map[string]string
}

// ----------------------
// Metrics
// ----------------------

type MetricRecord struct {
	Name      string
	Type      string // counter, gauge, histogram
	Value     float64
	Labels    map[string]string
	Timestamp time.Time
}

// ----------------------
// Telemetry Core
// ----------------------

type Telemetry struct {
	config  TelemetryConfig
	spans   []SpanRecord
	metrics []MetricRecord
	mu      sync.RWMutex
}

var telemetry *Telemetry

// InitTelemetry 初始化
func InitTelemetry(cfg TelemetryConfig) error {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "cargoguardcli"
	}
	cfg.ServiceVersion = "1.0.0"
	if cfg.Env == "" {
		cfg.Env = "dev"
	}

	telemetry = &Telemetry{
		config:  cfg,
		spans:   []SpanRecord{},
		metrics: []MetricRecord{},
	}

	log.Debug("📊 Telemetry initialized: service=%s, env=%s", cfg.ServiceName, cfg.Env)
	return nil
}

// Shutdown 关闭
func (t *Telemetry) Shutdown(ctx context.Context) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	log.Debug("📊 Telemetry shutdown: spans=%d, metrics=%d", len(t.spans), len(t.metrics))
	
	// 输出所有 spans
	for _, span := range t.spans {
		spanJSON, _ := json.Marshal(span)
		log.Debug("  └─ Span: %s", string(spanJSON))
	}
	
	return nil
}

// ----------------------
// Span Operations
// ----------------------

type spanKey struct{}

type SimpleSpan struct {
	name       string
	traceID    string
	spanID     string
	parentID   string
	startTime  time.Time
	endTime    time.Time
	attributes map[string]string
	events     []EventRecord
	err        error
	parent     *SimpleSpan
}

func (s *SimpleSpan) End(options ...interface{}) {
	s.endTime = time.Now()
	duration := s.endTime.Sub(s.startTime)

	log.Debug("📊 Span: %s | %v", s.name, duration)

	if telemetry != nil {
		telemetry.mu.Lock()
		defer telemetry.mu.Unlock()

		parentID := ""
		if s.parent != nil {
			parentID = s.parent.spanID
		}

		record := SpanRecord{
			Name:       s.name,
			TraceID:    s.traceID,
			SpanID:     s.spanID,
			ParentID:   parentID,
			StartTime:  s.startTime,
			EndTime:    s.endTime,
			Duration:   duration,
			Attributes: s.attributes,
			Events:     s.events,
		}
		telemetry.spans = append(telemetry.spans, record)
	}
}

func (s *SimpleSpan) AddEvent(name string, attrs map[string]string) {
	s.events = append(s.events, EventRecord{
		Name:      name,
		Timestamp: time.Now(),
		Attrs:     attrs,
	})
	log.Debug("  └─ Event: %s", name)
}

func (s *SimpleSpan) SetAttributes(attrs map[string]string) {
	for k, v := range attrs {
		s.attributes[k] = v
	}
}

// StartSpan 启动一个 span
func StartSpan(ctx context.Context, name string) (context.Context, *SimpleSpan) {
	span := &SimpleSpan{
		name:       name,
		traceID:    generateTraceID(),
		spanID:     generateSpanID(),
		startTime:  time.Now(),
		attributes: make(map[string]string),
		events:     []EventRecord{},
	}

	// 获取 parent span
	if parent := ctx.Value(spanKey{}); parent != nil {
		if p, ok := parent.(*SimpleSpan); ok {
			span.parent = p
			span.parentID = p.spanID
		}
	}

	newCtx := context.WithValue(ctx, spanKey{}, span)
	return newCtx, span
}

// AddSpanEvent 添加事件
func AddSpanEvent(ctx context.Context, name string, attrs map[string]string) {
	if span := ctx.Value(spanKey{}); span != nil {
		if s, ok := span.(*SimpleSpan); ok {
			s.AddEvent(name, attrs)
		}
	}
}

// SetSpanAttributes 设置属性
func SetSpanAttributes(ctx context.Context, attrs map[string]string) {
	if span := ctx.Value(spanKey{}); span != nil {
		if s, ok := span.(*SimpleSpan); ok {
			s.SetAttributes(attrs)
		}
	}
}

// RecordError 记录错误
func RecordError(ctx context.Context, err error) {
	if span := ctx.Value(spanKey{}); span != nil {
		if s, ok := span.(*SimpleSpan); ok {
			s.err = err
			log.Error("Span error: %s - %v", s.name, err)
		}
	}
}

// ----------------------
// Metrics
// ----------------------

type ScanMetrics struct {
	totalScans  int64
	totalFiles  int64
	totalIssues int64
	errorCount  int64
}

func NewScanMetrics(ctx context.Context) (*ScanMetrics, error) {
	return &ScanMetrics{}, nil
}

func (m *ScanMetrics) RecordScan(ctx context.Context, duration float64, files, issues int64, scanType string) {
	m.totalScans++
	m.totalFiles += files
	m.totalIssues += issues

	log.Debug("📈 Scan: type=%s, duration=%.2fs, files=%d, issues=%d",
		scanType, duration, files, issues)

	// 记录到 telemetry
	if telemetry != nil {
		telemetry.mu.Lock()
		defer telemetry.mu.Unlock()

		telemetry.metrics = append(telemetry.metrics, MetricRecord{
			Name:      "scan",
			Type:      "histogram",
			Value:     duration,
			Labels:    map[string]string{"scan_type": scanType},
			Timestamp: time.Now(),
		})
	}
}

func (m *ScanMetrics) RecordErrorMetric(ctx context.Context, errorType string) {
	m.errorCount++
	log.Debug("📈 Error: type=%s, total=%d", errorType, m.errorCount)
}

// GetSpans 获取所有 span
func GetSpans() []SpanRecord {
	if telemetry == nil {
		return nil
	}
	telemetry.mu.RLock()
	defer telemetry.mu.RUnlock()
	return telemetry.spans
}

// ----------------------
// 辅助函数
// ----------------------

func generateTraceID() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}

func generateSpanID() string {
	return fmt.Sprintf("%08x", time.Now().UnixNano()>>32)
}

// RecordScan 记录扫描指标 (全局函数)
func RecordScan(name string, value float64, labels map[string]string) {
	if telemetry == nil {
		return
	}
	telemetry.mu.Lock()
	defer telemetry.mu.Unlock()
	telemetry.metrics = append(telemetry.metrics, MetricRecord{
		Name:      name,
		Type:      "gauge",
		Value:     value,
		Labels:    labels,
		Timestamp: time.Now(),
	})
}

// ExportTraces 导出 traces (可扩展为 OTLP)
func ExportTraces() ([]byte, error) {
	if telemetry == nil {
		return nil, nil
	}
	telemetry.mu.RLock()
	defer telemetry.mu.RUnlock()
	return json.MarshalIndent(telemetry.spans, "", "  ")
}

// ExportMetrics 导出 metrics
func ExportMetrics() ([]byte, error) {
	if telemetry == nil {
		return nil, nil
	}
	telemetry.mu.RLock()
	defer telemetry.mu.RUnlock()
	return json.MarshalIndent(telemetry.metrics, "", "  ")
}
