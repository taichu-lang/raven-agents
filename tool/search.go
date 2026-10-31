package tool

import (
	"context"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// BM25 parameters (Okapi BM25 defaults).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// ToolSearcher ranks the candidate tools for a query. The registry passes its deferred set as candidates, so an
// implementation backed by an external index must restrict its hits to them; the registry additionally drops
// hits outside the set as a backstop.
type ToolSearcher interface {
	Search(ctx context.Context, query string, limit int, candidates []FuncTool) ([]FuncTool, error)
}

// NewBM25Searcher returns the default in-memory ToolSearcher. It indexes the candidates lazily: the index is
// rebuilt whenever the candidate set changes (keyed by a fingerprint of the sorted tool names), so
// RegisterDeferred needs no explicit invalidation. The registry is built per request, so at most one build
// happens per request in practice.
func NewBM25Searcher() ToolSearcher {
	return &bm25Searcher{}
}

type bm25Searcher struct {
	mu          sync.Mutex
	fingerprint string
	docs        []bm25Doc
	idf         map[string]float64
	avgLen      float64
}

type bm25Doc struct {
	tool   FuncTool
	tf     map[string]int
	length float64
}

func (s *bm25Searcher) Search(
	ctx context.Context,
	query string,
	limit int,
	candidates []FuncTool,
) ([]FuncTool, error) {
	queryTokens := tokenize(query)
	hits := make([]FuncTool, 0, limit)
	if len(queryTokens) == 0 || len(candidates) == 0 {
		return hits, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if fingerprint := candidateFingerprint(candidates); fingerprint != s.fingerprint {
		s.build(fingerprint, candidates)
	}

	type scored struct {
		FuncTool
		score float64
	}
	ranked := make([]scored, 0, len(s.docs))
	for _, doc := range s.docs {
		score := 0.0
		for _, token := range queryTokens {
			tf := float64(doc.tf[token])
			if tf == 0 {
				continue
			}

			idf := s.idf[token]
			score += idf * (tf * (bm25K1 + 1)) /
				(tf + bm25K1*(1-bm25B+bm25B*doc.length/s.avgLen))
		}

		if score > 0 {
			ranked = append(ranked, scored{FuncTool: doc.tool, score: score})
		}
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}

		return ranked[i].Name() < ranked[j].Name()
	})

	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	for _, item := range ranked {
		hits = append(hits, item.FuncTool)
	}

	return hits, nil
}

// build (re)indexes the candidate set. The searchable text of a tool is its name, description and top-level
// input parameter names (design doc 3.3).
func (s *bm25Searcher) build(fingerprint string, candidates []FuncTool) {
	docs := make([]bm25Doc, 0, len(candidates))
	df := make(map[string]int)
	totalLen := 0.0
	for _, t := range candidates {
		tokens := toolCorpus(t)
		tf := make(map[string]int, len(tokens))
		for _, token := range tokens {
			tf[token]++
		}
		for token := range tf {
			df[token]++
		}

		docs = append(docs, bm25Doc{tool: t, tf: tf, length: float64(len(tokens))})
		totalLen += float64(len(tokens))
	}

	count := float64(len(docs))
	idf := make(map[string]float64, len(df))
	for token, freq := range df {
		idf[token] = math.Log(1 + (count-float64(freq)+0.5)/(float64(freq)+0.5))
	}

	s.fingerprint = fingerprint
	s.docs = docs
	s.idf = idf
	if count > 0 {
		s.avgLen = totalLen / count
	}
}

// toolCorpus returns the searchable tokens of one tool: name + description + input parameter names.
func toolCorpus(t FuncTool) []string {
	tokens := tokenize(t.Name())
	tokens = append(tokens, tokenize(t.Description())...)
	if schema := t.InSchema(); schema != nil {
		for name := range schema.Properties {
			tokens = append(tokens, tokenize(name)...)
		}
	}

	return tokens
}

// candidateFingerprint keys the lazy index: the sorted candidate names, so any addition or removal invalidates
// it regardless of map iteration order.
func candidateFingerprint(candidates []FuncTool) string {
	names := make([]string, 0, len(candidates))
	for _, t := range candidates {
		names = append(names, t.Name())
	}
	slices.Sort(names)

	return strings.Join(names, "\n")
}

// tokenize splits text on '_', '-', whitespace and other non-alphanumeric runes, then on camelCase boundaries,
// and lowercases the pieces.
func tokenize(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == '_' || r == '-' || (!unicode.IsLetter(r) && !unicode.IsDigit(r))
	})

	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		tokens = append(tokens, splitCamelCase(field)...)
	}

	return tokens
}

// splitCamelCase splits "pdfConvert" into ["pdf", "convert"] and "HTTPServer" into ["http", "server"],
// lowercased.
func splitCamelCase(field string) []string {
	runes := []rune(field)
	parts := make([]string, 0, 1)
	start := 0
	for i := 1; i < len(runes); i++ {
		if !unicode.IsUpper(runes[i]) {
			continue
		}

		// A boundary is a lower/digit followed by an upper, or an upper run followed by an upper+lower pair.
		prevIsLowerOrDigit := unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])
		nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if prevIsLowerOrDigit || (unicode.IsUpper(runes[i-1]) && nextIsLower) {
			parts = append(parts, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}

	return append(parts, strings.ToLower(string(runes[start:])))
}
