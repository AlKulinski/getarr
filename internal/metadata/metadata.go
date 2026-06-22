package metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Content type discovered from a media page.
type Type string

const (
	TypeMovie   Type = "movie"
	TypeSeries  Type = "series"
	TypeSeason  Type = "season"
	TypeEpisode Type = "episode"
	TypeUnknown Type = "unknown"
)

// Options tunes metadata extraction.
type Options struct {
	OMDbKey string
}

// Info holds the parsed metadata we need to add something to *arr.
type Info struct {
	Source string

	Type Type

	Title string
	Year  int

	ImdbID       string // the IMDb ID of the page itself
	SeriesImdbID string // for episodes/seasons, the parent series

	SeasonNumber  int
	EpisodeNumber int
}

var (
	imdbIDRe    = regexp.MustCompile(`(?i)/title/(tt\d+)`)
	jsonldRe    = regexp.MustCompile(`(?s)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	titleRe     = regexp.MustCompile(`(?s)<title[^>]*>(.*?)</title>`)
	jsonpRe     = regexp.MustCompile(`(?s)^[^\(]*\((.*)\)[^\)]*$`)
	firstYearRe = regexp.MustCompile(`(\d{4})`)
)

// FromURL fetches a media page and extracts the relevant metadata.
func FromURL(raw string, opts Options) (*Info, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}

	source := detectSource(u)
	info := &Info{Source: source, Type: TypeUnknown}

	switch source {
	case "imdb":
		info.ImdbID = extractImdbID(u.Path)
		if err := imdbMetadata(info, opts); err != nil {
			return nil, err
		}
		// URL-based overrides for IMDb season pages.
		if strings.HasPrefix(u.Path, "/title/") && strings.Contains(u.Path, "/episodes") {
			info.Type = TypeSeason
			if s := u.Query().Get("season"); s != "" {
				info.SeasonNumber, _ = strconv.Atoi(s)
			}
			info.ImdbID = extractImdbID(u.Path)
		}
	case "rottentomatoes":
		if err := rottentomatoesMetadata(info, u); err != nil {
			guessFromPath(u, info)
		}
	default:
		guessFromPath(u, info)
	}

	return info, nil
}

func detectSource(u *url.URL) string {
	if strings.Contains(u.Host, "imdb.com") {
		return "imdb"
	}
	if imdbIDRe.MatchString(u.Path) || (strings.HasPrefix(u.Path, "/title/") && strings.Contains(u.Path, "/episodes")) {
		return "imdb"
	}
	if strings.Contains(u.Host, "rottentomatoes.com") {
		return "rottentomatoes"
	}
	if strings.HasPrefix(u.Path, "/m/") || strings.HasPrefix(u.Path, "/tv/") {
		return "rottentomatoes"
	}
	return u.Host
}

func imdbMetadata(info *Info, opts Options) error {
	if info.ImdbID == "" {
		return fmt.Errorf("no imdb id in url")
	}
	if opts.OMDbKey != "" {
		return omdbLookup(info, opts.OMDbKey)
	}
	return imdbSuggestionLookup(info)
}

func omdbLookup(info *Info, key string) error {
	u := "https://www.omdbapi.com/?i=" + info.ImdbID + "&apikey=" + url.QueryEscape(key)
	body, err := fetch(u)
	if err != nil {
		return err
	}
	var r struct {
		Title    string `json:"Title"`
		Year     string `json:"Year"`
		Type     string `json:"Type"`
		Season   string `json:"Season"`
		Episode  string `json:"Episode"`
		SeriesID string `json:"seriesID"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return err
	}
	info.Title = r.Title
	info.Year = firstYear(r.Year)
	switch r.Type {
	case "movie":
		info.Type = TypeMovie
	case "series":
		info.Type = TypeSeries
	case "episode":
		info.Type = TypeEpisode
		info.SeriesImdbID = r.SeriesID
		info.SeasonNumber, _ = strconv.Atoi(r.Season)
		info.EpisodeNumber, _ = strconv.Atoi(r.Episode)
	}
	return nil
}

func imdbSuggestionLookup(info *Info) error {
	u := "https://v2.sg.media-imdb.com/suggests/t/" + info.ImdbID + ".json"
	body, err := fetch(u)
	if err != nil {
		return err
	}
	m := jsonpRe.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return fmt.Errorf("unexpected imdb suggestion response")
	}
	var r struct {
		D []struct {
			Title string `json:"l"`
			Year  int    `json:"y"`
			QID   string `json:"qid"`
		} `json:"d"`
	}
	if err := json.Unmarshal([]byte(m[1]), &r); err != nil {
		return err
	}
	if len(r.D) == 0 {
		return fmt.Errorf("imdb suggestion returned no results")
	}
	first := r.D[0]
	info.Title = first.Title
	info.Year = first.Year
	switch first.QID {
	case "movie":
		info.Type = TypeMovie
	case "tvSeries", "tvMiniSeries":
		info.Type = TypeSeries
	case "tvEpisode":
		info.Type = TypeEpisode
	}
	return nil
}

