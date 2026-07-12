package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

var apiBase = "https://api.github.com"

// githubRelease is the subset of the GitHub release JSON we care about.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset is the subset of the GitHub asset JSON we care about.
type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

func fetchLatestRelease(ctx context.Context, client *http.Client) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		apiBase+"/repos/andatoshiki/omni/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release JSON: %w", err)
	}
	return &rel, nil
}

func downloadAsset(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("download returned %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}
