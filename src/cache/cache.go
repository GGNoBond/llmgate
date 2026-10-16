package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"time"

	"github.com/aoxin/llmgate/src/openai"
	"github.com/redis/go-redis/v9"
)

// Cache 是 L1 精确响应缓存：对 (model+messages+temperature+max_tokens) 做哈希，
// 命中则直接复用上游的完整 ChatCompletionResponse，跳过限流/计费/上游调用。
// 跨租户共享（key 不含 tenantID），Redis 出错时 fail-open（当作未命中）。
type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

func New(client *redis.Client, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Cache{client: client, ttl: ttl}
}

// keyInput 只保留会影响模型输出的字段，忽略 stream / tenant 等无关字段。
type keyInput struct {
	Model               string               `json:"model"`
	Messages            []openai.ChatMessage `json:"messages"`
	Temperature         *float64             `json:"temperature,omitempty"`
	MaxCompletionTokens *int                 `json:"max_completion_tokens,omitempty"`
}

func (c *Cache) Key(req *openai.ChatCompletionRequest) string {
	ki := keyInput{
		Model:               req.Model,
		Messages:            req.Messages,
		Temperature:         req.Temperature,
		MaxCompletionTokens: req.MaxCompletionTokens,
	}
	raw, _ := json.Marshal(ki)
	sum := sha256.Sum256(raw)
	return "cache:chat:" + hex.EncodeToString(sum[:])
}

// Get 返回缓存的响应；未命中或 Redis 出错时返回 (nil,false)。
func (c *Cache) Get(ctx context.Context, key string) (*openai.ChatCompletionResponse, bool) {
	if c == nil || c.client == nil {
		return nil, false
	}
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false // redis.Nil 或任何错误都 fail-open
	}
	var resp openai.ChatCompletionResponse
	if err := json.Unmarshal(val, &resp); err != nil {
		return nil, false
	}
	return &resp, true
}

// Set 写入缓存，TTL 加随机抖动（±10%）防雪崩。
func (c *Cache) Set(ctx context.Context, key string, resp *openai.ChatCompletionResponse) {
	if c == nil || c.client == nil || resp == nil {
		return
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	jitter := time.Duration(rand.Int63n(int64(c.ttl/5)+1)) - c.ttl/10
	c.client.Set(ctx, key, raw, c.ttl+jitter)
}
