package config

import (
	"encoding/json"
	"os"
	"sync"
)

// App holds the runtime configuration for getarr.
type App struct {
	// UI
	ListenAddr string `json:"listenAddr"`

	// *arr services
	SonarrURL string `json:"sonarrUrl"`
	SonarrKey string `json:"sonarrKey"`

	RadarrURL string `json:"radarrUrl"`
	RadarrKey string `json:"radarrKey"`

	ProwlarrURL string `json:"prowlarrUrl"`
	ProwlarrKey string `json:"prowlarrKey"`

	// Download client
	QBittorrentURL  string `json:"qbittorrentUrl"`
	QBittorrentUser string `json:"qbittorrentUser"`
	QBittorrentPass string `json:"qbittorrentPass"`

	// Metadata providers
	OMDbKey string `json:"omdbKey"`

	// Defaults used when adding items
	DefaultQualityProfile string `json:"defaultQualityProfile"`
	RootFolderPath        string `json:"rootFolderPath"`
	SeriesMonitor         string `json:"seriesMonitor"`
	LanguageProfile       string `json:"languageProfile"`
	SubtitleLanguages     string `json:"subtitleLanguages"`
	PreferredFormat       string `json:"preferredFormat"`
	Tags                  string `json:"tags"`
}

func defaultApp() App {
	return App{
		ListenAddr:            ":8080",
		DefaultQualityProfile: "Any",
		SeriesMonitor:         "all",
		RootFolderPath:        "/data/media",
	}
}

type Store struct {
	path string
	mu   sync.RWMutex
	app  App
}

func New(path string) (*Store, error) {
	s := &Store{path: path, app: defaultApp()}
	if _, err := os.Stat(path); err == nil {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &s.app); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Get() App {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.app
}

func (s *Store) Set(a App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.app = a
	b, err := json.MarshalIndent(s.app, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}
