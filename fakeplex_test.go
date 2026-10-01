package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fakePlexToken       = "fake-plex-token"
	fakeEpisodeMs       = 660_000
	fakeMovieMs         = 5_400_000
	fakeMovieCount      = 7
	fakeScrobbleAt      = 0.9
	fakeMovieSection    = "1"
	fakeShowSection     = "2"
	fakeMusicSection    = "3"
	fakeSeriesKey       = "200"
	fakeCollectionKey   = "150"
	fakePlaylistKey     = "500"
	fakePartFileSize    = 4096
	fakeSubtitleContent = "1\n00:00:01,000 --> 00:00:02,000\nhello\n"
	fakeMaxImageSide    = 16384
	fakeTrailerMs       = 106_000
	fakeBitrate         = 4000
	fakeTrackMs         = 200_000
	fakeRockGenre       = 900
	fakeDramaGenre      = 901
)

// fakePlex is an in-memory Plex Media Server: the subset of its API the
// proxy calls, with watch state that changes as Plex's would.
type fakePlex struct {
	mu      sync.Mutex
	items   map[string]*PlexMetadata
	members map[string][]string
	decided map[string]bool
	order   []string
	clock   int64
	mux     *http.ServeMux
}

func newFakePlex(t *testing.T) *httptest.Server {
	p := &fakePlex{items: map[string]*PlexMetadata{}, members: map[string][]string{}, decided: map[string]bool{}, clock: 1_700_000_000, mux: http.NewServeMux()}
	p.seed()
	p.routes()
	server := httptest.NewServer(p)
	t.Cleanup(server.Close)
	return server
}

func (p *fakePlex) add(m PlexMetadata) {
	p.clock += 100
	m.AddedAt = p.clock
	m.LibrarySectionID = map[string]int{"movie": 1, "collection": 1, "artist": 3, "album": 3, "track": 3}[m.Type]
	if m.LibrarySectionID == 0 {
		m.LibrarySectionID = 2
	}
	m.Thumb = fmt.Sprintf("/library/metadata/%s/thumb/%d", m.RatingKey, p.clock)
	if m.Type != "episode" {
		m.Art = fmt.Sprintf("/library/metadata/%s/art/%d", m.RatingKey, p.clock)
	}
	p.items[m.RatingKey] = &m
	p.order = append(p.order, m.RatingKey)
}

func fakeMedia(partID int, durationMs int64, bitrate int) []PlexMedia {
	return []PlexMedia{{Duration: durationMs, Bitrate: bitrate, Height: 1080, VideoCodec: "hevc", VideoResolution: "1080", Part: []PlexPart{{
		ID: partID, Key: fmt.Sprintf("/library/parts/%d/1/file.mkv", partID), Size: fakePartFileSize,
		File: fmt.Sprintf("/media/%d.mkv", partID), Container: "mkv", Duration: durationMs,
		Stream: []PlexStream{
			{ID: partID*10 + 1, StreamType: 1, Index: 0, Codec: "hevc", Width: 1920, Height: 1080},
			{ID: partID*10 + 2, StreamType: 2, Index: 1, Codec: "aac", LanguageCode: "eng", Channels: 2, Selected: true},
			{ID: partID*10 + 3, StreamType: 2, Index: 2, Codec: "ac3", LanguageCode: "rus", Channels: 6},
			{ID: partID*10 + 4, StreamType: 3, Index: 3, Codec: "pgs", LanguageCode: "eng"},
			{ID: partID*10 + 5, StreamType: 3, Key: fmt.Sprintf("/library/streams/%d", partID), Codec: "srt", LanguageCode: "eng"},
		},
	}}}}
}

