// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl

package collector

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"time"
)

const (
	// ecsVersion is the Elastic Common Schema version the ECS log format targets.
	ecsVersion = "8.11.0"
	// ecsServiceName identifies the exporter in ECS log output.
	ecsServiceName = "oracledb_exporter"
)

// NewECSLogger returns an slog.Logger that emits Elastic Common Schema (ECS)
// conformant JSON, suitable for ingestion by ELK (Elasticsearch/Logstash/Kibana).
//
// The output follows the field conventions documented for the official Elastic
// ECS logging libraries (https://www.elastic.co/docs/reference/ecs/logging/intro).
// Compared to the standard slog/promslog JSON output, it:
//   - renames "time" to "@timestamp", "level" to "log.level" (upper-cased, e.g.
//     "INFO", matching the official ECS loggers), and "msg" to "message";
//   - maps the source location to "log.origin.file.name",
//     "log.origin.file.line", and "log.origin.function";
//   - maps "error"/"err" attributes to "error.message" (and "error.type");
//   - emits time.Duration values as numeric nanoseconds (no unit suffix), as
//     expected by ECS duration fields; and
//   - adds "ecs.version", "service.name", and (when non-empty) "service.version"
//     to every record.
//
// serviceVersion is optional; pass an empty string to omit "service.version".
func NewECSLogger(w io.Writer, level slog.Level, serviceVersion string) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: ecsReplaceAttr,
	}
	handler := slog.NewJSONHandler(w, handlerOpts)

	baseAttrs := []slog.Attr{
		slog.String("ecs.version", ecsVersion),
		slog.String("service.name", ecsServiceName),
	}
	if serviceVersion != "" {
		baseAttrs = append(baseAttrs, slog.String("service.version", serviceVersion))
	}
	return slog.New(handler.WithAttrs(baseAttrs))
}

// ecsReplaceAttr rewrites standard slog attributes to their ECS equivalents.
func ecsReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	// Top-level reserved slog keys.
	if len(groups) == 0 {
		switch a.Key {
		case slog.TimeKey:
			a.Key = "@timestamp"
			return a
		case slog.LevelKey:
			a.Key = "log.level"
			// The official ECS loggers emit upper-cased severities, e.g. "INFO".
			if lvl, ok := a.Value.Any().(slog.Level); ok {
				a.Value = slog.StringValue(lvl.String())
			}
			return a
		case slog.MessageKey:
			a.Key = "message"
			return a
		case slog.SourceKey:
			if src, ok := a.Value.Any().(*slog.Source); ok {
				return ecsSourceGroup(src)
			}
			return a
		}
	}

	// Map error attributes to the ECS error fields.
	switch a.Key {
	case "error", "err":
		return ecsErrorGroup(a.Value)
	}

	// Emit durations as numeric nanoseconds, never as a unit string.
	if d, ok := a.Value.Any().(time.Duration); ok {
		a.Value = slog.Int64Value(d.Nanoseconds())
	}

	return a
}

// ecsSourceGroup converts an slog source location into the ECS log.origin fields.
func ecsSourceGroup(src *slog.Source) slog.Attr {
	if src == nil || src.File == "" {
		return slog.Attr{}
	}
	attrs := []any{
		slog.Group("file",
			slog.String("name", filepath.Base(src.File)),
			slog.Int("line", src.Line),
		),
	}
	if src.Function != "" {
		attrs = append(attrs, slog.String("function", filepath.Base(src.Function)))
	}
	return slog.Group("log.origin", attrs...)
}

// ecsErrorGroup maps an error attribute to the ECS error.message and error.type
// fields. The Go error type stands in for the ECS "type or class" of the error.
func ecsErrorGroup(v slog.Value) slog.Attr {
	msg := v.String()
	if err, ok := v.Any().(error); ok {
		return slog.Group("error",
			slog.String("message", err.Error()),
			slog.String("type", fmt.Sprintf("%T", err)),
		)
	}
	return slog.Group("error", slog.String("message", msg))
}
