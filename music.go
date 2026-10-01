package main

import (
	"cmp"
	"net/http"
	"net/url"
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

func (s *Server) artists(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	q["includeitemtypes"] = []string{"MusicArtist"}
	result, err := s.queryItems(q)
	respond(w, result, err)
}

// audioFile serves a track as stored, typed by its container.
func (s *Server) audioFile(w http.ResponseWriter, r *http.Request) {
	part, err := s.part(r.PathValue("id"), "")
	if err != nil {
		respond(w, "", err)
		return
	}
	s.proxyAs(w, r, s.plex.URL(part.Key, nil), http.Header{}, audioContentTypes[part.Container])
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
	s.proxyAs(w, r, s.plex.URL(transcodePath+"start.m3u8", url.Values{
		"path":         {"/library/metadata/" + m.RatingKey},
		"mediaIndex":   {"0"},
		"partIndex":    {"0"},
		"protocol":     {"hls"},
		"session":      {session},
		"directPlay":   {"0"},
		"directStream": {"0"},
		"hasMDE":       {"1"},
		"musicBitrate": {strconv.Itoa(bitrate / bitsPerKilobit)},
	}), transcoderHeader(session), "")
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
