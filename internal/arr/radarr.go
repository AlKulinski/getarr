package arr

import (
	"fmt"
	"net/url"
)

// Radarr is a small client for the Radarr v3/v4 API.
type Radarr struct {
	BaseURL string
	APIKey  string
}

func (r *Radarr) QualityProfiles() ([]QualityProfile, error) {
	var out []QualityProfile
	return out, doJSON("GET", r.BaseURL, "/api/v3/qualityprofile", r.APIKey, nil, &out)
}

func (r *Radarr) Lookup(imdbID, title string, year int) (map[string]any, error) {
	term := ""
	if imdbID != "" {
		term = "imdb:" + imdbID
	} else if year > 0 {
		term = fmt.Sprintf("%s %d", title, year)
	} else {
		term = title
	}
	u := "/api/v3/movie/lookup?term=" + url.QueryEscape(term)
	var out []map[string]any
	if err := doJSON("GET", r.BaseURL, u, r.APIKey, nil, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("radarr: no movie found for %q", term)
	}
	return out[0], nil
}

func (r *Radarr) Add(movie map[string]any, profileID int, rootFolder string, search bool, tags []string) error {
	movie["qualityProfileId"] = profileID
	movie["rootFolderPath"] = rootFolder
	movie["monitored"] = true
	movie["addOptions"] = map[string]any{
		"searchForMovie": search,
	}
	if len(tags) > 0 {
		movie["tags"] = tags
	}
	return doJSON("POST", r.BaseURL, "/api/v3/movie", r.APIKey, movie, nil)
}

func (r *Radarr) Queue() ([]map[string]any, error) {
	var out struct {
		Records []map[string]any `json:"records"`
	}
	if err := doJSON("GET", r.BaseURL, "/api/v3/queue?page=1&pageSize=50", r.APIKey, nil, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}
