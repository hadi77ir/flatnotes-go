<p align="center">
  <img src="docs/logo.svg" width="300px"></img>
</p>
<p align="center">
  <img alt="GitHub Release" src="https://img.shields.io/github/v/release/hadi77ir/flatnotes-go?style=for-the-badge">
</p>

flatnotes-go is a self-hosted, database-less note-taking web app that utilises a flat folder of markdown files for storage.

Log into the [demo site](https://demo.flatnotes.io) and take a look around. *Note: This site resets every 15 minutes.*

## Contents

* [Design Principle](#design-principle)
* [Features](#features)
* [Getting Started](#getting-started)
  * [Hosted](#hosted)
  * [Self Hosted](#self-hosted)
* [Roadmap](#roadmap)
* [Contributing](#contributing)
* [Sponsorship](#sponsorship)
* [Thanks](#thanks)

## Design Principle

flatnotes is designed to be a distraction-free note-taking app that puts your note content first. This means:

* A clean and simple user interface.
* No folders, notebooks or anything like that. Just all of your notes, backed by powerful search and tagging functionality.
* Quick access to a full-text search from anywhere in the app (keyboard shortcut "/").

Another key design principle is not to take your notes hostage. Your notes are just markdown files. There's no database, proprietary formatting, complicated folder structures or anything like that. You're free at any point to just move the files elsewhere and use another app.

Equally, the only thing flatnotes caches is the search index and that's incrementally synced on every search (and when flatnotes first starts). This means that you're free to add, edit & delete the markdown files outside of flatnotes even whilst flatnotes is running.

## Features

* Mobile responsive web interface.
* Raw/WYSIWYG markdown editor modes.
* Advanced search functionality.
* Note "tagging" functionality.
* Customisable home page.
* Wikilink support to easily link to other notes (`[[My Other Note]]`).
* Light/dark themes.
* Multiple authentication options (none, read-only, username/password, 2FA).
* Restful API.

See [the wiki](https://github.com/dullage/flatnotes/wiki) for more details.

## Getting Started

### Hosted

A quick and easy way to get started with flatnotes is to host it on PikaPods. Just click the button below and follow the instructions.

[![PikaPods](https://www.pikapods.com/static/run-button-34.svg)](https://www.pikapods.com/pods?run=flatnotes)


### Self Hosted

If you'd prefer to host flatnotes-go yourself then the recommendation is to use Docker.

To build from source, install the frontend dependencies and run:

```shell
npm ci
make build
```

The build embeds the generated frontend into the Go binary.

Useful build targets:

```shell
make test          # Run Go tests
make vet           # Run go vet
make npm-audit     # Run frontend dependency audit
make govulncheck   # Run Go vulnerability analysis
make security      # Run tests, vet, npm audit, and govulncheck
make docker        # Build the standard Docker image
make docker-rootless # Build the rootless Docker image
make snapshot      # Build a local GoReleaser snapshot
```

### Example Docker Run Command

```shell
docker run -d \
  -e "PUID=1000" \
  -e "PGID=1000" \
  -e "FLATNOTES_AUTH_TYPE=password" \
  -e "FLATNOTES_USERNAME=user" \
  -e 'FLATNOTES_PASSWORD=changeMe!' \
  -e "FLATNOTES_SECRET_KEY=aLongRandomSeriesOfCharactersAtLeast32Long" \
  -v "$(pwd)/data:/data" \
  -p "8080:8080" \
  ghcr.io/hadi77ir/flatnotes-go:latest
```

### Example Docker Compose
```yaml
version: "3"

services:
  flatnotes-go:
    container_name: flatnotes-go
    image: ghcr.io/hadi77ir/flatnotes-go:latest
    environment:
      PUID: 1000
      PGID: 1000
      FLATNOTES_AUTH_TYPE: "password"
      FLATNOTES_USERNAME: "user"
      FLATNOTES_PASSWORD: "changeMe!"
      FLATNOTES_SECRET_KEY: "aLongRandomSeriesOfCharactersAtLeast32Long"
    volumes:
      - "./data:/data"
      # Optional. Allows you to save the search index in a different location: 
      # - "./index:/data/.flatnotes"
    ports:
      - "8080:8080"
    restart: unless-stopped
```

### Rootless Docker

Both published images are scratch-based single-binary images. The standard image starts as root only long enough for `flatnotes-go` to fix `/data` ownership, then drops to `PUID:PGID`. A separate rootless image is also available and never starts as root:

```shell
docker run -d \
  -e "FLATNOTES_AUTH_TYPE=password" \
  -e "FLATNOTES_USERNAME=user" \
  -e 'FLATNOTES_PASSWORD=changeMe!' \
  -e "FLATNOTES_SECRET_KEY=aLongRandomSeriesOfCharactersAtLeast32Long" \
  -v "$(pwd)/data:/data" \
  -p "8080:8080" \
  ghcr.io/hadi77ir/flatnotes-go:latest-rootless
```

When using the rootless image, make sure the mounted data directory is writable by UID/GID `1000:1000`.

### Server Configuration

Every server setting is available as both an environment variable and a CLI flag. The Docker image starts `flatnotes-go` directly, so these can also be passed as command arguments.

| Environment variable | CLI flag | Default |
| --- | --- | --- |
| `FLATNOTES_HOST` | `--host` | `0.0.0.0` |
| `FLATNOTES_PORT` | `--port` | `8080` |
| `FLATNOTES_PATH` | `--path` | `/data` |
| `FLATNOTES_PATH_PREFIX` | `--path-prefix` | |
| `FLATNOTES_AUTH_TYPE` | `--auth-type` | `password` |
| `FLATNOTES_USERNAME` | `--username` | |
| `FLATNOTES_PASSWORD` | `--password` | |
| `FLATNOTES_SECRET_KEY` | `--secret-key` | |
| `FLATNOTES_SESSION_EXPIRY_DAYS` | `--session-expiry-days` | `30` |
| `FLATNOTES_TOTP_KEY` | `--totp-key` | |
| `FLATNOTES_QUICK_ACCESS_HIDE` | `--quick-access-hide` | `false` |
| `FLATNOTES_QUICK_ACCESS_TITLE` | `--quick-access-title` | `RECENTLY MODIFIED` |
| `FLATNOTES_QUICK_ACCESS_TERM` | `--quick-access-term` | `*` |
| `FLATNOTES_QUICK_ACCESS_SORT` | `--quick-access-sort` | `lastModified` |
| `FLATNOTES_QUICK_ACCESS_LIMIT` | `--quick-access-limit` | `4` |
| `FLATNOTES_TLS_CERT_FILE` | `--tls-cert-file` | |
| `FLATNOTES_TLS_KEY_FILE` | `--tls-key-file` | |
| `FLATNOTES_LOG_LEVEL` | `--log-level` | `info` |
| `FLATNOTES_CLIENT_DIST` | `--client-dist` | embedded frontend |

The frontend is embedded in the Go binary by default. Set `FLATNOTES_CLIENT_DIST` or pass `--client-dist` to serve an alternative static build directory.
The legacy `FLATNOTES_HIDE_RECENTLY_MODIFIED` variable is still accepted as an alias for `FLATNOTES_QUICK_ACCESS_HIDE`; the matching CLI alias is `--hide-recently-modified`.

### Releases

CI runs tests and builds the server on every commit. Tags matching `v*` trigger GoReleaser, which builds release archives and publishes standard and rootless container images to GitHub Container Registry. Release archives contain the `flatnotes-go` binary.

Published images:

```text
ghcr.io/hadi77ir/flatnotes-go:latest
ghcr.io/hadi77ir/flatnotes-go:latest-rootless
```

## Roadmap

I want to keep flatnotes as simple and distraction-free as possible which means limiting new features. This said, I welcome feedback and suggestions.

## Contributing

If you're interested in contributing to flatnotes, then please read the [CONTRIBUTING.md](CONTRIBUTING.md) file.

## Sponsorship

If you find this project useful, please consider buying me a beer. It would genuinely make my day.

[![Sponsor](https://img.shields.io/static/v1?label=Sponsor&message=%E2%9D%A4&logo=GitHub&color=%23fe8e86)](https://github.com/sponsors/Dullage)

## Thanks

A special thanks to 2 fantastic open-source projects that make flatnotes possible.

* [Bleve](https://blevesearch.com/) - A full-text search and indexing library for Go.
* [TOAST UI Editor](https://ui.toast.com/tui-editor) - A GFM Markdown and WYSIWYG editor for the browser.
