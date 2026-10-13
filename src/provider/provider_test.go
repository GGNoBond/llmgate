package provider

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestIsInstanceErr(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantInst bool
		wantCode int
	}{
		{"5xx实例错误", &UpstreamError{StatusCode: 502, Err: http.ErrServerClosed}, true, 502},
		{"429实例错误", &UpstreamError{StatusCode: 429, Err: http.ErrServerClosed}, true, 429},
		{"400非实例错误", &UpstreamError{StatusCode: 400, Err: http.ErrServerClosed}, false, 400},
		{"连接失败", &UpstreamError{StatusCode: 0, Err: http.ErrServerClosed}, true, 0},
		{"包装后的实例错误", fmt.Errorf("dail: %w", &UpstreamError{StatusCode: 0, Err: http.ErrServerClosed}), true, 0},
		{"普通error按502", http.ErrServerClosed, false, http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInst, gotCode := IsInstanceErr(tt.err)
			if gotInst != tt.wantInst || gotCode != tt.wantCode {
				t.Errorf("IsInstanceErr() = %v, %v, want %v, %v", gotInst, gotCode, tt.wantInst, tt.wantCode)
			}
		})
	}
}

func TestIsRateLimit(t *testing.T) {
	_, ok := IsRateLimit(&UpstreamError{StatusCode: 429, RetryAfter: 5 * time.Second, Err: http.ErrServerClosed})
	if !ok {
		t.Error("429 应被识别为限流")
	}
	if _, ok := IsRateLimit(&UpstreamError{StatusCode: 500, Err: http.ErrServerClosed}); ok {
		t.Error("500 不应被识别为限流")
	}

}
