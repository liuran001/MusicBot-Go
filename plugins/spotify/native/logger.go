package native

import (
	"fmt"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/liuran001/MusicBot-Go/bot"
)

// logAdapter adapts the bot's structured logger to go-librespot's printf-style
// librespot.Logger interface. go-librespot logs verbosely at Trace/Debug; we
// map those down to the bot logger's Debug to avoid noise, and carry WithField
// context as structured key/value args.
type logAdapter struct {
	base   bot.Logger
	fields []any
}

// newLogAdapter wraps a bot.Logger. A nil base yields a no-op logger.
func newLogAdapter(base bot.Logger) librespot.Logger {
	return &logAdapter{base: base}
}

func (l *logAdapter) emit(level func(string, ...any), format string, args []any) {
	if l.base == nil || level == nil {
		return
	}
	level(fmt.Sprintf(format, args...), l.fields...)
}

func (l *logAdapter) line(level func(string, ...any), args []any) {
	if l.base == nil || level == nil {
		return
	}
	level(fmt.Sprint(args...), l.fields...)
}

func (l *logAdapter) Tracef(format string, args ...interface{}) {
	if l.base != nil {
		l.emit(l.base.Debug, format, args)
	}
}
func (l *logAdapter) Debugf(format string, args ...interface{}) {
	if l.base != nil {
		l.emit(l.base.Debug, format, args)
	}
}
func (l *logAdapter) Infof(format string, args ...interface{}) {
	if l.base != nil {
		l.emit(l.base.Info, format, args)
	}
}
func (l *logAdapter) Warnf(format string, args ...interface{}) {
	if l.base != nil {
		l.emit(l.base.Warn, format, args)
	}
}
func (l *logAdapter) Errorf(format string, args ...interface{}) {
	if l.base != nil {
		l.emit(l.base.Error, format, args)
	}
}

func (l *logAdapter) Trace(args ...interface{}) {
	if l.base != nil {
		l.line(l.base.Debug, args)
	}
}
func (l *logAdapter) Debug(args ...interface{}) {
	if l.base != nil {
		l.line(l.base.Debug, args)
	}
}
func (l *logAdapter) Info(args ...interface{}) {
	if l.base != nil {
		l.line(l.base.Info, args)
	}
}
func (l *logAdapter) Warn(args ...interface{}) {
	if l.base != nil {
		l.line(l.base.Warn, args)
	}
}
func (l *logAdapter) Error(args ...interface{}) {
	if l.base != nil {
		l.line(l.base.Error, args)
	}
}

func (l *logAdapter) WithField(key string, value interface{}) librespot.Logger {
	if l.base == nil {
		return l
	}
	next := append(append([]any(nil), l.fields...), key, value)
	return &logAdapter{base: l.base, fields: next}
}

func (l *logAdapter) WithError(err error) librespot.Logger {
	return l.WithField("error", err)
}
