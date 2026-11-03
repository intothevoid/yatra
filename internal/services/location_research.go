package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/config"
)

type LocationResearch struct {
	Summary string
	Sources []string
}

// ResearchLocation uses the configured writer model by default. When web search
// is enabled, source snippets from Brave are passed into the writing prompt.
func ResearchLocation(ctx context.Context, cfg *config.Config, text adapters.TextAdapter, location string) (LocationResearch, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return LocationResearch{}, nil
	}
	var sourceText string
	var sources []string
	if cfg != nil && cfg.LocationWebSearch {
		if cfg.BraveSearchAPIKey == "" {
			return LocationResearch{}, fmt.Errorf("location web search is enabled but BRAVE_SEARCH_API_KEY is empty")
		}
		var err error
		sourceText, sources, err = braveLocationSearch(ctx, cfg.BraveSearchAPIKey, location)
		if err != nil {
			return LocationResearch{}, err
		}
	}
	prompt := "Give 4 to 6 concise, stable background facts about " + location + " that could help introduce a personal travel story. Keep the tone conversational and informal. Separate verified place context from anything that depends on personal experience. Do not invent specific dates, prices, or personal observations. Clearly say when a detail is uncertain."
	if sourceText != "" {
		prompt += "\n\nUse only these web search snippets for factual claims. Do not add claims that are absent from the snippets.\n" + sourceText
	}
	summary, err := text.Generate(ctx, prompt)
	if err != nil {
		return LocationResearch{}, fmt.Errorf("research location: %w", err)
	}
	return LocationResearch{Summary: strings.TrimSpace(summary), Sources: sources}, nil
}

func braveLocationSearch(ctx context.Context, apiKey, location string) (string, []string, error) {
	u := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(location+" travel history culture geography") + "&count=5"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("search location: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("location search returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				URL         string `json:"url"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, fmt.Errorf("decode location search: %w", err)
	}
	var snippets []string
	var sources []string
	for _, item := range result.Web.Results {
		snippets = append(snippets, fmt.Sprintf("%s: %s (%s)", item.Title, item.Description, item.URL))
		sources = append(sources, item.URL)
	}
	if len(snippets) == 0 {
		return "", nil, fmt.Errorf("location search returned no useful sources")
	}
	return strings.Join(snippets, "\n"), sources, nil
}
