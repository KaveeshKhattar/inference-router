package execute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"router/chat"
)

func TestPrefillThenStreamDecode(t *testing.T) {
	var prefillGot, decodeGot bool
	var decodeHeader string

	prefill := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefillGot = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer prefill.Close()

	decode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeGot = true
		decodeHeader = r.Header.Get("x-prefiller-host-port")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"ok\":true}\n\n"))
	}))
	defer decode.Close()

	ex := NewExecutor(0)
	req := chat.Request{
		Model:     "test-model",
		Messages:  []chat.Message{{Role: "user", Content: "hello"}},
		MaxTokens: 32,
	}

	pu, _ := url.Parse(prefill.URL)
	prefillerHostPort := pu.Host

	if err := ex.Prefill(context.Background(), req, prefill.URL); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	if err := ex.StreamDecode(context.Background(), rec, req, decode.URL, prefillerHostPort); err != nil {
		t.Fatal(err)
	}

	if !prefillGot || !decodeGot {
		t.Fatalf("prefill=%v decode=%v", prefillGot, decodeGot)
	}
	if decodeHeader != prefillerHostPort {
		t.Fatalf("header=%q want %q", decodeHeader, prefillerHostPort)
	}
	if !strings.Contains(rec.Body.String(), "data:") {
		t.Fatalf("expected streamed body, got %q", rec.Body.String())
	}
}
