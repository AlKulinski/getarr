package qbittorrent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client wraps the qBittorrent Web API.
type Client struct {
	BaseURL string
	User    string
	Pass    string

	mu     sync.Mutex
	client *http.Client
	cookie string
}

// Torrent is the subset of fields we display in the UI.
type Torrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	Progress     float64 `json:"progress"`
	State        string  `json:"state"`
	Size         int64   `json:"size"`
	Downloaded   int64   `json:"completed"`
	DownloadSpeed int64  `json:"dlspeed"`
	UploadSpeed  int64   `json:"upspeed"`
	ETA          int64   `json:"eta"`
}

func New(base, user, pass string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		BaseURL: strings.TrimSuffix(base, "/"),
		User:    user,
		Pass:    pass,
		client:  &http.Client{Jar: jar, Timeout: 30 * time.Second},
	}
}

func (c *Client) login() error {
	if c.User == "" && c.Pass == "" {
		return nil
	}
	data := url.Values{}
	data.Set("username", c.User)
	data.Set("password", c.Pass)
	resp, err := c.client.PostForm(c.BaseURL+"/api/v2/auth/login", data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qbittorrent login failed: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Ok." {
		return fmt.Errorf("qbittorrent login failed: %s", string(body))
	}
	return nil
}

func (c *Client) do(method, path string, body io.Reader, contentType string) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Referer", c.BaseURL)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden && c.User != "" {
		// Try re-authenticating once.
		resp.Body.Close()
		if err := c.login(); err != nil {
			return nil, err
		}
		req, _ = http.NewRequest(method, c.BaseURL+path, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("Referer", c.BaseURL)
		resp, err = c.client.Do(req)
	}
	return resp, err
}

// AddURL sends a magnet/torrent URL to qBittorrent.
func (c *Client) AddURL(link string) error {
	data := url.Values{}
	data.Set("urls", link)
	resp, err := c.do("POST", "/api/v2/torrents/add", strings.NewReader(data.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qbittorrent add failed: %d %s", resp.StatusCode, string(b))
	}
	return nil
}

// List returns the currently active torrents.
func (c *Client) List() ([]Torrent, error) {
	resp, err := c.do("GET", "/api/v2/torrents/info", nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qbittorrent list failed: %d %s", resp.StatusCode, string(b))
	}
	var out []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Progress returns a 0..100 formatted string.
func (t Torrent) ProgressPct() string {
	return strconv.FormatFloat(t.Progress*100, 'f', 1, 64) + "%"
}

// HumanSize formats bytes as KB/MB/GB.
func HumanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ETAString returns a human readable ETA.
func ETAString(seconds int64) string {
	if seconds <= 0 {
		return "∞"
	}
	d := time.Duration(seconds) * time.Second
	return d.Round(time.Second).String()
}
