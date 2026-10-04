package config

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/aoxin/llmgate/src/utils"
)

type ServerConfig struct {
	Addr         string        `mapstructure:"addr"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type TransportConfig struct {
	MaxIdleConns          int           `mapstructure:"max_idle_conns"`          // 最大空闲连接总数
	MaxIdleConnsPerHost   int           `mapstructure:"max_idle_conns_per_host"` // 单个host最大空闲连接数
	IdleConnTimeout       time.Duration `mapstructure:"idle_conn_timeout"`       // 空闲连接超时时间
	DialContextTimeout    time.Duration `mapstructure:"dial_context_timeout"`    // tcp连接超时时间
	TLSHandshakeTimeout   time.Duration `mapstructure:"tls_handshake_timeout"`   // TLS握手超时时间
	ResponseHeaderTimeout time.Duration `mapstructure:"response_header_timeout"` // 响应头超时时间
}

type UpstreamConfig struct {
	Name               string        `mapstructure:"name"`
	Type               string        `mapstructure:"type"`
	BaseURL            string        `mapstructure:"base_url"`
	APIKey             string        `mapstructure:"api_key"`
	Models             []string      `mapstructure:"models"`
	PerResponseTimeout time.Duration `mapstructure:"per_response_timeout"` // 单个请求超时时间
	StreamChunkTimeout time.Duration `mapstructure:"stream_chunk_timeout"` // chunk最大间隔时间
}
type LoggerConfig struct {
	Level         string `mapstructure:"level"`
	FileEnable    bool   `mapstructure:"file_enable"`
	ConsoleEnable bool   `mapstructure:"console_enable"`
	Filename      string `mapstructure:"filename"`
	MaxSize       int    `mapstructure:"max_size"`
	MaxBackups    int    `mapstructure:"max_backups"`
	MaxAge        int    `mapstructure:"max_age"`
	Compress      bool   `mapstructure:"compress"`
}
type Config struct {
	Server    ServerConfig     `mapstructure:"server"`
	Transport TransportConfig  `mapstructure:"transport"`
	Upstream  []UpstreamConfig `mapstructure:"upstreams"`
	Logger    LoggerConfig     `mapstructure:"logger"`
}

var (
	once sync.Once
	inst *Config
)

// GetConfig - Get config instance
func GetConfig(path string) *Config {
	once.Do(func() {
		root, err := utils.FindProjectRoot()
		if err != nil {
			panic(fmt.Sprintf("Failed to find project root: %v", err))
		}
		path = filepath.Join(root, path)
		viper.SetConfigFile(path)
		viper.SetConfigType("yaml")
		if err := viper.ReadInConfig(); err != nil {
			panic(fmt.Sprintf("Failed to read config file[%s]: %v", path, err))
		}
		inst = &Config{}
		if err := viper.Unmarshal(inst); err != nil {
			panic(fmt.Sprintf("Failed to unmarshal config: %v", err))
		}
	})
	return inst
}
