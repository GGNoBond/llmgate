package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/aoxin/llmgate/learningtest/tinytests"
	"gopkg.in/yaml.v3"
)

type ProxyConfig struct {
	Port         int           `yaml:"port"`
	Name         string        `yaml:"name"`
	ForwardPeers []ForwardPeer `yaml:"forward_peers"`
}
type ForwardPeer struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Path     string `yaml:"path"`
	Prefix   string `yaml:"prefix"`
}
type LoggerConfig struct {
	LogLevel  int      `yaml:"level"`
	LogOutput []string `yaml:"output"`
	LogDir    string   `yaml:"dir"`
}

type Config struct {
	Proxy  ProxyConfig  `yaml:"proxy"`
	Logger LoggerConfig `yaml:"logger"`
}

var (
	cfg     *Config
	onceCfg sync.Once
)

func loadConfig(path string) *Config {
	onceCfg.Do(func() {
		fmt.Println("=========== Loading config ===========")
		root, err := tinytests.FindProjectRoot()
		if err != nil {
			panic(fmt.Sprintf("Find project root failed: %v", err))
		}
		path = filepath.Join(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("Load path %s failed: %v", path, err))
		}
		cfg = &Config{}
		err = yaml.Unmarshal(data, cfg)
		if err != nil {
			panic(fmt.Sprintf("Unmarshal YAML failed: %v", err))
		}
		now := time.Now()
		if slices.Contains(cfg.Logger.LogOutput, "file") {
			if cfg.Logger.LogDir == "" {
				cfg.Logger.LogDir = "./log"
			}
			cfg.Logger.LogDir = fmt.Sprintf("%s/%s_%s.log", cfg.Logger.LogDir, cfg.Proxy.Name, now.Format("20060102150405"))
		} else {
			cfg.Logger.LogDir = ""
		}
		cfg.print()
		fmt.Println("=========== loaded success ===========")
	})
	return cfg
}

func (c *Config) print() {
	fmt.Println("Proxy server name:", c.Proxy.Name, "ListenPort:", c.Proxy.Port)
	for _, peer := range c.Proxy.ForwardPeers {
		fmt.Printf("Forward peer name: %s,   \tTo: %s:%d, \tPrefix:%s\n", peer.Name, peer.Host, peer.Port, peer.Prefix)
	}
	fmt.Println("Logger:", c.Logger.LogLevel, c.Logger.LogOutput, c.Logger.LogDir)
}
