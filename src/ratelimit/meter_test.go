package ratelimit

import (
	"testing"

	"github.com/aoxin/llmgate/src/openai"
)

func TestCountText_NonEmpty(t *testing.T) {
	if n := CountText("hello world"); n <= 0 {
		t.Errorf("token 应 > 0, got %d", n)
	}
}

func TestCountMessages_GrowsWithContent(t *testing.T) {
	short := CountMessages([]openai.ChatMessage{{Role: "user", Content: "hi"}})
	long := CountMessages([]openai.ChatMessage{{Role: "user", Content: "讲一个很长很长的故事讲一个很长很长的故事"}})
	if long <= short {
		t.Errorf("更长的消息 token 应更多: short=%d long=%d", short, long)
	}
}
