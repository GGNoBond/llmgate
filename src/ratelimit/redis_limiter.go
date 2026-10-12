package ratelimit

import (
	"context"
	"strconv"
	"time"

	"github.com/aoxin/llmgate/src/logger"
	"github.com/redis/go-redis/v9"
)

var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local force = tonumber(ARGV[5])

local bucket = redis.call('HMGET', key, 'tokens', 'last')
local tokens = tonumber(bucket[1])
local last = tonumber(bucket[2])

if tokens == nil then
	tokens = capacity
	last = now
end

local elapsed = (now - last) / 1000000.0
if elapsed > 0 then
	tokens = math.min(capacity, tokens + elapsed * rate)
end

local allowed = 0
if force == 1 or tokens >= requested then
	tokens = tokens - requested
	allowed = 1
end

redis.call('HMSET', key, 'tokens', tostring(tokens), 'last', tostring(now))
redis.call('EXPIRE', key, 120)

return {allowed, tostring(tokens)}
`)

type RedisLimiter struct {
	client *redis.Client
}

func (r *RedisLimiter) Allow(key string, capacity int, perSecond float64, n int) (bool, int) {
	now := time.Now().UnixMicro()
	res, err := tokenBucketScript.Run(context.Background(), r.client,
		[]string{key},
		capacity, perSecond, now, n, 0,
	).Result()
	if err != nil {
		return true, 0
	}
	arr := res.([]any)
	allowed := arr[0].(int64) == 1
	remaining, _ := strconv.ParseFloat(arr[1].(string), 64)
	logger.Get().Trace().Msgf("allow %d  from %s", n, key)
	return allowed, int(remaining)
}

func (r *RedisLimiter) Deduct(key string, capacity int, perSecond float64, n int) {
	now := time.Now().UnixMicro()
	tokenBucketScript.Run(context.Background(), r.client,
		[]string{key},
		capacity, perSecond, now, n, 1,
	)
	logger.Get().Trace().Msgf("deduct %d tokens from %s", n, key)
}

func NewRedisLimiter(client *redis.Client) *RedisLimiter {
	return &RedisLimiter{client: client}
}
