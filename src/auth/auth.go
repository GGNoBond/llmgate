package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/aoxin/llmgate/src/config"
	"github.com/aoxin/llmgate/src/middleware"
	"github.com/aoxin/llmgate/src/utils"
	"github.com/redis/go-redis/v9"
)

type (
	CtxAuthKey struct{}
)

// Tenant 下游租户
type Tenant struct {
	ID            string
	Name          string
	AllowedModels []string
	RateLimitRPM  int
	RateLimitTPM  int
}

type APIKey struct {
	Key      string
	TenantID string
	Disabled bool
}

// Store 租户表 生产环境使用Redis + DB
type Store struct {
	mu      sync.RWMutex
	keys    map[string]*APIKey
	tenants map[string]*Tenant
	rdb     *redis.Client
}

func NewStore(rdb *redis.Client) *Store {
	return &Store{keys: make(map[string]*APIKey), tenants: make(map[string]*Tenant), rdb: rdb}
}

// InitStore 暂时从config读取租户表
func (s *Store) InitStore(ctx context.Context, tenants []config.TenantConfig) {
	for _, t := range tenants {
		s.AddTenant(&Tenant{
			ID:            t.ID,
			Name:          t.Name,
			AllowedModels: t.AllowedModels,
			RateLimitRPM:  t.RateLimitRPM,
			RateLimitTPM:  t.RateLimitTPM,
		})
		s.AddKey(&APIKey{
			Key:      t.APIKey,
			TenantID: t.ID,
		})
		s.rdb.SetNX(ctx, "bal:"+t.ID, t.Balance, 0)
	}
}

func (s *Store) AddTenant(t *Tenant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[t.ID] = t
}
func (s *Store) AddKey(k *APIKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[k.Key] = k
}
func (s *Store) Verify(key string) (*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ak, ok := s.keys[key]
	if !ok {
		return nil, errors.New("invalid api key")
	}
	if ak.Disabled {
		return nil, errors.New("api key is disabled")
	}
	t, ok := s.tenants[ak.TenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return t, nil
}
func (s *Store) GetBalance(ctx context.Context, tenantID string) (int64, error) {
	val, err := s.rdb.Get(ctx, "bal:"+tenantID).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return val, nil
}
func (s *Store) DeductBalance(ctx context.Context, tenantID string, amount int64) int64 {
	return s.rdb.DecrBy(ctx, "bal:"+tenantID, amount).Val()
}
func (s *Store) SetBalance(ctx context.Context, tenantID string, amount int64) {
	s.rdb.Set(ctx, "bal:"+tenantID, amount, 0)
}
func (t *Tenant) ModelAllowed(model string) bool {
	if len(t.AllowedModels) == 0 {
		return true
	}
	return slices.Contains(t.AllowedModels, model)
}

func Auth(store *Store) middleware.MiddleWare {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			key := strings.TrimSpace(strings.TrimPrefix(h, "Bearer"))
			if key == "" {
				utils.WriteJSONErr(w, http.StatusUnauthorized, "api key is missing")
				return
			}
			tenant, err := store.Verify(key)
			if err != nil {
				utils.WriteJSONErr(w, http.StatusUnauthorized, err.Error())
				return
			}
			ctx := context.WithValue(r.Context(), CtxAuthKey{}, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
