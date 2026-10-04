package logger

import (
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

func Get() *zerolog.Logger {
	return &logger
}
func Init(config *config.LoggerConfig) {
	once.Do(func() {
		var writers []io.Writer
		if config.ConsoleEnable {
			consoleWriter := zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: zerolog.TimeFormatUnix,
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