func rottentomatoesMetadata(info *Info, u *url.URL) error {
	body, err := fetch(u.String())
	if err != nil {
		return err
	}
	nodes := extractJSONLD(body)
	for _, n := range nodes {
		mergeNode(n, info)
	}
	if info.Title == "" {
		info.Title = cleanTitle(titleRe.FindStringSubmatch(string(body)))
	}
	guessFromPath(u, info)
	return nil
}

func fetch(raw string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func extractImdbID(path string) string {
	m := imdbIDRe.FindStringSubmatch(path)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractJSONLD(body []byte) []map[string]any {
	var out []map[string]any
	for _, m := range jsonldRe.FindAllStringSubmatch(string(body), -1) {
		if len(m) < 2 {
			continue
		}
		text := strings.TrimSpace(m[1])
		text = strings.TrimPrefix(text, "<!--")
		text = strings.TrimSuffix(text, "-->")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			out = append(out, x)
		case []any:
			for _, item := range x {
				if obj, ok := item.(map[string]any); ok {
					out = append(out, obj)
				}
			}
		}
	}
	return out
}

func mergeNode(n map[string]any, info *Info) {
	t, _ := n["@type"].(string)
	switch t {
	case "Movie":
		info.Type = TypeMovie
	case "TVSeries":
		info.Type = TypeSeries
	case "TVSeason":
		info.Type = TypeSeason
	case "TVEpisode":
		info.Type = TypeEpisode
	}

	if name, ok := n["name"].(string); ok && name != "" {
		info.Title = name
	}

	if d, ok := n["datePublished"].(string); ok && d != "" {
		info.Year = firstYear(d)
	}

	if info.Type == TypeEpisode {
		if ep, ok := n["episodeNumber"].(float64); ok {
			info.EpisodeNumber = int(ep)
		} else if ep, ok := n["episodeNumber"].(string); ok {
			info.EpisodeNumber, _ = strconv.Atoi(ep)
		}
		if part, ok := n["partOfSeason"].(map[string]any); ok {
			if s, ok := part["seasonNumber"].(float64); ok {
				info.SeasonNumber = int(s)
			} else if s, ok := part["seasonNumber"].(string); ok {
				info.SeasonNumber, _ = strconv.Atoi(s)
			}
		}
		if part, ok := n["partOfSeries"].(map[string]any); ok {
			info.SeriesImdbID = imdbIDFromAny(part)
		}
	}

	if info.Type == TypeSeason {
		if s, ok := n["seasonNumber"].(float64); ok {
			info.SeasonNumber = int(s)
		} else if s, ok := n["seasonNumber"].(string); ok {
			info.SeasonNumber, _ = strconv.Atoi(s)
		}
		if part, ok := n["partOfSeries"].(map[string]any); ok {
			info.SeriesImdbID = imdbIDFromAny(part)
		}
	}
}

func imdbIDFromAny(v map[string]any) string {
	if id, ok := v["identifier"].(string); ok {
		return id
	}
	if u, ok := v["url"].(string); ok {
		return extractImdbID(u)
	}
	return ""
}

func guessFromPath(u *url.URL, info *Info) {
	path := u.Path
	switch info.Source {
	case "imdb":
		if info.ImdbID != "" && info.Type == TypeUnknown {
			// leave it to the *arr lookup code
		}
	case "rottentomatoes":
		if strings.HasPrefix(path, "/m/") {
			info.Type = TypeMovie
			info.Title = slugToTitle(path[len("/m/"):])
		} else if strings.HasPrefix(path, "/tv/") {
			info.Type = TypeSeries
			info.Title = slugToTitle(path[len("/tv/"):])
		}
	}
}

func slugToTitle(s string) string {
	s = strings.Trim(s, "/")
	if i := strings.IndexAny(s, "/"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	return strings.Title(s)
}

func cleanTitle(m []string) string {
	if len(m) < 2 {
		return ""
	}
	t := strings.TrimSpace(m[1])
	if i := strings.Index(t, " - "); i > 0 {
		t = t[:i]
	}
	if i := strings.Index(t, " | "); i > 0 {
		t = t[:i]
	}
	return t
}

func firstYear(s string) int {
	m := firstYearRe.FindStringSubmatch(s)
	if len(m) > 1 {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	return 0
}
