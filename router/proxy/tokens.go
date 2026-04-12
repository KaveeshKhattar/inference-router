package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
)

func estimateRequestTokensAndRestoreBody(r *http.Request) int64 {
	if r == nil || r.Body == nil {
		return 1
	}

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	return int64(len(body) / 4) // ~4 chars per token
}

// suppress unused import warning if log isn't used elsewhere
var _ = log.Printf