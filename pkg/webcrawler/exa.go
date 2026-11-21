package webcrawler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/taichu-lang/raven-agents/agent/util"
)

const (
	defaultExaBaseURL = "https://api.exa.ai"
	exaMaxResults     = 100
	exaMaxFetchURLs   = 100
	exaErrorBodyLimit = 1024
)

type ExaConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url"`
	ApiKey  string `yaml:"api_key"  json:"api_key"`
}

type ExaCrawler struct {
	BaseURL string
	ApiKey  string

	httpClient *http.Client
}

var _ Crawler = (*ExaCrawler)(nil)

func NewExaCrawler(cfg *ExaConfig) *ExaCrawler {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultExaBaseURL
	}

	return &ExaCrawler{
		BaseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		ApiKey:     cfg.ApiKey,
		httpClient: util.StreamClient,
	}
}

type exaSearchContents struct {
	Highlights bool `json:"highlights,omitempty"`
}

type exaSearchRequest struct {
	Query      string            `json:"query"`
	Type       string            `json:"type,omitempty"`
	NumResults int               `json:"numResults,omitempty"`
	Contents   exaSearchContents `json:"contents"`
}

type exaResultItem struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	URL        string   `json:"url"`
	Text       string   `json:"text"`
	Summary    string   `json:"summary"`
	Highlights []string `json:"highlights"`
}

type exaSearchResponse struct {
	Results []exaResultItem `json:"results"`
}

type exaContentsRequest struct {
	URLs []string `json:"urls"`
	Text bool     `json:"text"`
}

type exaContentsStatusError struct {
	Tag            string `json:"tag"`
	HTTPStatusCode int    `json:"httpStatusCode"`
}

type exaContentsStatus struct {
	ID     string                  `json:"id"`
	Status string                  `json:"status"`
	Error  *exaContentsStatusError `json:"error"`
}

type exaContentsResponse struct {
	Results  []exaResultItem     `json:"results"`
	Statuses []exaContentsStatus `json:"statuses"`
}

func exaSearchType(effort SearchEffort) string {
	if effort == SearchEffortLow {
		return "lite"
	}

	return "auto"
}

func (e *ExaCrawler) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if req == nil || strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("exa search: query is required")
	}

	body := exaSearchRequest{
		Query:      req.Query,
		Type:       exaSearchType(req.Effort),
		NumResults: min(req.MaxResults, exaMaxResults),
		Contents:   exaSearchContents{Highlights: true},
	}

	var raw exaSearchResponse
	if err := e.post(ctx, "/search", body, &raw); err != nil {
		return nil, fmt.Errorf("exa search: %w", err)
	}

	results := make([]*SearchResult, 0, len(raw.Results))
	for _, item := range raw.Results {
		results = append(results, &SearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: strings.Join(item.Highlights, "\n"),
			Summary: item.Summary,
		})
	}

	return &SearchResponse{Results: results}, nil
}

func (e *ExaCrawler) Fetch(ctx context.Context, req *FetchRequest) (*FetchResponse, error) {
	if len(req.URLs) == 0 {
		return nil, errors.New("exa fetch: at least one url is required")
	}

	if len(req.URLs) > exaMaxFetchURLs {
		return nil, fmt.Errorf("exa fetch: at most %d urls are allowed, got %d", exaMaxFetchURLs, len(req.URLs))
	}

	var raw exaContentsResponse
	if err := e.post(ctx, "/contents", exaContentsRequest{URLs: req.URLs, Text: true}, &raw); err != nil {
		return nil, fmt.Errorf("exa fetch: %w", err)
	}

	logger := slog.With("provider", "exa")
	failedCount := 0
	firstFailure := ""
	for _, status := range raw.Statuses {
		if status.Status != "error" {
			continue
		}

		reason := "unknown"
		if status.Error != nil {
			reason = fmt.Sprintf("%s (http status %d)", status.Error.Tag, status.Error.HTTPStatusCode)
		}
		if failedCount == 0 {
			firstFailure = reason
		}
		failedCount++
		logger.WarnContext(ctx, "failed to fetch url", "url", status.ID, "error", reason)
	}

	results := make([]FetchResult, 0, len(raw.Results))
	for _, item := range raw.Results {
		results = append(results, FetchResult{URL: item.URL, RawContent: item.Text})
	}

	if len(results) == 0 && failedCount > 0 {
		return nil, fmt.Errorf("exa fetch: all urls failed, first error: %s", firstFailure)
	}

	return &FetchResponse{Results: results}, nil
}

func (e *ExaCrawler) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+path, bytes.NewReader(payload))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.ApiKey)

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, exaErrorBodyLimit))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
