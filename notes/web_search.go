package notes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"keepy-go/config"
	"net/http"
	"net/url"
	"strings"
)

// searchAPIResponse represents the response from SearchAPI.io
type searchAPIResponse struct {
	KnowledgeGraph *knowledgeGraph `json:"knowledge_graph,omitempty"`
	OrganicResults []organicResult `json:"organic_results,omitempty"`
	RelatedSearches []relatedSearch `json:"related_searches,omitempty"`
}

type knowledgeGraph struct {
	Title       string `json:"title"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Source      *struct {
		Name string `json:"name"`
		Link string `json:"link"`
	} `json:"source,omitempty"`
}

type organicResult struct {
	Position     int    `json:"position"`
	Title        string `json:"title"`
	Link         string `json:"link"`
	DisplayedLink string `json:"displayed_link"`
	Snippet      string `json:"snippet"`
	Date         string `json:"date,omitempty"`
}

type relatedSearch struct {
	Query string `json:"query"`
}

// compactSearchResult is a simplified result returned to the LLM.
type compactSearchResult struct {
	KnowledgeGraph *knowledgeGraph  `json:"knowledge_graph,omitempty"`
	Results        []compactOrganic `json:"results"`
	RelatedQueries []string         `json:"related_queries,omitempty"`
}

type compactOrganic struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
	Date    string `json:"date,omitempty"`
}

// WebSearch calls the SearchAPI.io Google Light search and returns a compact JSON result.
func WebSearch(ctx context.Context, cfg *config.SearchAPIConfig, query string) (string, error) {
	params := url.Values{}
	params.Set("api_key", cfg.APIKey)
	params.Set("engine", "google_light")
	params.Set("q", query)

	apiURL := "https://www.searchapi.io/api/v1/search?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("searchapi error (status %d): %s", resp.StatusCode, string(body))
	}

	var raw searchAPIResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	// Build compact result for the LLM
	compact := compactSearchResult{
		KnowledgeGraph: raw.KnowledgeGraph,
	}

	for _, r := range raw.OrganicResults {
		compact.Results = append(compact.Results, compactOrganic{
			Title:   r.Title,
			Link:    r.Link,
			Snippet: r.Snippet,
			Date:    r.Date,
		})
	}

	var related []string
	for _, r := range raw.RelatedSearches {
		related = append(related, r.Query)
	}
	compact.RelatedQueries = related

	// Marshal to JSON string
	out, err := json.Marshal(compact)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}

	// Truncate if too long to avoid excessive token usage
	result := string(out)
	const maxLen = 8000
	if len(result) > maxLen {
		result = result[:maxLen]
		// Try to cut at the last complete JSON object boundary
		if idx := strings.LastIndex(result, "},"); idx > 0 {
			result = result[:idx+1] + "]}"
		}
	}

	return result, nil
}
