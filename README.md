# jellyfin-to-plex-proxy

Serves a Plex library over the Jellyfin API, so Jellyfin apps — Swiftfin and Infuse for video,
Finamp for music — browse and play it. Plex stays the only database: watch state, resume points and
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

| Variable             | Default                  | Meaning                                                 |
| -------------------- | ------------------------ | ------------------------------------------------------- |
| `PLEX_TOKEN`         | required                 | the Plex owner's token                                  |
| `JELLYFIN_USERNAME`  | `jellyfin`               | user name apps sign in with                             |
| `JELLYFIN_PASSWORD`  | `jellyfin`               | password apps sign in with                              |
| `PLEX_URL`           | `http://127.0.0.1:32400` | Plex Media Server                                       |
| `LISTEN`             | `:8096`                  | Jellyfin API address                                    |
| `TLS_CERT`           | —                        | certificate chain; with `TLS_KEY`, serve HTTPS too      |
| `TLS_KEY`            | —                        | its private key                                         |
| `LISTEN_TLS`         | `:8920`                  | HTTPS address                                           |
| `LOGIN_MAX_FAILURES` | `5`                      | wrong passwords before a user name locks; `0` for never |
| `LOGIN_LOCKOUT`      | `15m`                    | how long it stays locked                                |

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

### HTTPS

Apple's apps trust only a certificate from a public authority, such as Let's Encrypt, for a name
that resolves to the host: a self-signed one is refused. With one, hand it to the service through
systemd, which reads the key as root — on the Plex host, as root:

```sh
mkdir -p /etc/systemd/system/jellyfin-to-plex-proxy.service.d
cat >/etc/systemd/system/jellyfin-to-plex-proxy.service.d/tls.conf <<'UNIT'
[Service]
LoadCredential=tls.crt:/etc/letsencrypt/live/<name>/fullchain.pem
LoadCredential=tls.key:/etc/letsencrypt/live/<name>/privkey.pem
Environment=TLS_CERT=%d/tls.crt TLS_KEY=%d/tls.key
UNIT
systemctl daemon-reload && systemctl restart jellyfin-to-plex-proxy
```

Apps then connect to `https://<name>:8920`; plain HTTP stays on `8096`. systemd copies the
certificate at start: restart the service after a renewal.

### macOS

On the machine running the proxy:

```sh
chmod +x jellyfin-to-plex-proxy-macos-*
xattr -d com.apple.quarantine jellyfin-to-plex-proxy-macos-*
PLEX_TOKEN=<token> ./jellyfin-to-plex-proxy-macos-arm64
```

### Windows

In PowerShell, on the machine running the proxy:

```powershell
$env:PLEX_TOKEN = "<token>"
.\jellyfin-to-plex-proxy-windows-amd64.exe
```

Allow it through the firewall when Windows asks, or apps cannot reach it.

## Connect

Add the server — found on the LAN, or at `http://<host>:8096` — and sign in: `jellyfin` /
`jellyfin` unless set otherwise. Sign-in survives restarts; changing the user name or password signs
every app out.

| App      | Add it as        | Setting                                                     |
| -------- | ---------------- | ----------------------------------------------------------- |
| Swiftfin | a server         | —                                                           |
| Infuse   | a Jellyfin share | Library Mode off (Direct Mode), the default                 |
| Finamp   | a server         | Transcoding on, for Ogg and other formats Apple cannot play |

Tested with:

- Swiftfin on Apple TV
- Swiftfin on macOS
- Infuse on Apple TV, Direct Mode
- Finamp, the App Store version

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

> The live run changes, then restores: watched flags and resume points on one series
> (`testSeries` in `api_test.go`), an episode's track choice, one unplayed song's play state, and a
> favorite. Play counts, last-viewed dates and Plex's history do not come back: pick a series whose
> history you do not care about.

## How it maps

| Jellyfin                            | Plex                                                  |
| ----------------------------------- | ----------------------------------------------------- |
| libraries                           | movie, show and music sections, and collections       |
| `Movie` `Series` `Season` `Episode` | `movie` `show` `season` `episode`                     |
| resume, next up                     | on deck                                               |
| played, unplayed                    | `/:/scrobble`, `/:/unscrobble`; a song when it starts |
| favorites                           | the top rating, 10, as Plexamp loves                  |
| audio, subtitle choice              | the file's selected streams                           |
| playlists                           | Plex playlists                                        |
| genres; year and rating filters     | Plex genres; its `year`, `contentRating` filters      |
| language, first-letter filters      | `audioLanguage`, `subtitleLanguage`, `firstCharacter` |
| music transcoding                   | Plex's transcoder, AAC over HLS                       |
| playback progress                   | `/:/progress`                                         |
| stream, image, subtitle             | the part, `/photo/:/transcode`, the stream            |

A stopped video's position is read as Jellyfin reads it:

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
  defaults where the network is not yours alone. Wrong passwords in a row lock a user name out, by
  default five for 15 minutes; apps already signed in keep working.
- Video plays as stored: the app must decode the file, as Swiftfin's default player does.
- Only music transcodes, through Plex's transcoder, which loads codecs from Plex's data folder: that
  folder must not sit on a `noexec` mount.
- Movie, show and music libraries, and Plex collections: no photos or Live TV.
- Tag filters and people are empty.

## License

MIT — see `LICENSE`.
