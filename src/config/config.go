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
	Addr            string        `mapstructure:"addr"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	UsageConfigFile string        `mapstructure:"usage_file"`
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
	Weight             int           `mapstructure:"weight"`               // 实例权重
	Health             HealthConfig  `mapstructure:"health"`               // 健康检查配置
	KeyPool            []string      `mapstructure:"key_pool"`             // api key 码池
}

type HealthConfig struct {
	Path          string        `mapstructure:"path"`
	Interval      time.Duration `mapstructure:"interval"`       // checker探测间隔
	Timeout       time.Duration `mapstructure:"timeout"`        // checker探测超时时间
	FailThreshold int64         `mapstructure:"fail_threshold"` // 连续失败n次后标记为 unhealthy
	OkThreshold   int64         `mapstructure:"ok_threshold"`   // 连续成功n次后标记为 healthy
}
type TenantConfig struct {
	ID            string   `mapstructure:"id"`
	Name          string   `mapstructure:"name"`
	APIKey        string   `mapstructure:"api_key"`
	AllowedModels []string `mapstructure:"allowed_models"`
	RateLimitRPM  int      `mapstructure:"rate_limit_rpm"`
	RateLimitTPM  int      `mapstructure:"rate_limit_tpm"`
	Balance       int64    `mapstructure:"balance"`
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
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type CacheConfig struct {
	Enable bool          `mapstructure:"enable"`
	TTL    time.Duration `mapstructure:"ttl"`
}
type Config struct {
	Server    ServerConfig     `mapstructure:"server"`
	Transport TransportConfig  `mapstructure:"transport"`
	Redis     RedisConfig      `mapstructure:"redis"`
	Cache     CacheConfig      `mapstructure:"cache"`
	Upstreams []UpstreamConfig `mapstructure:"upstreams"`
	Tenants   []TenantConfig   `mapstructure:"tenants"`
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
		inst.applyDefaults()
	})
	return inst
}

// 部分配置的合法校验及默认值
func (c *Config) applyDefaults() {
	for i := range c.Upstreams {
		u := &c.Upstreams[i]
		if u.PerResponseTimeout <= 0 {
			u.PerResponseTimeout = 5 * time.Minute // 设置默认值为5分钟
		}
		if u.StreamChunkTimeout <= 0 {
			u.StreamChunkTimeout = 10 * time.Second
		}
		if u.Weight <= 0 {
			u.Weight = 1
		}
		if u.Health.Interval <= 0 {
			u.Health.Interval = 5 * time.Second
		}
		if u.Health.Timeout <= 0 {
			u.Health.Timeout = 2 * time.Second
		}
		if u.Health.FailThreshold <= 0 {
			u.Health.FailThreshold = 3
		}
		if u.Health.OkThreshold <= 0 {
			u.Health.OkThreshold = 3
		}
	}
}
