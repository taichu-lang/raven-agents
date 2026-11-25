package mcp

import "net/http"

// headerRoundTripper adds the configured headers (ex: authorization) to every request of the session.
type headerRoundTripper struct {
	headers map[string]string
	next    http.RoundTripper
}

func (r *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for name, value := range r.headers {
		req.Header.Set(name, value)
	}

	return r.next.RoundTrip(req)
}

func newHeaderClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil
	}

	return &http.Client{Transport: &headerRoundTripper{headers: headers, next: http.DefaultTransport}}
}
