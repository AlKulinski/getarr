package arr

import (
	"fmt"
	"net/url"
	"strconv"
)

// Sonarr is a small client for the Sonarr v3/v4 API.
type Sonarr struct {
	BaseURL string
	APIKey  string
}

func (s *Sonarr) QualityProfiles() ([]QualityProfile, error) {
	var out []QualityProfile
	return out, doJSON("GET", s.BaseURL, "/api/v3/qualityprofile", s.APIKey, nil, &out)
}

func (s *Sonarr) Lookup(imdbID, title string, year int) (map[string]any, error) {
	term := ""
	if imdbID != "" {
		term = "imdb:" + imdbID
	} else if year > 0 {
		term = fmt.Sprintf("%s %d", title, year)
	} else {
		term = title
	}
	u := "/api/v3/series/lookup?term=" + url.QueryEscape(term)
	var out []map[string]any
	if err := doJSON("GET", s.BaseURL, u, s.APIKey, nil, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sonarr: no series found for %q", term)
	}
	return out[0], nil
}

func (s *Sonarr) Add(series map[string]any, profileID int, rootFolder, monitor string, search bool, tags []string) (int, error) {
	series["qualityProfileId"] = profileID
	series["rootFolderPath"] = rootFolder
	series["monitored"] = true
	series["seasonFolder"] = true
	series["addOptions"] = map[string]any{
		"monitor":                  monitor,
		"searchForMissingEpisodes": search,
	}
	if len(tags) > 0 {
		series["tags"] = tags
	}

	var out map[string]any
	if err := doJSON("POST", s.BaseURL, "/api/v3/series", s.APIKey, series, &out); err != nil {
		return 0, err
	}
	idF, _ := out["id"].(float64)
	return int(idF), nil
}

func (s *Sonarr) GetEpisodes(seriesID int) ([]map[string]any, error) {
	u := "/api/v3/episode?seriesId=" + strconv.Itoa(seriesID)
	var out []map[string]any
	return out, doJSON("GET", s.BaseURL, u, s.APIKey, nil, &out)
}

func (s *Sonarr) MonitorEpisodes(episodeIDs []int) error {
	payload := map[string]any{
		"episodeIds": episodeIDs,
		"monitored":  true,
	}
	return doJSON("PUT", s.BaseURL, "/api/v3/episode/monitor", s.APIKey, payload, nil)
}

func (s *Sonarr) Command(name string, payload map[string]any) error {
	body := map[string]any{"name": name}
	for k, v := range payload {
		body[k] = v
	}
	return doJSON("POST", s.BaseURL, "/api/v3/command", s.APIKey, body, nil)
}

func (s *Sonarr) Queue() ([]map[string]any, error) {
	var out struct {
		Records []map[string]any `json:"records"`
	}
	if err := doJSON("GET", s.BaseURL, "/api/v3/queue?page=1&pageSize=50", s.APIKey, nil, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}
