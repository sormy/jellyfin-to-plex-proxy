package main

import (
	"cmp"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	transcodePath       = "/music/:/transcode/universal/"
	defaultAudioBitrate = 320_000
	transcoderProduct   = "jellyfin-to-plex-proxy"
)

// Plex serves every file as octet-stream; Apple's player needs the type.
var audioContentTypes = map[string]string{
	"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "mp4": "audio/mp4", "aac": "audio/aac",
	"alac": "audio/mp4", "wav": "audio/wav", "ogg": "audio/ogg", "opus": "audio/ogg",
}

// Plex converts only to a target the client declares.
const hlsAudioTarget = "add-transcode-target(type=musicProfile&context=streaming&protocol=hls&container=mpegts&audioCodec=aac)"

// withAlbumTotals adds what Plex leaves off albums: their length and, in
// lists, their track count, summed from one track query per section.
func (s *Server) withAlbumTotals(found []PlexMetadata) []PlexMetadata {
	bySection := map[int][]string{}
	for _, m := range found {
		if m.Type == "album" {
			bySection[m.LibrarySectionID] = append(bySection[m.LibrarySectionID], m.RatingKey)
		}
	}
	durations, counts := map[string]int64{}, map[string]int{}
	for section, albums := range bySection {
		tracks, err := s.plex.AlbumTracks(strconv.Itoa(section), albums)
		if err != nil {
			log.Printf("album totals: %v", err)
			return found
		}
		for _, t := range tracks {
			durations[t.ParentRatingKey] += t.Duration
			counts[t.ParentRatingKey]++
		}
	}
	for i, m := range found {
		if m.Type == "album" {
			found[i].Duration, found[i].LeafCount = durations[m.RatingKey], counts[m.RatingKey]
		}
	}
	return found
}

// genreTypes names whose genres a library lists: a music library's albums,
// as genre pages list albums.
var genreTypes = map[string]string{"movie": "1", "show": "2", "artist": "9"}

func (s *Server) genres(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	sections, err := s.plex.Sections()
	if err != nil {
		fail(w, err)
		return
	}
	if kind, key, ok := DecodeID(q.Get("parentid")); ok && kind == KindLibrary {
		sections = slices.DeleteFunc(sections, func(d PlexDirectory) bool { return d.Key != key })
	}
	genres := []Item{}
	seen := map[string]bool{}
	for _, section := range sections {
		plexType, ok := genreTypes[section.Type]
		if !ok {
			continue
		}
		found, err := s.plex.SectionValues(section.Key, "genre", plexType)
		if err != nil {
			fail(w, err)
			return
		}
		for _, genre := range found {
			id := EncodeID(KindGenre, genre.Key)
			if seen[id] {
				continue
			}
			seen[id] = true
			genres = append(genres, Item{
				Id: id, ServerId: s.serverID, Name: genre.Title, IsFolder: true,
				Type: map[bool]string{true: "MusicGenre", false: "Genre"}[section.Type == "artist"],
			})
		}
	}
	page := paging(q)
	writeJSON(w, ItemsResult{Items: paginate(genres, page), TotalRecordCount: len(genres), StartIndex: page.Start})
}

func (s *Server) artists(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	q["includeitemtypes"] = []string{"MusicArtist"}
	result, err := s.queryItems(q)
	respond(w, result, err)
}

// audioFile serves a track as stored, typed by its container.
func (s *Server) audioFile(w http.ResponseWriter, r *http.Request) {
	part, err := s.part(r.PathValue("id"), query(r).Get("mediasourceid"))
	if err != nil {
		respond(w, "", err)
		return
	}
	s.proxyAs(w, r, s.plex.URL(part.Key, nil), http.Header{}, audioContentTypes[part.Container])
}

// audioStream answers Jellyfin's stream and stream.{container}, as stored.
func (s *Server) audioStream(w http.ResponseWriter, r *http.Request) {
	if file := r.PathValue("file"); file != "stream" && !strings.HasPrefix(file, "stream.") {
		http.NotFound(w, r)
		return
	}
	s.audioFile(w, r)
}

// universalAudio plays a track as stored when the client lists its container,
// else as AAC over HLS: the playlist's session paths resolve beside this one.
func (s *Server) universalAudio(w http.ResponseWriter, r *http.Request) {
	part, err := s.part(r.PathValue("id"), query(r).Get("mediasourceid"))
	if err != nil {
		respond(w, "", err)
		return
	}
	if playsAsStored(query(r)["container"], part.Container) {
		s.audioFile(w, r)
		return
	}
	s.audioPlaylist(w, r)
}

// playsAsStored reads Jellyfin's `container|codec` list; an empty one takes any.
func playsAsStored(accepted []string, container string) bool {
	return len(accepted) == 0 || slices.ContainsFunc(accepted, func(entry string) bool {
		name, _, _ := strings.Cut(entry, "|")
		return strings.EqualFold(name, container)
	})
}

// audioPlaylist starts Plex converting a track to AAC over HLS. The playlist
// points at session paths relative to this one, which audioSession relays.
func (s *Server) audioPlaylist(w http.ResponseWriter, r *http.Request) {
	m, err := s.plexItem(r.PathValue("id"))
	if err != nil {
		respond(w, "", err)
		return
	}
	q := query(r)
	session := strings.ToLower(cmp.Or(q.Get("playsessionid"), s.playSession(r.PathValue("id"))))
	bitrate := cmp.Or(intParam(q, "audiobitrate"), intParam(q, "maxstreamingbitrate"), defaultAudioBitrate)
	transcode := url.Values{
		"path":         {"/library/metadata/" + m.RatingKey},
		"mediaIndex":   {"0"},
		"partIndex":    {"0"},
		"protocol":     {"hls"},
		"session":      {session},
		"directPlay":   {"0"},
		"directStream": {"0"},
		"hasMDE":       {"1"},
		"musicBitrate": {strconv.Itoa(bitrate / bitsPerKilobit)},
	}
	// Plex refuses to start a session it has not decided on first.
	if err := s.plex.call(http.MethodGet, transcodePath+"decision", transcode, transcoderHeader(session)); err != nil {
		fail(w, err)
		return
	}
	s.proxyAs(w, r, s.plex.URL(transcodePath+"start.m3u8", transcode), transcoderHeader(session), "")
}

// audioSession relays a transcode's playlists and segments; the session id
// in the path, unguessable, stands in for a token as players fetch bare.
func (s *Server) audioSession(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest")
	session, _, _ := strings.Cut(rest, "/")
	s.proxyAs(w, r, s.plex.URL(transcodePath+"session/"+rest, nil), transcoderHeader(session), "")
}

func transcoderHeader(session string) http.Header {
	return http.Header{
		"X-Plex-Client-Identifier":    {session},
		"X-Plex-Session-Identifier":   {session},
		"X-Plex-Product":              {transcoderProduct},
		"X-Plex-Platform":             {"Generic"},
		"X-Plex-Client-Profile-Extra": {hlsAudioTarget},
	}
}
