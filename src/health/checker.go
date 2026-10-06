package health

import (
	"context"
	"net/http"
	"time"

	"github.com/aoxin/llmgate/src/logger"
	"github.com/aoxin/llmgate/src/router"
)

type Checker struct {
	client *http.Client
}

func NewChecker() *Checker {
	return &Checker{client: &http.Client{}}
}

// Start 每个node起一个goroutine发送health check探针
func (c *Checker) Start(nodes []*router.Node) {
	for _, node := range nodes {
		go func(node *router.Node) {
			ticker := time.NewTicker(node.Backend.HealthCfg.Interval)
			defer ticker.Stop()
			for range ticker.C {
				healthURL := node.Provider.BaseURL() + node.Backend.HealthCfg.Path
				ctx, cancel := context.WithTimeout(context.Background(), node.Backend.HealthCfg.Timeout)
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
				resp, err := c.client.Do(req)
				cancel()

				alive := err == nil && resp.StatusCode < http.StatusInternalServerError
				if resp != nil {
					resp.Body.Close()
				}
				if alive {
					node.Backend.ResetFail()
					if !node.Backend.Healthy() && node.Backend.RecordOK() {
						node.Backend.SetHealthy(true)
						logger.Get().Info().Msgf("node %s set to healthy", node.Backend.ID)
					}

				} else {
					node.Backend.ResetOK()
					if node.Backend.Healthy() && node.Backend.RecordFail() {
						node.Backend.SetHealthy(false)
						logger.Get().Info().Msgf("node %s set to unhealthy", node.Backend.ID)
					}
				}
			}
		}(node)
	}
}
