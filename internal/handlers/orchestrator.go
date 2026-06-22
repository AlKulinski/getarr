package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aleksander/getarr/internal/arr"
	"github.com/aleksander/getarr/internal/config"
	"github.com/aleksander/getarr/internal/metadata"
	"github.com/aleksander/getarr/internal/qbittorrent"
	"github.com/aleksander/getarr/internal/store"
)

// Orchestrator ties the metadata parsing, *arr APIs and qBittorrent together.
type Orchestrator struct {
	cfg   *config.Store
	st    *store.Store
	qbit  *qbittorrent.Client
}

func NewOrchestrator(cfg *config.Store, st *store.Store) *Orchestrator {
	return &Orchestrator{cfg: cfg, st: st}
}

func (o *Orchestrator) qbitClient() *qbittorrent.Client {
	cfg := o.cfg.Get()
	if cfg.QBittorrentURL == "" {
		return nil
	}
	return qbittorrent.New(cfg.QBittorrentURL, cfg.QBittorrentUser, cfg.QBittorrentPass)
}

func (o *Orchestrator) Add(rawURL string) *store.Request {
	id := randomID()
	req := &store.Request{
		ID:        id,
		URL:       rawURL,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	o.st.Add(*req)

	cfg := o.cfg.Get()
	info, err := metadata.FromURL(rawURL, metadata.Options{OMDbKey: cfg.OMDbKey})
	if err != nil {
		o.setError(req, "metadata: "+err.Error())
		return req
	}
	req.Title = info.Title
	req.Type = string(info.Type)
	o.st.Update(req.ID, req.Status, req.Message)

	if err := o.ensureConfigured(cfg, info.Type); err != nil {
		o.setError(req, err.Error())
		return req
	}

	var addErr error
	switch info.Type {
	case metadata.TypeMovie:
		addErr = o.addMovie(cfg, info, req)
	case metadata.TypeSeries:
		addErr = o.addSeries(cfg, info, req)
	case metadata.TypeSeason:
		addErr = o.addSeason(cfg, info, req)
	case metadata.TypeEpisode:
		addErr = o.addEpisode(cfg, info, req)
	default:
		addErr = o.guessAndAdd(cfg, info, req)
	}

	if addErr != nil {
		o.setError(req, addErr.Error())
	}
	return req
}

func (o *Orchestrator) setError(req *store.Request, msg string) {
	req.Status = store.StatusError
	req.Message = msg
	req.UpdatedAt = time.Now()
	o.st.Update(req.ID, req.Status, req.Message)
}

func (o *Orchestrator) setStatus(req *store.Request, status store.Status, msg string) {
	req.Status = status
	req.Message = msg
	req.UpdatedAt = time.Now()
	o.st.Update(req.ID, req.Status, req.Message)
}

func (o *Orchestrator) ensureConfigured(cfg config.App, t metadata.Type) error {
	switch t {
	case metadata.TypeMovie:
		if cfg.RadarrURL == "" || cfg.RadarrKey == "" {
			if cfg.ProwlarrURL == "" || cfg.QBittorrentURL == "" {
				return fmt.Errorf("movies require either Radarr or Prowlarr+qBittorrent")
			}
		}
	case metadata.TypeSeries, metadata.TypeSeason, metadata.TypeEpisode:
		if cfg.SonarrURL == "" || cfg.SonarrKey == "" {
			if cfg.ProwlarrURL == "" || cfg.QBittorrentURL == "" {
				return fmt.Errorf("tv requires either Sonarr or Prowlarr+qBittorrent")
			}
		}
	default:
		if (cfg.RadarrURL == "" || cfg.RadarrKey == "") && (cfg.SonarrURL == "" || cfg.SonarrKey == "") {
			if cfg.ProwlarrURL == "" || cfg.QBittorrentURL == "" {
				return fmt.Errorf("no configured *arr or Prowlarr+qBittorrent")
			}
		}
	}
	return nil
}

func (o *Orchestrator) addMovie(cfg config.App, info *metadata.Info, req *store.Request) error {
	if cfg.RadarrURL != "" && cfg.RadarrKey != "" {
		r := &arr.Radarr{BaseURL: cfg.RadarrURL, APIKey: cfg.RadarrKey}
		profiles, err := r.QualityProfiles()
		if err != nil {
			return fmt.Errorf("radarr profiles: %w", err)
		}
		movie, err := r.Lookup(info.ImdbID, info.Title, info.Year)
		if err != nil {
			return fmt.Errorf("radarr lookup: %w", err)
		}
		pid := arrFirst(profiles, cfg.DefaultQualityProfile)
		if err := r.Add(movie, pid, cfg.RootFolderPath, true, nil); err != nil {
			return fmt.Errorf("radarr add: %w", err)
		}
		o.setStatus(req, store.StatusAdded, "Added movie to Radarr")
		return nil
	}
	return o.addViaProwlarr(cfg, info, req, []int{2000})
}

func (o *Orchestrator) addSeries(cfg config.App, info *metadata.Info, req *store.Request) error {
	if cfg.SonarrURL != "" && cfg.SonarrKey != "" {
		s := &arr.Sonarr{BaseURL: cfg.SonarrURL, APIKey: cfg.SonarrKey}
		profiles, err := s.QualityProfiles()
		if err != nil {
			return fmt.Errorf("sonarr profiles: %w", err)
		}
		series, err := s.Lookup(info.ImdbID, info.Title, info.Year)
		if err != nil {
			return fmt.Errorf("sonarr lookup: %w", err)
		}
		pid := sonarrFirst(profiles, cfg.DefaultQualityProfile)
		monitor := cfg.SeriesMonitor
		if monitor == "" {
			monitor = "all"
		}
		seriesID, err := s.Add(series, pid, cfg.RootFolderPath, monitor, nil)
		if err != nil {
			return fmt.Errorf("sonarr add: %w", err)
		}
		if err := s.Command("SeriesSearch", map[string]any{"seriesId": seriesID}); err != nil {
			return fmt.Errorf("sonarr search: %w", err)
		}
		o.setStatus(req, store.StatusSearching, "Added series to Sonarr and started search")
		return nil
	}
	return o.addViaProwlarr(cfg, info, req, []int{5000})
}

func (o *Orchestrator) addSeason(cfg config.App, info *metadata.Info, req *store.Request) error {
	if info.SeasonNumber == 0 {
		return o.addSeries(cfg, info, req)
	}
	if cfg.SonarrURL != "" && cfg.SonarrKey != "" {
		s := &arr.Sonarr{BaseURL: cfg.SonarrURL, APIKey: cfg.SonarrKey}
		profiles, err := s.QualityProfiles()
		if err != nil {
			return fmt.Errorf("sonarr profiles: %w", err)
		}
		series, err := s.Lookup(info.SeriesImdbID, info.Title, info.Year)
		if err != nil {
			return fmt.Errorf("sonarr lookup: %w", err)
		}
		pid := sonarrFirst(profiles, cfg.DefaultQualityProfile)
		seriesID, err := s.Add(series, pid, cfg.RootFolderPath, "none", nil)
		if err != nil {
			return fmt.Errorf("sonarr add: %w", err)
		}
		ids, err := episodeIDs(s, seriesID, info.SeasonNumber, 0)
		if err != nil {
			return fmt.Errorf("sonarr episodes: %w", err)
		}
		if len(ids) == 0 {
			return fmt.Errorf("no episodes found for season %d", info.SeasonNumber)
		}
		if err := s.MonitorEpisodes(ids); err != nil {
			return fmt.Errorf("sonarr monitor: %w", err)
		}
		if err := s.Command("SeasonSearch", map[string]any{"seriesId": seriesID, "seasonNumber": info.SeasonNumber}); err != nil {
			return fmt.Errorf("sonarr search: %w", err)
		}
		o.setStatus(req, store.StatusSearching, fmt.Sprintf("Added season %d to Sonarr and started search", info.SeasonNumber))
		return nil
	}
	return o.addViaProwlarr(cfg, info, req, []int{5000})
}

func (o *Orchestrator) addEpisode(cfg config.App, info *metadata.Info, req *store.Request) error {
	if info.SeasonNumber == 0 || info.EpisodeNumber == 0 {
		return o.addSeries(cfg, info, req)
	}
	if cfg.SonarrURL != "" && cfg.SonarrKey != "" {
		s := &arr.Sonarr{BaseURL: cfg.SonarrURL, APIKey: cfg.SonarrKey}
		profiles, err := s.QualityProfiles()
		if err != nil {
			return fmt.Errorf("sonarr profiles: %w", err)
		}
		series, err := s.Lookup(info.SeriesImdbID, info.Title, info.Year)
		if err != nil {
			return fmt.Errorf("sonarr lookup: %w", err)
		}
		pid := sonarrFirst(profiles, cfg.DefaultQualityProfile)
		seriesID, err := s.Add(series, pid, cfg.RootFolderPath, "none", nil)
		if err != nil {
			return fmt.Errorf("sonarr add: %w", err)
		}
		ids, err := episodeIDs(s, seriesID, info.SeasonNumber, info.EpisodeNumber)
		if err != nil {
			return fmt.Errorf("sonarr episodes: %w", err)
		}
		if len(ids) == 0 {
			return fmt.Errorf("no episode found for S%02dE%02d", info.SeasonNumber, info.EpisodeNumber)
		}
		if err := s.MonitorEpisodes(ids); err != nil {
			return fmt.Errorf("sonarr monitor: %w", err)
		}
		if err := s.Command("EpisodeSearch", map[string]any{"episodeIds": ids}); err != nil {
			return fmt.Errorf("sonarr search: %w", err)
		}
		o.setStatus(req, store.StatusSearching, fmt.Sprintf("Added S%02dE%02d to Sonarr and started search", info.SeasonNumber, info.EpisodeNumber))
		return nil
	}
	return o.addViaProwlarr(cfg, info, req, []int{5000})
}

func (o *Orchestrator) guessAndAdd(cfg config.App, info *metadata.Info, req *store.Request) error {
	if info.ImdbID == "" {
		return fmt.Errorf("cannot determine content type for %q", info.Title)
	}
	if cfg.RadarrURL != "" && cfg.RadarrKey != "" {
		r := &arr.Radarr{BaseURL: cfg.RadarrURL, APIKey: cfg.RadarrKey}
		if _, err := r.Lookup(info.ImdbID, "", 0); err == nil {
			return o.addMovie(cfg, info, req)
		}
	}
	if cfg.SonarrURL != "" && cfg.SonarrKey != "" {
		s := &arr.Sonarr{BaseURL: cfg.SonarrURL, APIKey: cfg.SonarrKey}
		if _, err := s.Lookup(info.ImdbID, "", 0); err == nil {
			info.Type = metadata.TypeSeries
			return o.addSeries(cfg, info, req)
		}
	}
	return fmt.Errorf("could not identify %s as a movie or series", info.ImdbID)
}

func (o *Orchestrator) addViaProwlarr(cfg config.App, info *metadata.Info, req *store.Request, categories []int) error {
	if cfg.ProwlarrURL == "" || cfg.QBittorrentURL == "" {
		return fmt.Errorf("no *arr configured and Prowlarr/qBittorrent not set")
	}
	p := &arr.Prowlarr{BaseURL: cfg.ProwlarrURL, APIKey: cfg.ProwlarrKey}
	query := info.ImdbID
	if query == "" {
		if info.Year > 0 {
			query = fmt.Sprintf("%s %d", info.Title, info.Year)
		} else {
			query = info.Title
		}
	}
	releases, err := p.Search(query, categories)
	if err != nil {
		return fmt.Errorf("prowlarr search: %w", err)
	}
	var best arr.Release
	for _, r := range releases {
		if r.MagnetURL == "" && r.DownloadURL == "" {
			continue
		}
		if best.Title == "" || r.Seeders > best.Seeders {
			best = r
		}
	}
	if best.Title == "" {
		return fmt.Errorf("no release found")
	}
	link := best.MagnetURL
	if link == "" {
		link = best.DownloadURL
	}
	q := o.qbitClient()
	if q == nil {
		return fmt.Errorf("qBittorrent not configured")
	}
	if err := q.AddURL(link); err != nil {
		return fmt.Errorf("qbittorrent add: %w", err)
	}
	o.setStatus(req, store.StatusAdded, fmt.Sprintf("Sent release to qBittorrent: %s", best.Title))
	return nil
}

func episodeIDs(s *arr.Sonarr, seriesID, season, episode int) ([]int, error) {
	eps, err := s.GetEpisodes(seriesID)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, e := range eps {
		sn, _ := e["seasonNumber"].(float64)
		en, _ := e["episodeNumber"].(float64)
		eid, _ := e["id"].(float64)
		if int(sn) == season {
			if episode == 0 || int(en) == episode {
				ids = append(ids, int(eid))
			}
		}
	}
	return ids, nil
}

func arrFirst(profiles []arr.QualityProfile, name string) int {
	for _, p := range profiles {
		if strings.EqualFold(p.Name, name) {
			return p.ID
		}
	}
	if len(profiles) > 0 {
		return profiles[0].ID
	}
	return 0
}

func sonarrFirst(profiles []arr.QualityProfile, name string) int {
	return arrFirst(profiles, name)
}

func randomID() string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
