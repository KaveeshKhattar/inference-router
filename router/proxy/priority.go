package proxy

import (
	"net/http"

	"router/admission"
)

const priorityHeader = "X-Router-Priority"

// RequestPriority reads the PGKeeper priority class from a request header.
func RequestPriority(r *http.Request) admission.Priority {
	return admission.ParsePriority(r.Header.Get(priorityHeader))
}