func (p *fakePlex) seed() {
	for i := 1; i <= fakeMovieCount; i++ {
		key := strconv.Itoa(100 + i)
		p.add(PlexMetadata{RatingKey: key, Type: "movie", Title: fmt.Sprintf("Movie %d", i), Year: 2000 + i,
			Duration: fakeMovieMs, Media: fakeMedia(100+i, fakeMovieMs, fakeBitrate)})
	}
	p.items["102"].Genre = []PlexTag{{ID: fakeDramaGenre, Tag: "Fake Drama"}}
	// Plex merges versions of one title, as a trailer filed beside the movie.
	first := p.items["101"]
	first.Media = append(fakeMedia(1001, fakeTrailerMs, fakeBitrate/2), first.Media...)
	p.add(PlexMetadata{RatingKey: fakeCollectionKey, Type: "collection", Title: "Fake Saga", ChildCount: 2})
	p.members[fakeCollectionKey] = []string{"101", "102"}
	p.addMusic()
	p.add(PlexMetadata{RatingKey: fakePlaylistKey, Type: "playlist", Title: "Fake Mix", PlaylistType: "audio", LeafCount: 2,
		Duration: 2 * fakeTrackMs, Composite: "/playlists/" + fakePlaylistKey + "/composite/1"})
	p.members[fakePlaylistKey] = []string{"403", "402"}
	// Plexamp keeps smart playlists that stay empty until something is loved.
	p.add(PlexMetadata{RatingKey: "501", Type: "playlist", Title: "Loved", PlaylistType: "audio", Smart: true})
	p.add(PlexMetadata{RatingKey: "502", Type: "playlist", Title: "Recently Played", PlaylistType: "audio", Smart: true, LeafCount: 1})
	p.members["502"] = []string{"402"}
	p.addShow(fakeSeriesKey, "We Bare Bears", 2, 3)
	p.addShow("300", "The Other Show", 1, 1)
}

// addMusic files one band, one album and its tracks, as Plex's artist,
// album and track; the tracks are Ogg, as Plex keeps them.
func (p *fakePlex) addMusic() {
	p.add(PlexMetadata{RatingKey: "400", Type: "artist", Title: "Fake Band"})
	p.add(PlexMetadata{RatingKey: "401", Type: "album", Title: "Fake Album", Year: 2020, Index: 1,
		ParentRatingKey: "400", ParentTitle: "Fake Band", Genre: []PlexTag{{ID: fakeRockGenre, Tag: "Fake Rock"}}})
	album := p.items["401"]
	for i, title := range []string{"Opening", "Closing"} {
		key := 402 + i
		p.add(PlexMetadata{RatingKey: strconv.Itoa(key), Type: "track", Title: title, Index: i + 1, ParentIndex: 1,
			ParentRatingKey: "401", ParentTitle: "Fake Album", ParentThumb: album.Thumb, ParentYear: 2020,
			GrandparentRatingKey: "400", GrandparentTitle: "Fake Band", Duration: fakeTrackMs,
			Media: []PlexMedia{{Duration: fakeTrackMs, Bitrate: 320, Container: "ogg", Part: []PlexPart{{
				ID: key, Key: fmt.Sprintf("/library/parts/%d/1/file.ogg", key), File: fmt.Sprintf("/media/%d.ogg", key),
				Container: "ogg", Size: fakePartFileSize, Duration: fakeTrackMs,
				Stream: []PlexStream{{ID: key * 10, StreamType: 2, Index: 0, Codec: "vorbis", Channels: 2, SamplingRate: 44100, Selected: true}},
			}}}}})
	}
}

func (p *fakePlex) addShow(key, title string, seasons, episodes int) {
	show, _ := strconv.Atoi(key)
	p.add(PlexMetadata{RatingKey: key, Type: "show", Title: title})
	for s := 1; s <= seasons; s++ {
		season := strconv.Itoa(show + 10*s)
		p.add(PlexMetadata{RatingKey: season, Type: "season", Title: fmt.Sprintf("Season %d", s), Index: s,
			ParentRatingKey: key, ParentTitle: title})
		for e := 1; e <= episodes; e++ {
			rk := show + 10*s + e
			p.add(PlexMetadata{RatingKey: strconv.Itoa(rk), Type: "episode", Title: fmt.Sprintf("Episode %d", e),
				Index: e, ParentIndex: s, ParentRatingKey: season, ParentTitle: fmt.Sprintf("Season %d", s),
				GrandparentRatingKey: key, GrandparentTitle: title, Duration: fakeEpisodeMs,
				Media: fakeMedia(rk, fakeEpisodeMs, fakeBitrate)})
		}
	}
}

