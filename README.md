# getarr

A tiny Go + HTMX app that lives on your internal network and lets you start
movie/TV downloads by prefixing an IMDb or Rotten Tomatoes URL with `dl.`.

It talks to your existing *arr stack (Sonarr, Radarr, Prowlarr) and
qBittorrent, and shows a live progress indicator for everything that is
currently downloading.

## What it does

- `dl.imdb.com/title/tt0137523/` → adds **Fight Club** to Radarr (movie)
- `dl.imdb.com/title/tt0944947/` → adds **Game of Thrones** to Sonarr (whole series)
- `dl.imdb.com/title/tt0944947/episodes?season=1` → downloads season 1
- `dl.rottentomatoes.com/m/fight_club` → adds the movie
- `dl.rottentomatoes.com/tv/game_of_thrones` → adds the series

The UI also has a manual URL form and configuration page.

## Requirements

- Go 1.22+ (to build locally) or Docker
- Sonarr (TV)
- Radarr (movies) – or use Prowlarr + qBittorrent as a fallback
- Prowlarr (optional, used as a fallback when Radarr/Sonarr are not set)
- qBittorrent (optional, provides the live progress indicator)

## Quick start

### Run from source

```bash
go build -o getarr ./cmd/getarr
./getarr -data ./data
```

Then open `http://<server-ip>:8080` and fill in the configuration.

### Run with Docker Compose

```bash
docker compose up -d --build
```

The compose file exposes the app on port `8080` and mounts `./data` for the
config file.

## Proxmox LXC container

1. Create a new Debian/Ubuntu LXC container in Proxmox (1 CPU, 512 MB RAM and
   a few GB disk are plenty).
2. Inside the container install Docker or run the Go binary directly:

   ```bash
   apt update && apt install -y git golang ca-certificates
   git clone <this-repo> /opt/getarr
   cd /opt/getarr
   go build -o getarr ./cmd/getarr
   ```

3. Make it start on boot, e.g. with a systemd service:

   ```ini
   # /etc/systemd/system/getarr.service
   [Unit]
   Description=getarr
   After=network.target

   [Service]
   Type=simple
   WorkingDirectory=/opt/getarr
   ExecStart=/opt/getarr/getarr -data /opt/getarr/data
   Restart=on-failure

   [Install]
   WantedBy=multi-user.target
   ```

   ```bash
   systemctl daemon-reload
   systemctl enable --now getarr
   ```

## DNS / how the `dl.` prefix works

`getarr` itself only needs an HTTP request to arrive at its address. The
`dl.` magic is done with a local DNS record. The easiest way is a Pi-hole,
AdGuard Home or router DNS rewrite:

```
dl.imdb.com        → <getarr-ip>
dl.www.imdb.com    → <getarr-ip>
dl.rottentomatoes.com → <getarr-ip>
```

When the request arrives, `getarr` strips the leading `dl.`, fetches the
original IMDb/RT page metadata and starts the download. If you prefer, just
paste the URL into the manual form instead of using DNS.

## Configuration

All settings are saved to `data/config.json` through the web UI.

| Setting | Description |
|---|---|
| Sonarr URL / key | For TV shows |
| Radarr URL / key | For movies |
| Prowlarr URL / key | Fallback direct indexer search |
| qBittorrent URL / user / pass | Live progress + direct fallback downloads |
| OMDb API key | Optional. Makes IMDb **episode** pages (SxxExx) work perfectly. Without it, episode pages are added as the whole series. |
| Quality profile | Name of the Sonarr/Radarr quality profile to use, e.g. `Any`, `HD-1080p` |
| Root folder path | Where Sonarr/Radarr store media, e.g. `/data/media` |
| Series monitor mode | Which episodes to monitor when adding a whole series |
| Subtitle languages / format / tags | Stored as defaults / notes for your own *arr custom formats |

### Quality, subtitles and format

`getarr` does not parse video files. It passes your quality profile name to
Sonarr/Radarr. Subtitle handling, formats and tags are kept in the config so
you can apply them through your existing Sonarr/Radarr custom formats, release
profiles and tag IDs.

## Live progress

The dashboard polls `/api/queue` every 3 seconds with HTMX and renders:

- qBittorrent torrent progress bars
- Sonarr queue status
- Radarr queue status

Errors from a service that is offline or misconfigured are shown inline so the
page keeps working.

## URL routing handled by the app

- `/title/{id}` and `/title/{id}/episodes` – IMDb
- `/m/{slug}` and `/tv/{slug}` – Rotten Tomatoes
- `/api/queue` – HTMX queue fragment
- `/api/download` – manual download form
- `/config` – save settings

## Development

```bash
go test ./...
go run ./cmd/getarr -data ./data
```

## License

MIT
