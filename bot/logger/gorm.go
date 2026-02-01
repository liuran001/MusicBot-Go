package logger

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm/logger"
)

// GormLogger adapts slog.Logger to gorm logger.Interface.
type GormLogger struct {
	logger        *slog.Logger
	level         logger.LogLevel
	slowThreshold time.Duration
}

// NewGormLogger creates a GORM logger with the given level.
func NewGormLogger(base *slog.Logger, level logger.LogLevel) *GormLogger {
	return &GormLogger{
		logger:        base,
		level:         level,
		slowThreshold: 200 * time.Millisecond,
	}
}

func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	copy := *l
	copy.level = level
	return &copy
}

func (l *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.level < logger.Info {
		return
	}
	l.logger.InfoContext(ctx, msg, "data", data)
}

func (l *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.level < logger.Warn {
		return
	}
	l.logger.WarnContext(ctx, msg, "data", data)
}

func (l *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.level < logger.Error {
		return
	}
	l.logger.ErrorContext(ctx, msg, "data", data)
}

func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level == logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	if err != nil && l.level >= logger.Error && !errors.Is(err, logger.ErrRecordNotFound) {
		l.logger.ErrorContext(ctx, "gorm error",
			"error", err,
			"elapsed", elapsed,
			"rows", rows,
			"sql", sql,
		)
		return
	}

	if l.slowThreshold > 0 && elapsed > l.slowThreshold && l.level >= logger.Warn {
		l.logger.WarnContext(ctx, "gorm slow query",
			"elapsed", elapsed,
			"rows", rows,
			"sql", sql,
		)
		return
	}

	if l.level == logger.Info {
		l.logger.InfoContext(ctx, "gorm query",
			"elapsed", elapsed,
			"rows", rows,
			"sql", sql,
		)
	}
}
