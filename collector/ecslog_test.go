// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestECSLoggerProducesECSConformantFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewECSLogger(&buf, slog.LevelInfo, "2.4.1")

	logger.Info("scrape complete",
		"database", "db1",
		"duration", 5*time.Minute,
		"error", errors.New("boom"),
	)

	raw := buf.Bytes()
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("ECS log output is not valid JSON: %v\noutput: %s", err, raw)
	}

	// Core ECS fields (matching the official ECS logging examples).
	if _, ok := record["@timestamp"]; !ok {
		t.Errorf("expected @timestamp field, got: %s", raw)
	}
	if got := record["log.level"]; got != "INFO" {
		t.Errorf("expected log.level=INFO, got %v", got)
	}
	if got := record["message"]; got != "scrape complete" {
		t.Errorf("expected message field, got %v", got)
	}
	if got := record["ecs.version"]; got != ecsVersion {
		t.Errorf("expected ecs.version=%q, got %v", ecsVersion, got)
	}
	if got := record["service.name"]; got != ecsServiceName {
		t.Errorf("expected service.name=%q, got %v", ecsServiceName, got)
	}
	if got := record["service.version"]; got != "2.4.1" {
		t.Errorf("expected service.version=2.4.1, got %v", got)
	}

	// Standard slog keys must not leak through.
	for _, key := range []string{"time", "level", "msg", "source"} {
		if _, ok := record[key]; ok {
			t.Errorf("unexpected non-ECS key %q in output: %s", key, raw)
		}
	}

	// error attribute mapped to the ECS error group.
	errObj, ok := record["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T (%v)", record["error"], record["error"])
	}
	if got := errObj["message"]; got != "boom" {
		t.Errorf("expected error.message=boom, got %v", got)
	}
	if _, ok := errObj["type"].(string); !ok {
		t.Errorf("expected error.type string, got %T", errObj["type"])
	}

	// Duration must be numeric nanoseconds with no unit suffix.
	gotDuration, ok := record["duration"].(float64)
	if !ok {
		t.Fatalf("expected numeric duration, got %T (%v)", record["duration"], record["duration"])
	}
	if want := float64((5 * time.Minute).Nanoseconds()); gotDuration != want {
		t.Errorf("expected duration=%v ns, got %v", want, gotDuration)
	}
	if strings.Contains(string(raw), "5m0s") {
		t.Errorf("duration should not be a unit string, got: %s", raw)
	}
}

func TestECSLoggerOmitsServiceVersionWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	logger := NewECSLogger(&buf, slog.LevelInfo, "")

	logger.Info("hello")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := record["service.version"]; ok {
		t.Errorf("expected service.version to be omitted, got: %s", buf.Bytes())
	}
}

func TestECSLoggerEmitsSourceOrigin(t *testing.T) {
	var buf bytes.Buffer
	logger := NewECSLogger(&buf, slog.LevelInfo, "")

	logger.Info("hello")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	origin, ok := record["log.origin"].(map[string]any)
	if !ok {
		t.Fatalf("expected log.origin object, got %T", record["log.origin"])
	}
	file, ok := origin["file"].(map[string]any)
	if !ok {
		t.Fatalf("expected log.origin.file object, got %T", origin["file"])
	}
	if name, _ := file["name"].(string); name != "ecslog_test.go" {
		t.Errorf("expected log.origin.file.name=ecslog_test.go, got %v", file["name"])
	}
	if _, ok := file["line"].(float64); !ok {
		t.Errorf("expected numeric log.origin.file.line, got %T", file["line"])
	}
}

func TestECSLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewECSLogger(&buf, slog.LevelWarn, "")

	logger.Info("should be filtered")
	if buf.Len() != 0 {
		t.Errorf("expected info log to be filtered at warn level, got: %s", buf.Bytes())
	}

	logger.Warn("should appear")
	if buf.Len() == 0 {
		t.Error("expected warn log to be emitted at warn level")
	}
}
