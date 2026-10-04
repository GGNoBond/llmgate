package tinytests

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	LogLevelDebug = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

func levelToString(lv uint8) string {
	switch lv {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

type logger struct {
	mu     sync.Mutex
	level  uint8
	writer io.Writer
}

var (
	lgr     *logger
	onceLgr sync.Once
)

func logCore(lv uint8, msg string, args ...any) {
	if lgr == nil {
		panic("please call InitLogger first")
	}
	lgr.mu.Lock()
	lgLv := lgr.level
	w := lgr.writer
	lgr.mu.Unlock()

	if lv < lgLv {
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	content := fmt.Sprintf(msg, args...)
	line := fmt.Sprintf("%s [%s] %s\n", now, levelToString(lv), content)
	_, _ = w.Write([]byte(line))

	if lv == LogLevelFatal {
		os.Exit(1)
	}
}

func Debug(msg string, args ...any) { logCore(LogLevelDebug, msg, args...) }
func Info(msg string, args ...any)  { logCore(LogLevelInfo, msg, args...) }
func Warn(msg string, args ...any)  { logCore(LogLevelWarn, msg, args...) }
func Error(msg string, args ...any) { logCore(LogLevelError, msg, args...) }
func Fatal(msg string, args ...any) {
	logCore(LogLevelFatal, msg, args...)
	os.Exit(1)
}
func ConditionDebug(condition bool, msg string, args ...any) {
	if condition {
		Debug(msg, args...)
	}
}
func ConditionInfo(condition bool, msg string, args ...any) {
	if condition {
		Info(msg, args...)
	}
}

func ConditionWarn(condition bool, msg string, args ...any) {
	if condition {
		Warn(msg, args...)
	}
}
func ConditionError(condition bool, msg string, args ...any) {
	if condition {
		Error(msg, args...)
	}
}
func ConditionFatal(condition bool, msg string, args ...any) {
	if condition {
		Fatal(msg, args...)
	}
}

func InitLogger(output []string, logDir string, level uint8) error {
	if lgr == nil {
		onceLgr.Do(func() {
			lgr = &logger{}
		})
	}
	lgr.mu.Lock()
	defer lgr.mu.Unlock()
	lgr.level = level

	var writers []io.Writer
	for _, part := range output {
		part = strings.TrimSpace(part)
		switch part {
		case "stdout":
			writers = append(writers, os.Stdout)
		case "file":
			if logDir == "" {
				return fmt.Errorf("logDir is empty")
			}
			logFile, err := os.OpenFile(logDir, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				return err
			}
			writers = append(writers, logFile)
		}
	}

	if len(writers) == 0 {
		return fmt.Errorf("no writers")
	}
	lgr.writer = io.MultiWriter(writers...)
	return nil
}
