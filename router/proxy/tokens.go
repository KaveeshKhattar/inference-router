package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"router/chat"
)

type ChatMessage = chat.Message
type ChatRequest = chat.Request

// ParseChatRequest reads the body, restores it for the reverse proxy, and
// returns the parsed request plus a token estimate for load scoring.
func ParseChatRequest(r *http.Request) (ChatRequest, float64) {
	if r.Body == nil {
		return ChatRequest{}, 0
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return ChatRequest{}, 512
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req ChatRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return ChatRequest{}, 0
	}
	return req, chat.EstimateTokens(req)
}

func PromptText(req ChatRequest) string {
	return chat.PromptText(req)
}