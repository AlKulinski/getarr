package arr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type QualityProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func doJSON(method, base, path, key string, body, out any) error {
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	if idx := strings.Index(path, "?"); idx >= 0 {
		u.Path = path[:idx]
		u.RawQuery = path[idx+1:]
	} else {
		u.Path = path
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, u.String(), bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s -> %d: %s", method, u.String(), resp.StatusCode, string(b))
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func firstQuality(profiles []QualityProfile, name string) int {
	for _, p := range profiles {
		if p.Name == name {
			return p.ID
		}
	}
	if len(profiles) > 0 {
		return profiles[0].ID
	}
	return 0
}
