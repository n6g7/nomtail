package log

import (
	"context"
	"log/slog"
	"os"
)

const (
	LevelTrace   slog.Level = -8
	DefaultLevel slog.Level = slog.LevelInfo
)

type Logger struct {
	*slog.Logger
}

func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		Logger: l.Logger.With(args...),
	}
}

func (l *Logger) Trace(msg string, args ...any) {
	l.Log(context.Background(), LevelTrace, msg, args...)
}

func (l *Logger) TraceContext(ctx context.Context, msg string, args ...any) {
	l.Log(ctx, LevelTrace, msg, args...)
}

var logLevel = new(slog.LevelVar)

func SetupLogger() *Logger {
	logLevel.Set(DefaultLevel)
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	return &Logger{
		Logger: slog.New(handler),
	}
}

func SetLevel(lvl slog.Level) {
	logLevel.Set(lvl)
}
