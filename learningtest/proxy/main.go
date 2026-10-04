package main

import (
	"github.com/aoxin/llmgate/learningtest/tinytests"
)

func main() {
	cfg := loadConfig("config/config_learningtest.yaml")
	tinytests.InitLogger(cfg.Logger.LogOutput, cfg.Logger.LogDir, uint8(cfg.Logger.LogLevel))
	ReverseProxyRun(&cfg.Proxy)
}
