package ratelimit

import (
	"github.com/aoxin/llmgate/src/openai"
	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

var enc *tiktoken.Tiktoken

func tokenizerInit() {
	tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	var err error
	enc, err = tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		enc = nil
	}
}
func CountText(s string) int {
	if enc == nil {
		return estimate(s)
	}
	return len(enc.Encode(s, nil, nil))
}
func estimate(s string) int {
	n := 0
	for _, c := range s {
		if c > 0x7F {
			n += 2
		} else {
			n += 1
		}
	}
	return n/4 + 1
}
func CountMessages(msgs []openai.ChatMessage) int {
	total := 0
	for _, m := range msgs {
		total += 4
		total += CountText(m.Content)
		total += CountText(m.Role)
	}
	total += 2
	return total
}
