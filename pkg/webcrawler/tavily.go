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
	defaultTavilyBaseURL = "https://api.tavily.com"
	tavilyMaxResults     = 20
	tavilyErrorBodyLimit = 1024
)

type TavilyConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url"`
	ApiKey  string `yaml:"api_key"  json:"api_key"`
}

type TavilyCrawler struct {
	BaseURL string
	ApiKey  string

	httpClient *http.Client
}

var _ Crawler = (*TavilyCrawler)(nil)

func NewTavilyCrawler(cfg *TavilyConfig) *TavilyCrawler {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultTavilyBaseURL
	}

	return &TavilyCrawler{
		BaseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		ApiKey:     cfg.ApiKey,
		httpClient: util.StreamClient,
	}
}

type tavilySearchRequest struct {
	Query       string `json:"query"`
	SearchDepth string `json:"search_depth,omitempty"`
	MaxResults  int    `json:"max_results,omitempty"`
}

type tavilySearchItem struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Content    string  `json:"content"`
	RawContent string  `json:"raw_content"`
	Score      float32 `json:"score"`
}

type tavilySearchResponse struct {
	Results []tavilySearchItem `json:"results"`
}

type tavilyExtractRequest struct {
	URLs   []string `json:"urls"`
	Format string   `json:"format,omitzero"`
	Query  string   `json:"query,omitzero"`
}

type tavilyExtractItem struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

type tavilyExtractFailure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type tavilyExtractResponse struct {
	Results       []tavilyExtractItem    `json:"results"`
	FailedResults []tavilyExtractFailure `json:"failed_results"`
}

func tavilySearchDepth(effort SearchEffort) string {
	switch effort {
	case SearchEffortLow:
		return "fast"
	case SearchEffortHigh:
		return "advanced"
	default:
		return "basic"
	}
}

func (t *TavilyCrawler) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if req == nil || strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("tavily search: query is required")
	}

	body := tavilySearchRequest{
		Query:       req.Query,
		SearchDepth: tavilySearchDepth(req.Effort),
		MaxResults:  min(req.MaxResults, tavilyMaxResults),
	}

	var raw tavilySearchResponse
	if err := t.post(ctx, "/search", body, &raw); err != nil {
		return nil, fmt.Errorf("tavily search: %w", err)
	}

	results := make([]*SearchResult, 0, len(raw.Results))
	for _, item := range raw.Results {
		results = append(results, &SearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Content,
		})
	}
	return &SearchResponse{Results: results}, nil
}

func (t *TavilyCrawler) Fetch(ctx context.Context, req *FetchRequest) (*FetchResponse, error) {
	if len(req.URLs) == 0 {
		return nil, errors.New("tavily fetch: at least one url is required")
	}

	// Without 'query', tavily will fetch full page content. With 'query', tavily only return short content
	// snippets (about 800 characters each) * chunks_per_source (defaults to 3).
	body := tavilyExtractRequest{URLs: req.URLs, Format: string(FetchFormatMarkdown), Query: req.Query}

	var raw tavilyExtractResponse
	if err := t.post(ctx, "/extract", body, &raw); err != nil {
		return nil, fmt.Errorf("tavily fetch: %w", err)
	}

	logger := slog.With("provider", "tavily")
	for _, failure := range raw.FailedResults {
		logger.WarnContext(ctx, "failed to extract url", "url", failure.URL, "error", failure.Error)
	}

	results := make([]FetchResult, 0, len(raw.Results))
	for _, item := range raw.Results {
		results = append(results, FetchResult{URL: item.URL, RawContent: item.RawContent})
	}

	if len(results) == 0 && len(raw.FailedResults) > 0 {
		return nil, fmt.Errorf("tavily fetch: all urls failed, first error: %s", raw.FailedResults[0].Error)
	}

	return &FetchResponse{Results: results}, nil
}

func (t *TavilyCrawler) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+path, bytes.NewReader(payload))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+t.ApiKey)

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, tavilyErrorBodyLimit))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