func (p *fakePlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("X-Plex-Token") != fakePlexToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mux.ServeHTTP(w, r)
}

func (p *fakePlex) routes() {
	p.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		p.write(w, PlexContainer{MachineIdentifier: strings.Repeat("ab", 20), FriendlyName: "Fake Plex", Version: "1.42"})
	})
	p.mux.HandleFunc("GET /library/sections", func(w http.ResponseWriter, r *http.Request) {
		p.write(w, PlexContainer{Directory: []PlexDirectory{
			{Key: fakeMovieSection, Type: "movie", Title: "Movies", Location: []PlexLocation{{Path: "/media/movies"}}},
			{Key: fakeShowSection, Type: "show", Title: "TV Shows", Location: []PlexLocation{{Path: "/media/tv"}}},
			{Key: fakeMusicSection, Type: "artist", Title: "Music", Location: []PlexLocation{{Path: "/media/music"}}},
		}})
	})
	p.mux.HandleFunc("GET /library/sections/{section}/all", func(w http.ResponseWriter, r *http.Request) {
		kind := map[string]string{fakeMovieSection: "movie", fakeShowSection: "show", fakeMusicSection: "artist"}[r.PathValue("section")]
		p.list(w, r, kind, sectionHolds[kind])
	})
	p.mux.HandleFunc("GET /library/all", func(w http.ResponseWriter, r *http.Request) { p.list(w, r, "", nil) })
	p.mux.HandleFunc("GET /playlists", func(w http.ResponseWriter, r *http.Request) {
		listed := p.matching(func(m *PlexMetadata) bool { return m.Type == "playlist" })
		for i := range listed {
			if listed[i].Smart {
				listed[i].LeafCount = 0 // Plex's list holds a smart playlist's count stale.
			}
		}
		p.write(w, PlexContainer{Metadata: listed})
	})
	p.mux.HandleFunc("GET /playlists/{key}", func(w http.ResponseWriter, r *http.Request) {
		p.write(w, PlexContainer{Metadata: p.matching(func(m *PlexMetadata) bool { return m.Type == "playlist" && m.RatingKey == r.PathValue("key") })})
	})
	p.mux.HandleFunc("GET /playlists/{key}/items", func(w http.ResponseWriter, r *http.Request) {
		var items []PlexMetadata
		for _, key := range p.members[r.PathValue("key")] {
			items = append(items, withoutStreams(p.view(p.items[key])))
		}
		p.write(w, PlexContainer{Metadata: items})
	})
	p.mux.HandleFunc("GET /library/sections/{section}/genre", func(w http.ResponseWriter, r *http.Request) {
		kind := fakeTypeNames[r.URL.Query().Get("type")]
		var genres []PlexDirectory
		for _, m := range p.matching(func(m *PlexMetadata) bool { return m.Type == kind }) {
			for _, g := range m.Genre {
				genres = append(genres, PlexDirectory{Key: strconv.Itoa(g.ID), Title: g.Tag})
			}
		}
		p.write(w, PlexContainer{Directory: genres})
	})
	p.mux.HandleFunc("GET /library/metadata/{key}", func(w http.ResponseWriter, r *http.Request) {
		m, ok := p.items[r.PathValue("key")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		p.write(w, PlexContainer{Metadata: []PlexMetadata{p.view(m)}})
	})
	p.mux.HandleFunc("GET /library/metadata/{key}/children", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		p.write(w, PlexContainer{Metadata: p.matching(func(m *PlexMetadata) bool {
			return m.ParentRatingKey == key || slices.Contains(p.members[key], m.RatingKey)
		})})
	})
	p.mux.HandleFunc("GET /library/sections/{section}/collections", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("section") != fakeMovieSection {
			p.write(w, PlexContainer{})
			return
		}
		p.write(w, PlexContainer{Metadata: p.matching(func(m *PlexMetadata) bool { return m.Type == "collection" })})
	})
	p.mux.HandleFunc("GET /library/metadata/{key}/allLeaves", func(w http.ResponseWriter, r *http.Request) {
		// Plex answers allLeaves only above the folders; a season gets an empty list.
		if m := p.items[r.PathValue("key")]; m == nil || (m.Type != "show" && m.Type != "artist") {
			p.write(w, PlexContainer{})
			return
		}
		p.write(w, PlexContainer{Metadata: p.leaves(r.PathValue("key"))})
	})
	p.mux.HandleFunc("GET /library/onDeck", func(w http.ResponseWriter, r *http.Request) {
		p.write(w, PlexContainer{Metadata: p.onDeck()})
	})
	p.mux.HandleFunc("GET /hubs/search", func(w http.ResponseWriter, r *http.Request) {
		term := strings.ToLower(r.URL.Query().Get("query"))
		hubs := map[string]*PlexHub{}
		var container PlexContainer
		for _, m := range p.matching(func(m *PlexMetadata) bool { return strings.Contains(strings.ToLower(m.Title), term) }) {
			if hubs[m.Type] == nil {
				container.Hub = append(container.Hub, PlexHub{Type: m.Type})
				hubs[m.Type] = &container.Hub[len(container.Hub)-1]
			}
			hub := &container.Hub[slices.IndexFunc(container.Hub, func(h PlexHub) bool { return h.Type == m.Type })]
			hub.Metadata = append(hub.Metadata, m)
		}
		p.write(w, container)
	})
	p.mux.HandleFunc("GET /:/scrobble", func(w http.ResponseWriter, r *http.Request) {
		p.scrobble(p.items[r.URL.Query().Get("key")])
	})
	p.mux.HandleFunc("GET /:/unscrobble", func(w http.ResponseWriter, r *http.Request) {
		for _, m := range p.subtree(r.URL.Query().Get("key")) {
			m.ViewCount, m.ViewOffset = 0, 0
		}
	})
	p.mux.HandleFunc("PUT /:/rate", func(w http.ResponseWriter, r *http.Request) {
		m := p.items[r.URL.Query().Get("key")]
		rating, err := strconv.ParseFloat(r.URL.Query().Get("rating"), 64)
		if m == nil || err != nil {
			http.Error(w, "bad rating", http.StatusBadRequest)
			return
		}
		m.UserRating = max(rating, 0)
	})
	p.mux.HandleFunc("GET /:/progress", func(w http.ResponseWriter, r *http.Request) {
		m := p.items[r.URL.Query().Get("key")]
		position, _ := strconv.ParseInt(r.URL.Query().Get("time"), 10, 64)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		if position > plexMinResumeMs {
			m.ViewOffset = position
		}
	})
	p.mux.HandleFunc("GET /photo/:/transcode", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		width, _ := strconv.Atoi(q.Get("width"))
		height, _ := strconv.Atoi(q.Get("height"))
		// Plex answers 500 to huge boxes; minSize makes it cover instead of fit.
		if q.Get("url") == "" || width == 0 || height == 0 || max(width, height) > fakeMaxImageSide || q.Has("minSize") {
			http.Error(w, "bad transcode", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte{0xff, 0xd8, 0xff})
	})
	p.mux.HandleFunc("GET /music/:/transcode/universal/decision", func(w http.ResponseWriter, r *http.Request) {
		p.decided[r.URL.Query().Get("session")] = true
		p.write(w, PlexContainer{})
	})
	p.mux.HandleFunc("GET /music/:/transcode/universal/start.m3u8", func(w http.ResponseWriter, r *http.Request) {
		// Plex converts only to a target the client declares, for a session it decided on.
		if !strings.Contains(r.Header.Get("X-Plex-Client-Profile-Extra"), "protocol=hls") || !p.decided[r.URL.Query().Get("session")] ||
			p.items[strings.TrimPrefix(r.URL.Query().Get("path"), "/library/metadata/")] == nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=256000\nsession/%s/base/index.m3u8\n", r.URL.Query().Get("session"))
	})
	p.mux.HandleFunc("GET /music/:/transcode/universal/session/{session}/base/{file}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("file") == "index.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n00000.ts\n#EXT-X-ENDLIST\n"))
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write(make([]byte, 188))
	})
	p.mux.HandleFunc("GET /library/parts/{id}/{stamp}/{file}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, r.PathValue("file"), time.Unix(p.clock, 0), bytes.NewReader(make([]byte, fakePartFileSize)))
	})
	p.mux.HandleFunc("PUT /library/parts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		q := r.URL.Query()
		for _, m := range p.items {
			for i := range m.Media {
				for j := range m.Media[i].Part {
					if part := &m.Media[i].Part[j]; part.ID == id {
						selectStream(part.Stream, plexAudio, q.Get("audioStreamID"))
						selectStream(part.Stream, plexSubtitle, q.Get("subtitleStreamID"))
					}
				}
			}
		}
	})
	p.mux.HandleFunc("GET /library/streams/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fakeSubtitleContent))
	})
}

