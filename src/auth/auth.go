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
)

// Tenant 下游租户
type Tenant struct {
	ID            string
	Name          string
	AllowedModels []string
	RateLimitRPM  int
	Balance       int64
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
}

func NewStore() *Store {
	return &Store{keys: make(map[string]*APIKey), tenants: make(map[string]*Tenant)}
}

// InitStore 暂时从config读取租户表
func (s *Store) InitStore(tenants []config.TenantConfig) {
	for _, t := range tenants {
		s.AddTenant(&Tenant{
			ID:            t.ID,
			Name:          t.Name,
			AllowedModels: t.AllowedModels,
			RateLimitRPM:  t.RateLimitRPM,
			Balance:       t.Balance,
		})
		s.AddKey(&APIKey{
			Key:      t.APIKey,
			TenantID: t.ID,
		})
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
			ctx := context.WithValue(r.Context(), middleware.CtxAuthKey{}, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
