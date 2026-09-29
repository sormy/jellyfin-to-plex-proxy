# jellyfin-to-plex-proxy

Serves a Plex library over the Jellyfin API, so Jellyfin apps — Swiftfin and Infuse on Apple TV,
iPhone and iPad — browse and play it. Plex stays the only database: watch state, resume points and
new media are Plex's, with nothing to sync.

One small static binary. It listens where Jellyfin would (`8096`, discovery on UDP `7359`) and
talks to Plex over its HTTP API.

## Download

From [Releases](https://github.com/sormy/jellyfin-to-plex-proxy/releases/latest):

| Platform     | File                                       | Oldest OS     |
| ------------ | ------------------------------------------ | ------------- |
| Linux x86-64 | `jellyfin-to-plex-proxy-linux-amd64`       | kernel 2.6.32 |
| Linux ARM64  | `jellyfin-to-plex-proxy-linux-arm64`       | kernel 2.6.32 |
| macOS Intel  | `jellyfin-to-plex-proxy-macos-amd64`       | 10.15         |
| macOS Apple  | `jellyfin-to-plex-proxy-macos-arm64`       | 11            |
| Windows x64  | `jellyfin-to-plex-proxy-windows-amd64.exe` | 10            |

## Configure

| Variable            | Default                  | Meaning                     |
| ------------------- | ------------------------ | --------------------------- |
| `PLEX_TOKEN`        | required                 | the Plex owner's token      |
| `JELLYFIN_USERNAME` | `jellyfin`               | user name apps sign in with |
| `JELLYFIN_PASSWORD` | `jellyfin`               | password apps sign in with  |
| `PLEX_URL`          | `http://127.0.0.1:32400` | Plex Media Server           |
| `LISTEN`            | `:8096`                  | Jellyfin API address        |

## Plex token

The proxy acts as the Plex server's owner, so it needs the owner's token. Any one of:

- **Plex Web**, signed in as the owner: open any item, `⋯` → **Get Info** → **View XML**; the
  token is the `X-Plex-Token=` value at the end of the page's address.
- **On the Plex host**, where Plex keeps it as `PlexOnlineToken`:

| Plex host | Where                                                                                    |
| --------- | ---------------------------------------------------------------------------------------- |
| Linux     | `/var/lib/plexmediaserver/Library/Application Support/Plex Media Server/Preferences.xml` |
| Docker    | `/config/Library/Application Support/Plex Media Server/Preferences.xml`                  |
| macOS     | `defaults read com.plexapp.plexmediaserver PlexOnlineToken`                              |
| Windows   | `reg query "HKCU\Software\Plex, Inc.\Plex Media Server" /v PlexOnlineToken`              |

The token grants full control of the Plex account: keep it out of shell history and shared files.

## Install

### Linux, as a service

The unit is `jellyfin-to-plex-proxy.service` in this repository, and in each release. On the Plex
host, as root:

```sh
release=https://github.com/sormy/jellyfin-to-plex-proxy/releases/latest/download
arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSLo /usr/local/bin/jellyfin-to-plex-proxy "$release/jellyfin-to-plex-proxy-linux-$arch"
chmod 755 /usr/local/bin/jellyfin-to-plex-proxy
curl -fsSLo /etc/systemd/system/jellyfin-to-plex-proxy.service "$release/jellyfin-to-plex-proxy.service"
```

Write the env file — this copies the token without printing it — and start:

```sh
prefs='/var/lib/plexmediaserver/Library/Application Support/Plex Media Server/Preferences.xml'
sed -n 's/.*PlexOnlineToken="\([^"]*\)".*/PLEX_TOKEN=\1/p' "$prefs" | install -m 600 /dev/stdin /etc/jellyfin-to-plex-proxy.env
systemctl enable --now jellyfin-to-plex-proxy
```

`journalctl -u jellyfin-to-plex-proxy -f` logs every request with the app's name and the status.

### macOS

```sh
chmod +x jellyfin-to-plex-proxy-macos-*
xattr -d com.apple.quarantine jellyfin-to-plex-proxy-macos-*
PLEX_TOKEN=<token> ./jellyfin-to-plex-proxy-macos-arm64
```

### Windows

In PowerShell:

```powershell
$env:PLEX_TOKEN = "<token>"
.\jellyfin-to-plex-proxy-windows-amd64.exe
```

Allow it through the firewall when Windows asks, or apps cannot reach it.

## Connect

Add the server — found on the LAN, or at `http://<host>:8096` — and sign in: `jellyfin` /
`jellyfin` unless set otherwise. Sign-in survives restarts; changing the user name or password signs
every app out.

| App      | Add it as        | Setting                                     |
| -------- | ---------------- | ------------------------------------------- |
| Swiftfin | a server         | —                                           |
| Infuse   | a Jellyfin share | Library Mode off (Direct Mode), the default |

Tested with:

- Swiftfin on Apple TV
- Swiftfin on macOS
- Infuse on Apple TV, Direct Mode

Other Jellyfin apps may call endpoints the proxy lacks: they log as `404` in the journal.

## Build

```sh
./build.sh
```

Vets, tests, and writes every release binary plus `SHA256SUMS` to `dist/`. It builds with Go
1.22 — the newest release that still targets macOS 10.15 and Windows 10 — which the `go` command
downloads on first run. Go 1.22 no longer receives security fixes.

## Test

```sh
CGO_ENABLED=0 go test ./...
```

Runs the scenarios against an in-memory fake Plex. With `JELLYFIN_URL=http://<host>:8096` set, and
`JELLYFIN_USERNAME` and `JELLYFIN_PASSWORD` if not the defaults, the same scenarios run against a
live proxy and its Plex.

> The live run changes watch state on one series, `testSeries` in `api_test.go`, and restores its
> watched flags and resume points. Play counts, last-viewed dates and Plex's history do not come
> back: pick a series whose history you do not care about.

## How it maps

| Jellyfin                            | Plex                                       |
| ----------------------------------- | ------------------------------------------ |
| libraries                           | movie and show sections                    |
| `Movie` `Series` `Season` `Episode` | `movie` `show` `season` `episode`          |
| resume, next up                     | on deck                                    |
| played, unplayed                    | `/:/scrobble`, `/:/unscrobble`             |
| playback progress                   | `/:/progress`                              |
| stream, image, subtitle             | the part, `/photo/:/transcode`, the stream |

A stopped position is read as Jellyfin reads it:

| Stopped at                          | Result          |
| ----------------------------------- | --------------- |
| the first minute, or under 5%       | no resume point |
| over 90%, or the end                | watched         |
| anywhere else, item under 5 minutes | watched         |
| anywhere else                       | resume point    |

The Plex token never leaves the proxy: streams, images and subtitles are proxied, not redirected. A
stream URL is valid only with the play session the proxy signed for that item.

## Limits

- One user, the Plex owner. Plex managed users are not exposed.
- Anyone on the network who knows the user name and password gets the owner's library: change the
  defaults where the network is not yours alone.
- Direct play only, no transcoding: the app must decode the file. Swiftfin's default player does.
- Movie and show libraries only: no music, photos or Live TV.
- Favorites are acknowledged, not stored — Plex has none.
- Genre, tag and language filters and people are empty.

## License

MIT — see `LICENSE`.
