package chat

// Message is one OpenAI-compatible chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a chat completions request body.
type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

const DefaultModel = "meta-llama/Llama-3.1-8B-Instruct"

func ModelOrDefault(m string) string {
	if m != "" {
		return m
	}
	return DefaultModel
}

func PromptText(req Request) string {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	if total == 0 {
		return ""
	}
	b := make([]byte, 0, total)
	for _, m := range req.Messages {
		b = append(b, m.Content...)
	}
	return string(b)
}

func EstimateTokens(req Request) float64 {
	totalChars := 0
	for _, m := range req.Messages {
		totalChars += len(m.Content)
	}
	promptTokens := totalChars / 4
	return float64(promptTokens + req.MaxTokens)
}
