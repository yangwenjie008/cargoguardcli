package main

import (
	"testing"
)

// ==================== Logger Tests ====================

func TestNewLogger(t *testing.T) {
	cfg := LoggerConfig{
		Level:    "debug",
		Output:   "stdout",
		WithTime: true,
		WithColor: true,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	defer logger.Close()

	if logger == nil {
		t.Error("NewLogger() returned nil logger")
	}
}

func TestLoggerLevels(t *testing.T) {
	cfg := LoggerConfig{
		Level:    "debug",
		Output:   "stdout",
		WithTime: true,
	}

	logger, _ := NewLogger(cfg)
	defer logger.Close()

	// Test different log levels
	tests := []struct {
		level    string
		expected LogLevel
	}{
		{"debug", DEBUG},
		{"info", INFO},
		{"warn", WARN},
		{"error", ERROR},
		{"fatal", FATAL},
		{"unknown", INFO}, // defaults to INFO
	}

	for _, tt := range tests {
		logger.SetLevel(tt.level)
		if logger.level != tt.expected {
			t.Errorf("SetLevel(%s) = %v, want %v", tt.level, logger.level, tt.expected)
		}
	}
}

func TestLoggerClose(t *testing.T) {
	// Test that Close doesn't panic
	logger, _ := NewLogger(LoggerConfig{Level: "info"})
	err := logger.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestLoggerWithFile(t *testing.T) {
	cfg := LoggerConfig{
		Level:    "info",
		Output:   "stdout",
		LogFile:  "/tmp/test-logger.log",
		WithTime: true,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	defer logger.Close()

	if logger.file == nil {
		t.Error("Expected file to be created")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"debug", DEBUG},
		{"info", INFO},
		{"warn", WARN},
		{"error", ERROR},
		{"fatal", FATAL},
		{"DEBUG", INFO},  // case insensitive
		{"INFO", INFO},
		{"unknown", INFO}, // default to INFO
	}

	for _, tt := range tests {
		result := parseLevel(tt.input)
		if result != tt.expected {
			t.Errorf("parseLevel(%s) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestLevelNames(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
		{FATAL, "FATAL"},
	}

	for _, tt := range tests {
		if name := levelNames[tt.level]; name != tt.expected {
			t.Errorf("levelNames[%v] = %s, want %s", tt.level, name, tt.expected)
		}
	}
}

func TestGetLogger(t *testing.T) {
	// GetLogger should return a logger even without InitLogger
	logger := GetLogger()
	if logger == nil {
		t.Error("GetLogger() returned nil")
	}
}

func TestInitLogger(t *testing.T) {
	cfg := LoggerConfig{
		Level:  "debug",
		Output: "stdout",
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger() error = %v", err)
	}

	// After InitLogger, GetLogger should return configured logger
	logger := GetLogger()
	if logger == nil {
		t.Error("GetLogger() returned nil after InitLogger")
	}
}
