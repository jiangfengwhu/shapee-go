package notes

import (
	"context"
	"fmt"
	"io"
	"keepy-go/config"
	"net/http"
)

// FetchURLMarkdown calls the Jina Reader API to convert a URL to markdown.
func FetchURLMarkdown(ctx context.Context, cfg *config.JinaConfig, url string) (string, error) {
	apiURL := "https://r.jina.ai/" + url

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "text/markdown")

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
		return "", fmt.Errorf("jina API error (status %d): %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}
