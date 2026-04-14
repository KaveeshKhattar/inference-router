package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages  []ChatMessage `json:"messages"`
	MaxTokens int            `json:"max_tokens"`
}

// estimateRequestTokensAndRestoreBody:
// 1. reads request body
// 2. estimates token usage
// 3. restores body so reverse proxy can reuse it
func estimateRequestTokensAndRestoreBody(r *http.Request) float64 {
	if r.Body == nil {
		return 0
	}

	// read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return 512
	}

	// restore body for downstream proxy
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// decode request
	var req ChatRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return 0
	}

	// extract prompt text
	totalChars := 0
	for _, m := range req.Messages {
		totalChars += len(m.Content)
	}

	// ⚠️ simple approximation (replace later with real tokenizer)
	promptTokens := totalChars / 4

	// final estimate
	return float64(promptTokens + req.MaxTokens)
}