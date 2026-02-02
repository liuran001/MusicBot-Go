package logger

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
)

// Logger wraps slog.Logger to satisfy bot.Logger.
type Logger struct {
	logger  *slog.Logger
	logFile *os.File // Keep reference to close on shutdown
}

// New creates a new Logger with configurable output format.
func New(level, format string, addSource bool) (*Logger, error) {
	logFile, output, err := logOutput()
	if err != nil {
		return nil, err
	}

	options := &slog.HandlerOptions{
		Level:     parseLevel(level),
		AddSource: addSource,
	}

	format = strings.ToLower(strings.TrimSpace(format))
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(output, options)
	} else {
		handler = slog.NewTextHandler(output, options)
	}

	return &Logger{logger: slog.New(handler), logFile: logFile}, nil
}

// With returns a child logger with additional fields.
func (l *Logger) With(args ...any) bot.Logger {
	return &Logger{logger: l.logger.With(args...)}
}

func (l *Logger) Debug(msg string, args ...any) { l.logger.Debug(msg, args...) }
func (l *Logger) Info(msg string, args ...any)  { l.logger.Info(msg, args...) }
func (l *Logger) Warn(msg string, args ...any)  { l.logger.Warn(msg, args...) }
func (l *Logger) Error(msg string, args ...any) { l.logger.Error(msg, args...) }

// Slog returns the underlying slog.Logger.
func (l *Logger) Slog() *slog.Logger {
	return l.logger
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		fallthrough
	default:
		return slog.LevelInfo
	}
}

func logOutput() (*os.File, io.Writer, error) {
	if err := os.MkdirAll("./log", 0755); err != nil {
		return nil, nil, err
	}

	fileName := time.Now().Local().Format("2006-01-02") + ".log"
	filePath := filepath.Join("./log", fileName)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, nil, err
	}

	if file == nil {
		return nil, nil, errors.New("log file handle is nil")
	}

	return file, io.MultiWriter(os.Stdout, file), nil
}

// Close closes the log file handle.
func (l *Logger) Close() error {
	if l == nil || l.logFile == nil {
		return nil
	}
	return l.logFile.Close()
}
