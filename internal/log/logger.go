package log

import (
	"io"
	"os"

	"github.com/sirupsen/logrus"
	"golang.org/x/term"
)

const (
	LevelFatal = iota
	LevelError
	LevelWarn
	LevelInfo
	LevelDebug
)

var levelToLogrus = map[int]logrus.Level{
	LevelFatal: logrus.FatalLevel,
	LevelError: logrus.ErrorLevel,
	LevelWarn:  logrus.WarnLevel,
	LevelInfo:  logrus.InfoLevel,
	LevelDebug: logrus.DebugLevel,
}

// toLogrusLevel maps an internal level to logrus, defaulting to Info for any
// value outside the defined constants (the zero value would be PanicLevel,
// which would silently suppress everything).
func toLogrusLevel(level int) logrus.Level {
	if l, ok := levelToLogrus[level]; ok {
		return l
	}
	return logrus.InfoLevel
}

const timestampFormat = "2006-01-02T15:04:05.000"

// Logger wraps logrus, preserving the package's printf-style API.
type Logger struct {
	l *logrus.Logger
}

func formatterFor(w io.Writer) logrus.Formatter {
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return &logrus.TextFormatter{FullTimestamp: true, TimestampFormat: timestampFormat}
	}
	return &logrus.JSONFormatter{TimestampFormat: timestampFormat}
}

// NewLoggerWithOutput builds a logger writing to w, selecting a TTY-aware formatter.
func NewLoggerWithOutput(level int, w io.Writer) *Logger {
	l := logrus.New()
	l.SetOutput(w)
	l.SetFormatter(formatterFor(w))
	l.SetLevel(toLogrusLevel(level))
	return &Logger{l: l}
}

// NewLogger builds a logger writing to stdout. The console arg is retained for
// API compatibility and ignored (formatter selection is TTY-aware).
func NewLogger(level int, _ bool) *Logger {
	return NewLoggerWithOutput(level, os.Stdout)
}

func (log *Logger) SetLevel(level int) { log.l.SetLevel(toLogrusLevel(level)) }

// SetLogFile redirects log output to the given file (replacing stdout). The file
// is created 0o640 — logs may contain hostnames or error detail, so it is not
// world-readable.
func (log *Logger) SetLogFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	log.l.SetOutput(f)
	log.l.SetFormatter(formatterFor(f))
	return nil
}

func (log *Logger) Fatal(format string, args ...any) { log.l.Fatalf(format, args...) }
func (log *Logger) Error(format string, args ...any) { log.l.Errorf(format, args...) }
func (log *Logger) Warn(format string, args ...any)  { log.l.Warnf(format, args...) }
func (log *Logger) Info(format string, args ...any)  { log.l.Infof(format, args...) }
func (log *Logger) Debug(format string, args ...any) { log.l.Debugf(format, args...) }
