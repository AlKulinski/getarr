package arr

import (
	"fmt"
	"net/url"
)

// Prowlarr is a minimal fallback client for direct indexer searches.
type Prowlarr struct {
	BaseURL string
	APIKey  string
}

// Release is a single search result from Prowlarr.
type Release struct {
	Title       string  `json:"title"`
	MagnetURL   string  `json:"magnetUrl"`
	DownloadURL string  `json:"downloadUrl"`
	Size        int64   `json:"size"`
	Seeders     int     `json:"seeders"`
	Peers       int     `json:"peers"`
	Category    []int   `json:"category"`
}

func (p *Prowlarr) Search(query string, categories []int) ([]Release, error) {
	u, _ := url.Parse(p.BaseURL)
	u.Path = "/api/v1/search"
	q := u.Query()
	q.Set("query", query)
	for _, c := range categories {
		q.Add("categories", fmt.Sprintf("%d", c))
	}
	q.Set("limit", "20")
	u.RawQuery = q.Encode()

	var out []Release
	if err := doJSON("GET", p.BaseURL, "/api/v1/search?"+q.Encode(), p.APIKey, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