func (p *fakePlex) write(w http.ResponseWriter, c PlexContainer) {
	c.Size = len(c.Metadata) + len(c.Directory)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(plexEnvelope{MediaContainer: c})
}

func (p *fakePlex) scrobble(m *PlexMetadata) {
	for _, leaf := range p.subtree(m.RatingKey) {
		leaf.ViewCount++
		leaf.ViewOffset = 0
		leaf.LastViewedAt = p.clock
	}
}

// subtree is the playable items under key: itself, or its episodes.
func (p *fakePlex) subtree(key string) []*PlexMetadata {
	var found []*PlexMetadata
	for _, k := range p.order {
		m := p.items[k]
		if m.Duration > 0 && (k == key || m.ParentRatingKey == key || m.GrandparentRatingKey == key) {
			found = append(found, m)
		}
	}
	return found
}

// view fills in the counts Plex derives for shows and seasons.
func (p *fakePlex) view(m *PlexMetadata) PlexMetadata {
	v := *m
	if slices.Contains([]string{"show", "season", "artist", "album"}, m.Type) {
		v.LeafCount, v.ViewedLeafCount = 0, 0
		for _, leaf := range p.subtree(m.RatingKey) {
			v.LeafCount++
			if leaf.ViewCount > 0 {
				v.ViewedLeafCount++
			}
		}
		v.ChildCount = len(p.matching(func(c *PlexMetadata) bool { return c.ParentRatingKey == m.RatingKey }))
	}
	return v
}

