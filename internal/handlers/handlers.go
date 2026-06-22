package handlers

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/aleksander/getarr/internal/arr"
	"github.com/aleksander/getarr/internal/config"
	"github.com/aleksander/getarr/internal/qbittorrent"
	"github.com/aleksander/getarr/internal/store"
	"github.com/go-chi/chi/v5"
)

//go:embed templates/* static/*
var webFS embed.FS

type Handlers struct {
	cfg *config.Store
	st  *store.Store
	orc *Orchestrator
	tpl *template.Template
}

type PageData struct {
	Config   config.App
	Requests []store.Request
	Result   *store.Request
	Queue    QueueData
}

type QueueData struct {
	Torrents []qbittorrent.Torrent
	Sonarr   []map[string]any
	Radarr   []map[string]any
	Errors   []string
}

func New(cfg *config.Store, st *store.Store) (*Handlers, error) {
	funcs := template.FuncMap{
		"statusClass": statusClass,
		"humanSize":   qbittorrent.HumanSize,
		"humanNumber": humanNumber,
		"eta":         qbittorrent.ETAString,
	}
	tpl, err := template.New("").Funcs(funcs).ParseFS(webFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Handlers{
		cfg: cfg,
		st:  st,
		orc: NewOrchestrator(cfg, st),
		tpl: tpl,
	}, nil
}

func (h *Handlers) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.index)
	r.Post("/config", h.postConfig)
	r.Get("/api/queue", h.queue)
	r.Post("/api/download", h.download)

	// IMDb routes
	r.Get("/title/{id}", h.imdbTitle)
	r.Get("/title/{id}/episodes", h.imdbEpisodes)

	// Rotten Tomatoes routes
	r.Get("/m/{slug}", h.rtMovie)
	r.Get("/tv/{slug}", h.rtSeries)

	staticFS, err := fs.Sub(webFS, "static")
	if err != nil {
		panic(err)
	}
	r.Get("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))).ServeHTTP)
	return r
}

func (h *Handlers) render(w http.ResponseWriter, name string, data any) {
	if err := h.tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handlers) index(w http.ResponseWriter, r *http.Request) {
	h.render(w, "base", PageData{
		Config:   h.cfg.Get(),
		Requests: h.st.List(),
		Queue:    QueueData{},
	})
}

func (h *Handlers) postConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.render(w, "toast", toast{"error", "Bad form"})
		return
	}
	cfg := configFromForm(r)
	if err := h.cfg.Set(cfg); err != nil {
		h.render(w, "toast", toast{"error", err.Error()})
		return
	}
	h.render(w, "toast", toast{"success", "Configuration saved"})
}

func (h *Handlers) queue(w http.ResponseWriter, r *http.Request) {
	q := QueueData{}
	cfg := h.cfg.Get()

	if cfg.QBittorrentURL != "" {
		c := qbittorrent.New(cfg.QBittorrentURL, cfg.QBittorrentUser, cfg.QBittorrentPass)
		if torrents, err := c.List(); err != nil {
			q.Errors = append(q.Errors, "qBittorrent: "+err.Error())
		} else {
			q.Torrents = torrents
		}
	}
	if cfg.SonarrURL != "" && cfg.SonarrKey != "" {
		s := &arr.Sonarr{BaseURL: cfg.SonarrURL, APIKey: cfg.SonarrKey}
		if records, err := s.Queue(); err != nil {
			q.Errors = append(q.Errors, "Sonarr: "+err.Error())
		} else {
			q.Sonarr = records
		}
	}
	if cfg.RadarrURL != "" && cfg.RadarrKey != "" {
		r := &arr.Radarr{BaseURL: cfg.RadarrURL, APIKey: cfg.RadarrKey}
		if records, err := r.Queue(); err != nil {
			q.Errors = append(q.Errors, "Radarr: "+err.Error())
		} else {
			q.Radarr = records
		}
	}
	h.render(w, "queue", q)
}

func (h *Handlers) download(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.render(w, "download-result", &store.Request{Status: store.StatusError, Message: "bad form"})
		return
	}
	req := h.orc.Add(r.FormValue("url"))
	h.render(w, "download-result", req)
}

func (h *Handlers) imdbTitle(w http.ResponseWriter, r *http.Request) {
	h.handleMedia(w, r, rawURL(r))
}

func (h *Handlers) imdbEpisodes(w http.ResponseWriter, r *http.Request) {
	h.handleMedia(w, r, rawURL(r))
}

func (h *Handlers) rtMovie(w http.ResponseWriter, r *http.Request) {
	h.handleMedia(w, r, rawURL(r))
}

func (h *Handlers) rtSeries(w http.ResponseWriter, r *http.Request) {
	h.handleMedia(w, r, rawURL(r))
}

func (h *Handlers) handleMedia(w http.ResponseWriter, r *http.Request, raw string) {
	req := h.orc.Add(raw)
	// HTMX requests get the result fragment; browsers get a full page.
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, "download-result", req)
		return
	}
	h.render(w, "base", PageData{
		Config:   h.cfg.Get(),
		Requests: h.st.List(),
		Result:   req,
		Queue:    QueueData{},
	})
}

func rawURL(r *http.Request) string {
	host := r.Host
	if strings.HasPrefix(host, "dl.") {
		host = host[len("dl."):]
	}
	return "https://" + host + r.URL.RequestURI()
}

func configFromForm(r *http.Request) config.App {
	return config.App{
		ListenAddr:            r.FormValue("listenAddr"),
		SonarrURL:             strings.TrimSpace(r.FormValue("sonarrUrl")),
		SonarrKey:             strings.TrimSpace(r.FormValue("sonarrKey")),
		RadarrURL:             strings.TrimSpace(r.FormValue("radarrUrl")),
		RadarrKey:             strings.TrimSpace(r.FormValue("radarrKey")),
		ProwlarrURL:           strings.TrimSpace(r.FormValue("prowlarrUrl")),
		ProwlarrKey:           strings.TrimSpace(r.FormValue("prowlarrKey")),
		OMDbKey:               strings.TrimSpace(r.FormValue("omdbKey")),
		QBittorrentURL:        strings.TrimSpace(r.FormValue("qbittorrentUrl")),
		QBittorrentUser:       strings.TrimSpace(r.FormValue("qbittorrentUser")),
		QBittorrentPass:       r.FormValue("qbittorrentPass"),
		DefaultQualityProfile: r.FormValue("defaultQualityProfile"),
		RootFolderPath:        r.FormValue("rootFolderPath"),
		SeriesMonitor:         r.FormValue("seriesMonitor"),
		LanguageProfile:       r.FormValue("languageProfile"),
		SubtitleLanguages:     r.FormValue("subtitleLanguages"),
		PreferredFormat:       r.FormValue("preferredFormat"),
		Tags:                  r.FormValue("tags"),
	}
}

type toast struct {
	Class   string
	Message string
}

func statusClass(s store.Status) string {
	switch s {
	case store.StatusAdded:
		return "added"
	case store.StatusSearching:
		return "searching"
	case store.StatusError:
		return "error"
	default:
		return "pending"
	}
}

func humanNumber(v any) string {
	switch n := v.(type) {
	case float64:
		return qbittorrent.HumanSize(int64(n))
	case int64:
		return qbittorrent.HumanSize(n)
	case int:
		return qbittorrent.HumanSize(int64(n))
	default:
		return fmt.Sprintf("%v", v)
	}
}
