package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// PlaylistChange is Jellyfin's create and update payload; Ids, when sent,
// is the whole playlist in order.
type PlaylistChange struct {
	Name      string   `json:"Name"`
	Ids       []string `json:"Ids"`
	MediaType string   `json:"MediaType"`
}

type PlaylistCreated struct {
	Id string `json:"Id"`
}

type PlaylistUser struct {
	UserId  string `json:"UserId"`
	CanEdit bool   `json:"CanEdit"`
}

type PlaylistInfo struct {
	OpenAccess bool           `json:"OpenAccess"`
	Shares     []PlaylistUser `json:"Shares"`
	ItemIds    []string       `json:"ItemIds"`
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var change PlaylistChange
	if err := json.NewDecoder(r.Body).Decode(&change); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	keys := itemKeys(change.Ids)
	if len(keys) == 0 {
		http.Error(w, "Plex keeps no empty playlist", http.StatusBadRequest)
		return
	}
	playlistType := map[bool]string{true: "video", false: "audio"}[change.MediaType == "Video"]
	key, err := s.plex.CreatePlaylist(change.Name, playlistType, ItemsURI(s.machineID, keys))
	respond(w, PlaylistCreated{Id: EncodeID(KindPlaylist, key)}, err)
}

func (s *Server) playlist(w http.ResponseWriter, r *http.Request) {
	key, ok := playlistKey(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	entries, err := s.plex.PlaylistItems(key)
	info := PlaylistInfo{Shares: []PlaylistUser{}, ItemIds: []string{}}
	for _, m := range entries {
		info.ItemIds = append(info.ItemIds, EncodeID(KindItem, m.RatingKey))
	}
	respond(w, info, err)
}

// playlistUser lets the one user edit every playlist but a smart one,
// which Plex fills by its rules.
func (s *Server) playlistUser(w http.ResponseWriter, r *http.Request) {
	key, ok := playlistKey(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	m, err := s.plex.Playlist(key)
	respond(w, PlaylistUser{UserId: s.userID(), CanEdit: !m.Smart}, err)
}

func (s *Server) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	key, ok := playlistKey(r)
	var change PlaylistChange
	if !ok || json.NewDecoder(r.Body).Decode(&change) != nil {
		http.Error(w, "bad playlist update", http.StatusBadRequest)
		return
	}
	if change.Name != "" {
		if err := s.plex.RenamePlaylist(key, change.Name); err != nil {
			fail(w, err)
			return
		}
	}
	if change.Ids != nil {
		if err := s.replacePlaylist(key, itemKeys(change.Ids)); err != nil {
			fail(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// replacePlaylist clears and refills, Plex's way to reorder and remove at once.
func (s *Server) replacePlaylist(key string, keys []string) error {
	if err := s.plex.ClearPlaylist(key); err != nil || len(keys) == 0 {
		return err
	}
	return s.plex.AddToPlaylist(key, ItemsURI(s.machineID, keys))
}

func (s *Server) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	key, ok := playlistKey(r)
	keys := itemKeys(query(r)["ids"])
	if !ok || len(keys) == 0 {
		http.Error(w, "bad playlist addition", http.StatusBadRequest)
		return
	}
	if err := s.plex.AddToPlaylist(key, ItemsURI(s.machineID, keys)); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeFromPlaylist takes entries by PlaylistItemId: numbers only, as they
// land in a Plex path.
func (s *Server) removeFromPlaylist(w http.ResponseWriter, r *http.Request) {
	key, ok := playlistKey(r)
	var entries []int
	for _, entry := range query(r)["entryids"] {
		id, err := strconv.Atoi(entry)
		if err != nil || id <= 0 {
			ok = false
		}
		entries = append(entries, id)
	}
	if !ok || len(entries) == 0 {
		http.Error(w, "bad playlist removal", http.StatusBadRequest)
		return
	}
	for _, entry := range entries {
		if err := s.plex.RemoveFromPlaylist(key, entry); err != nil {
			fail(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func playlistKey(r *http.Request) (string, bool) {
	kind, key, ok := DecodeID(r.PathValue("id"))
	return key, ok && kind == KindPlaylist
}

// itemKeys keeps the ids that name library items, as Plex rating keys.
func itemKeys(ids []string) []string {
	var keys []string
	for _, id := range ids {
		if kind, key, ok := DecodeID(id); ok && kind == KindItem {
			keys = append(keys, key)
		}
	}
	return keys
}