// matching lists items as Plex lists do: without their streams, and albums
// without their track count.
func (p *fakePlex) matching(keep func(*PlexMetadata) bool) []PlexMetadata {
	found := []PlexMetadata{}
	for _, k := range p.order {
		if keep(p.items[k]) {
			listed := withoutStreams(p.view(p.items[k]))
			if listed.Type == "album" {
				listed.LeafCount = 0
			}
			found = append(found, listed)
		}
	}
	return found
}

func withoutStreams(m PlexMetadata) PlexMetadata {
	media := slices.Clone(m.Media)
	for i := range media {
		media[i].Part = slices.Clone(media[i].Part)
		for j := range media[i].Part {
			media[i].Part[j].Stream = nil
		}
	}
	m.Media = media
	return m
}

func (p *fakePlex) leaves(key string) []PlexMetadata {
	var found []PlexMetadata
	for _, m := range p.subtree(key) {
		found = append(found, withoutStreams(p.view(m)))
	}
	return found
}

// onDeck is what Plex offers to continue: anything in progress, then the
// next unwatched episode of each show already begun.
func (p *fakePlex) onDeck() []PlexMetadata {
	deck := p.matching(func(m *PlexMetadata) bool { return m.ViewOffset > 0 })
	for _, show := range p.matching(func(m *PlexMetadata) bool { return m.Type == "show" }) {
		episodes := p.leaves(show.RatingKey)
		last := -1
		for i, e := range episodes {
			if e.ViewCount > 0 {
				last = i
			}
		}
		for _, e := range episodes[last+1:] {
			if last >= 0 && e.ViewCount == 0 && e.ViewOffset == 0 {
				deck = append(deck, e)
				break
			}
		}
	}
	return deck
}

