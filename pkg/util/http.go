package util

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"path"
	"time"
)

func UriAppend(uri string, part string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	u.Path = path.Join(u.Path, part)
	return u.String(), nil
}

func UriAppendQuery(uri string, key string, value string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", err
	}

	params.Add(key, value)
	u.RawQuery = params.Encode()
	return u.String(), nil
}

func newTransport(enableHTTP2 bool) *http.Transport {
	protos := []string{"http/1.1"}
	if enableHTTP2 {
		protos = append(protos, "h2")
	}
	return &http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second, // if timeout, error is: timeout awaiting response headers
		TLSHandshakeTimeout:   30 * time.Second,
		TLSClientConfig:       &tls.Config{NextProtos: protos},
		ForceAttemptHTTP2:     enableHTTP2,
	}
}

func NewStreamClient() *http.Client {
	return &http.Client{
		Transport: newTransport(false),
	}
}
