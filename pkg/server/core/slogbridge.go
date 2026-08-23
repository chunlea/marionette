package core

import (
	"context"
	"log/slog"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapSlogHandler forwards slog records into zap.
//
// pkg/jobs takes an *slog.Logger while everything else here takes a
// *zap.Logger. Without a bridge the chunk GC job would write to slog's default
// handler - a second log stream, in a different format, from a job whose whole
// purpose is deleting blobs. That is not where an operator should have to go
// looking when something disappears.
//
// This exists instead of go.uber.org/zap/exp/zapslog because adding a module
// dependency needs coordinator approval, and thirty lines is cheaper than the
// round trip.
type zapSlogHandler struct {
	logger *zap.Logger
	attrs  []slog.Attr
	group  string
}

// newSlogLogger returns an *slog.Logger that writes through logger.
func newSlogLogger(logger *zap.Logger) *slog.Logger {
	return slog.New(&zapSlogHandler{logger: logger})
}

func (h *zapSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.logger.Core().Enabled(zapLevel(level))
}

func (h *zapSlogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make([]zap.Field, 0, len(h.attrs)+record.NumAttrs())
	for _, attr := range h.attrs {
		fields = append(fields, h.field(attr))
	}
	record.Attrs(func(attr slog.Attr) bool {
		fields = append(fields, h.field(attr))
		return true
	})

	if ce := h.logger.Check(zapLevel(record.Level), record.Message); ce != nil {
		ce.Write(fields...)
	}
	return nil
}

func (h *zapSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

func (h *zapSlogHandler) WithGroup(name string) slog.Handler {
	next := *h
	if h.group != "" && name != "" {
		next.group = h.group + "." + name
	} else if name != "" {
		next.group = name
	}
	return &next
}

// field converts one slog attribute, prefixing it with the current group so
// grouped keys do not collide.
func (h *zapSlogHandler) field(attr slog.Attr) zap.Field {
	key := attr.Key
	if h.group != "" {
		key = h.group + "." + key
	}
	return zap.Any(key, attr.Value.Any())
}

// zapLevel maps slog levels onto zap's. slog's scale is coarser, so anything
// above Error is still Error: zap's higher levels panic or exit the process,
// which a log line must never do on its own.
func zapLevel(level slog.Level) zapcore.Level {
	switch {
	case level < slog.LevelInfo:
		return zapcore.DebugLevel
	case level < slog.LevelWarn:
		return zapcore.InfoLevel
	case level < slog.LevelError:
		return zapcore.WarnLevel
	default:
		return zapcore.ErrorLevel
	}
}