var fakeTypeNames = map[string]string{
	"1": "movie", "2": "show", "3": "season", "4": "episode", "8": "artist", "9": "album", "10": "track",
}

// list answers a listing; a section holds only its own kinds of item.
func (p *fakePlex) list(w http.ResponseWriter, r *http.Request, plexType string, holds []string) {
	q := r.URL.Query()
	plexType = cmp.Or(fakeTypeNames[q.Get("type")], plexType)
	unwatched := q.Get("unwatched")
	rating, rated := q.Get("userRating"), q.Has("userRating")
	albums := strings.Split(q.Get("album.id"), ",")
	found := p.matching(func(m *PlexMetadata) bool {
		played := m.ViewCount > 0
		inSection := holds == nil || slices.Contains(holds, m.Type)
		ratingMatches := !rated || strconv.FormatFloat(m.UserRating, 'f', -1, 64) == rating
		inAlbum := !q.Has("album.id") || slices.Contains(albums, m.ParentRatingKey)
		inGenre := !q.Has("genre") || slices.ContainsFunc(p.genresOf(m), func(g PlexTag) bool { return strconv.Itoa(g.ID) == q.Get("genre") })
		return m.Type == plexType && inSection && ratingMatches && inAlbum && inGenre && (unwatched == "" || (unwatched == "1") != played)
	})
	field, order, _ := strings.Cut(q.Get("sort"), ":")
	key := map[string]func(PlexMetadata) string{
		"titleSort":       func(m PlexMetadata) string { return m.Title },
		"addedAt":         func(m PlexMetadata) string { return fmt.Sprint(m.AddedAt) },
		"episode.addedAt": func(m PlexMetadata) string { return fmt.Sprint(p.newestLeaf(m.RatingKey)) },
		"year":            func(m PlexMetadata) string { return fmt.Sprint(m.Year) },
	}[field]
	if key != nil {
		slices.SortStableFunc(found, func(a, b PlexMetadata) int {
			if order == "desc" {
				a, b = b, a
			}
			return strings.Compare(key(a), key(b))
		})
	}
	start, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
	size, err := strconv.Atoi(r.Header.Get("X-Plex-Container-Size"))
	if err != nil {
		size = len(found)
	}
	c := PlexContainer{TotalSize: len(found), Offset: start, Metadata: paginate(found, Page{Start: start, Size: size})}
	p.write(w, c)
}

func (p *fakePlex) newestLeaf(key string) int64 {
	var newest int64
	for _, m := range p.subtree(key) {
		newest = max(newest, m.AddedAt)
	}
	return newest
}

// selectStream marks one stream of a kind selected, as Plex does; "0" selects none.
func selectStream(streams []PlexStream, kind int, id string) {
	if id == "" {
		return
	}
	for i := range streams {
		if streams[i].StreamType == kind {
			streams[i].Selected = strconv.Itoa(streams[i].ID) == id
		}
	}
}

// genresOf is an item's genres; a track takes its album's, as Plex filters it.
func (p *fakePlex) genresOf(m *PlexMetadata) []PlexTag {
	if m.Type == "track" {
		if album := p.items[m.ParentRatingKey]; album != nil {
			return album.Genre
		}
	}
	return m.Genre
}
