package logging

import (
	"fmt"
	"io"
	"log"
	"os"
)

// Level represents a log severity level.
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
}

// Logger provides structured key=value logging.
type Logger struct {
	level  Level
	logger *log.Logger
}

// New creates a Logger that writes to stderr with the given minimum level.
func New(level Level) *Logger {
	return &Logger{
		level:  level,
		logger: log.New(os.Stderr, "", 0),
	}
}

// SetOutput changes the output destination.
func (l *Logger) SetOutput(w io.Writer) {
	l.logger.SetOutput(w)
}

// SetLevel changes the minimum log level.
func (l *Logger) SetLevel(level Level) {
	l.level = level
}

// Level returns the current minimum log level.
func (l *Logger) Level() Level {
	return l.level
}

func (l *Logger) log(level Level, msg string, kvs ...interface{}) {
	if level < l.level {
		return
	}

	line := fmt.Sprintf("%-5s %s", levelNames[level], msg)
	if len(kvs) > 0 {
		pairs := make([]string, 0, len(kvs)/2)
		for i := 0; i+1 < len(kvs); i += 2 {
			pairs = append(pairs, fmt.Sprintf("%s=%v", kvs[i], kvs[i+1]))
		}
		// If odd number of kvs, append the last one with a key of "extra"
		if len(kvs)%2 != 0 {
			pairs = append(pairs, fmt.Sprintf("extra=%v", kvs[len(kvs)-1]))
		}
		line += " " + joinPairs(pairs)
	}

	l.logger.Print(line)
}

func joinPairs(pairs []string) string {
	s := ""
	for i, p := range pairs {
		if i > 0 {
			s += " "
		}
		s += p
	}
	return s
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, kvs ...interface{}) {
	l.log(DEBUG, msg, kvs...)
}

// Info logs an info message.
func (l *Logger) Info(msg string, kvs ...interface{}) {
	l.log(INFO, msg, kvs...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, kvs ...interface{}) {
	l.log(WARN, msg, kvs...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, kvs ...interface{}) {
	l.log(ERROR, msg, kvs...)
}

// DefaultLogger is a package-level logger for backward compatibility.
var defaultLogger = New(INFO)

// SetDefault sets the package-level default logger.
func SetDefault(l *Logger) {
	defaultLogger = l
}

// Default returns the package-level default logger.
func Default() *Logger {
	return defaultLogger
}

// Package-level convenience functions.
func Debug(msg string, kvs ...interface{}) { defaultLogger.Debug(msg, kvs...) }
func Info(msg string, kvs ...interface{})  { defaultLogger.Info(msg, kvs...) }
func Warn(msg string, kvs ...interface{})  { defaultLogger.Warn(msg, kvs...) }
func Error(msg string, kvs ...interface{}) { defaultLogger.Error(msg, kvs...) }

// StdLogger returns a *log.Logger that writes through this logger at INFO level.
// Useful for passing to http.Server.ErrorLog or similar.
func (l *Logger) StdLogger() *log.Logger {
	return log.New(&loggerWriter{l: l, level: INFO}, "", 0)
}

type loggerWriter struct {
	l     *Logger
	level Level
}

func (w *loggerWriter) Write(p []byte) (int, error) {
	w.l.log(w.level, string(p))
	return len(p), nil
}
