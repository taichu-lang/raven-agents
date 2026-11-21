package webcrawler

import "context"

type SearchEffort string

const (
	SearchEffortLow    SearchEffort = "low"
	SearchEffortMedium SearchEffort = "medium"
	SearchEffortHigh   SearchEffort = "high"
)

type FetchFormat string

const (
	FetchFormatMarkdown FetchFormat = "markdown"
	FetchFormatText     FetchFormat = "text"
)

type SearchRequest struct {
	Query      string       `json:"query"                jsonschema:"Web search query."`
	Effort     SearchEffort `json:"effort,omitzero"      jsonschema:"Controls the latency vs. relevance tradeoff and how search result is generated. Defaults to medium."`
	MaxResults int          `json:"max_results,omitzero" jsonschema:"The maximum number of search results to return. Defaults to 10."`
}

type SearchResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`

	// Snippet is the short description of the search result.
	Snippet string `json:"snippet,omitzero"`
	Summary string `json:"summary,omitzero"`
}

type SearchResponse struct {
	Results []*SearchResult `json:"results"`
}

type FetchRequest struct {
	Query string   `json:"query,omitzero"`
	URLs  []string `json:"urls"`
}

type FetchResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

type FetchResponse struct {
	Results []FetchResult `json:"results"`
}

type Crawler interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
	Fetch(context.Context, *FetchRequest) (*FetchResponse, error)
}

type Config struct {
	Exa    *ExaConfig    `yaml:"exa,omitempty"    json:"exa,omitzero"`
	Tavily *TavilyConfig `yaml:"tavily,omitempty" json:"tavily,omitzero"`
}

func NewCrawler(cfg *Config) Crawler {
	if cfg.Exa != nil {
		return NewExaCrawler(cfg.Exa)
	}

	if cfg.Tavily != nil {
		return NewTavilyCrawler(cfg.Tavily)
	}

	return nil
}
