package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"router/chat"
)

const prefillerHeader = "x-prefiller-host-port"

// Executor runs sequential prefill → decode for disaggregated inference.
type Executor struct {
	client  *http.Client
	timeout time.Duration
}

func NewExecutor(timeout time.Duration) *Executor {
	return &Executor{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		timeout: timeout,
	}
}

// Prefill sends max_tokens=1 to the prefill pod and waits for completion.
func (e *Executor) Prefill(ctx context.Context, req chat.Request, prefillURL string) error {
	body, err := json.Marshal(prefillPayload(req))
	if err != nil {
		return fmt.Errorf("marshal prefill: %w", err)
	}
	start := time.Now()
	if err := e.postJSON(ctx, prefillURL+"/v1/chat/completions", body, nil); err != nil {
		return err
	}
	log.Printf("execute: prefill done pod=%s took %s", prefillURL, time.Since(start))
	return nil
}

// StreamDecode forwards the decode request with a prefiller hint and streams the response.
func (e *Executor) StreamDecode(
	ctx context.Context,
	w http.ResponseWriter,
	req chat.Request,
	decodeURL, prefillerHostPort string,
) error {
	body, err := json.Marshal(decodePayload(req))
	if err != nil {
		return fmt.Errorf("marshal decode: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, decodeURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build decode request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if prefillerHostPort != "" {
		httpReq.Header.Set(prefillerHeader, prefillerHostPort)
	}

	start := time.Now()
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	defer resp.Body.Close()

	if err := copyResponse(w, resp); err != nil {
		return fmt.Errorf("stream decode: %w", err)
	}
	log.Printf("execute: decode streamed pod=%s prefiller=%s took %s",
		decodeURL, prefillerHostPort, time.Since(start))
	return nil
}

func (e *Executor) postJSON(ctx context.Context, endpoint string, body []byte, extraHeaders map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

type chatPayload struct {
	Model     string         `json:"model"`
	Messages  []chat.Message `json:"messages"`
	MaxTokens int            `json:"max_tokens"`
	Stream    bool           `json:"stream"`
}

func prefillPayload(req chat.Request) chatPayload {
	return chatPayload{
		Model:     chat.ModelOrDefault(req.Model),
		Messages:  req.Messages,
		MaxTokens: 1,
		Stream:    false,
	}
}

func decodePayload(req chat.Request) chatPayload {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 128
	}
	return chatPayload{
		Model:     chat.ModelOrDefault(req.Model),
		Messages:  req.Messages,
		MaxTokens: maxTokens,
		Stream:    true,
	}
}

func copyResponse(w http.ResponseWriter, resp *http.Response) error {
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
