package logger

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/aoxin/llmgate/src/config"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	once   sync.Once
	logger zerolog.Logger
)

// traceKey 是 ctx 里存 traceID 的私有 key（放 logger 包避免 logger→middleware 循环依赖）。
type traceKey struct{}

// WithTraceID 把 traceID 绑定进 ctx。
func WithTraceID(ctx context.Context, tid string) context.Context {
	return context.WithValue(ctx, traceKey{}, tid)
}

// Ctx 返回带 trace_id 字段的 logger；ctx 里没有 traceID 时退回全局 logger。
func Ctx(ctx context.Context) *zerolog.Logger {
	if ctx != nil {
		if tid, ok := ctx.Value(traceKey{}).(string); ok && tid != "" {
			l := logger.With().Str("trace_id", tid).Logger()
			return &l
		}
	}
	return &logger
}

func Get() *zerolog.Logger {
	return &logger
}
func Init(config *config.LoggerConfig) {
	once.Do(func() {
		var writers []io.Writer
		if config.ConsoleEnable {
			consoleWriter := zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: "15:04:05 PM",
			}
			writers = append(writers, consoleWriter)
		}
		if config.FileEnable {
			lumber := &lumberjack.Logger{
				Filename:   config.Filename,
				MaxSize:    config.MaxSize,
				MaxBackups: config.MaxBackups,
				MaxAge:     config.MaxAge,
				Compress:   config.Compress,
			}
			writers = append(writers, lumber)
		}
		mw := io.MultiWriter(writers...)
		logger = zerolog.New(mw).With().Timestamp().Logger()
		setLevel(config.Level)
	})
}

func setLevel(level string) {
	switch level {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
