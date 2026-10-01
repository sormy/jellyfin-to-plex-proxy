package main

import (
	"cmp"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// Swiftfin warns about servers older than its SDK.
const jellyfinVersion = "12.0.0"

const (
	authenticationProvider = "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider"
	passwordResetProvider  = "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider"
)

const idLength = 32

var collectionTypes = map[string]string{"movie": "movies", "show": "tvshows", "artist": "music"}

type Config struct {
	UserName string
	Password string
}

type Server struct {
	plex       *Plex
	config     Config
	serverID   string
	serverName string
	mux        *http.ServeMux
}

func NewServer(plex *Plex, config Config) (*Server, error) {
	identity, err := plex.Identity()
	if err != nil {
		return nil, err
	}
	s := &Server{
		plex:       plex,
		config:     config,
		serverID:   identity.MachineIdentifier[:idLength],
		serverName: identity.FriendlyName,
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) secret(purpose, value string) string {
	mac := hmac.New(sha256.New, []byte(s.config.Password))
	mac.Write([]byte(purpose + ":" + value))
	return hex.EncodeToString(mac.Sum(nil))[:idLength]
}

func (s *Server) accessToken() string          { return s.secret("token", s.config.UserName) }
func (s *Server) userID() string               { return s.secret("user", s.config.UserName) }
func (s *Server) playSession(id string) string { return s.secret("play", id) }

func (s *Server) user() User {
	return User{
		Id:                    s.userID(),
		Name:                  s.config.UserName,
		ServerId:              s.serverID,
		HasPassword:           true,
		HasConfiguredPassword: true,
		Configuration: UserConfiguration{
			MyMediaExcludes:            []string{},
			LatestItemsExcludes:        []string{},
			OrderedViews:               []string{},
			PlayDefaultAudioTrack:      true,
			SubtitleMode:               "Default",
			HidePlayedInLatest:         true,
			RememberAudioSelections:    true,
			RememberSubtitleSelections: true,
			EnableNextEpisodeAutoPlay:  true,
		},
		Policy: UserPolicy{
			EnableUserPreferenceAccess:     true,
			EnableRemoteAccess:             true,
			EnableMediaPlayback:            true,
			EnableAudioPlaybackTranscoding: true,
			EnableContentDownloading:       true,
			EnableAllDevices:               true,
			EnableAllFolders:               true,
			SyncPlayAccess:                 "None",
			AuthenticationProviderId:       authenticationProvider,
			PasswordResetProviderId:        passwordResetProvider,
		},
	}
}

// ClientFields parses `MediaBrowser Key=value, Key="value"`, as Jellyfin
// clients send it in Authorization or X-Emby-Authorization.
func ClientFields(r *http.Request) map[string]string {
	header := r.Header.Get("Authorization")
	if header == "" {
		header = r.Header.Get("X-Emby-Authorization")
	}
	_, header, _ = strings.Cut(header, " ")
	fields := map[string]string{}
	for _, pair := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok {
			fields[key] = strings.Trim(value, `"`)
		}
	}
	return fields
}

func (s *Server) authorized(r *http.Request) bool {
	candidates := []string{
		ClientFields(r)["Token"],
		r.Header.Get("X-Emby-Token"),
		r.Header.Get("X-MediaBrowser-Token"),
		query(r).Get("api_key"),
		query(r).Get("apikey"),
	}
	for _, token := range candidates {
		if hmac.Equal([]byte(token), []byte(s.accessToken())) {
			return true
		}
	}
	return false
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	r.URL.Path = strings.ToLower(r.URL.Path)
	s.mux.ServeHTTP(recorder, r)
	log.Printf("%s: %s %s?%s %d", cmp.Or(ClientFields(r)["Client"], "-"), r.Method, r.URL.Path, r.URL.RawQuery, recorder.status)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// public registers a route; paths match case-insensitively, as Jellyfin's do.
func (s *Server) public(pattern string, handler http.HandlerFunc) {
	method, path, _ := strings.Cut(pattern, " ")
	s.mux.HandleFunc(method+" "+strings.ToLower(path), handler)
}

func (s *Server) private(pattern string, handler http.HandlerFunc) {
	s.public(pattern, func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler(w, r)
	})
}

func (s *Server) routes() {
	s.public("GET /System/Info/Public", s.systemInfo)
	s.public("GET /Users/Public", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, []User{s.user()}) })
	s.public("GET /QuickConnect/Enabled", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, false) })
	s.public("GET /Branding/Configuration", emptyObject)
	s.public("POST /Users/AuthenticateByName", s.authenticate)
	s.public("GET /Items/{id}/Images/{type}", s.image)
	s.public("GET /Items/{id}/Images/{type}/{index}", s.image)
	s.public("GET /Videos/{id}/{file}", s.stream)
	s.public("GET /Videos/{id}/{source}/Subtitles/{index}/{start}/{file}", s.subtitle)

	s.private("GET /Users/Me", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.user()) })
	s.private("GET /Users/{userId}", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.user()) })
	s.private("DELETE /Auth/Keys/{token}", noContent)
	s.private("POST /Sessions/Capabilities", noContent)
	s.private("POST /Sessions/Capabilities/Full", noContent)
	s.private("GET /UserViews", s.views)
	s.private("GET /Users/{userId}/Views", s.views)
	s.private("GET /UserViews/GroupingOptions", s.groupingOptions)
	s.private("GET /Library/VirtualFolders", s.virtualFolders)
	s.private("GET /DisplayPreferences/{id}", s.displayPreferences)
	s.private("POST /DisplayPreferences/{id}", noContent)
	s.private("GET /Library/MediaFolders", s.views)
	s.private("GET /Users/{userId}/GroupingOptions", s.groupingOptions)
	s.private("GET /Items", s.items)
	s.private("GET /Users/{userId}/Items", s.items)
	s.private("GET /Items/{id}", s.item)
	s.private("GET /Users/{userId}/Items/{id}", s.item)
	s.private("GET /Items/Latest", s.latest)
	s.private("GET /Users/{userId}/Items/Latest", s.latest)
	s.private("GET /UserItems/Resume", s.resume)
	s.private("GET /Users/{userId}/Items/Resume", s.resume)
	s.private("GET /Shows/NextUp", s.nextUp)
	s.private("GET /Shows/{id}/Seasons", s.seasons)
	s.private("GET /Shows/{id}/Episodes", s.episodes)
	s.private("POST /Items/{id}/PlaybackInfo", s.playbackInfo)
	s.private("GET /Playback/BitrateTest", s.bitrateTest)
	s.private("GET /Items/{id}/PlaybackInfo", s.playbackInfo)
	s.private("GET /System/Endpoint", emptyObject)
	s.private("POST /Sessions/Logout", noContent)
	s.private("GET /Genres", s.genres)
	s.private("GET /Playlists/{id}/Items", s.playlistItems)
	s.private("GET /Artists", s.artists)
	s.private("GET /Artists/AlbumArtists", s.artists)
	s.private("GET /Items/{id}/File", s.audioFile)
	s.private("GET /Audio/{id}/main.m3u8", s.audioPlaylist)
	s.public("GET /Audio/{id}/session/{rest...}", s.audioSession)
	s.private("POST /Sessions/Playing", s.reportPlayback(playbackStarted))
	s.private("POST /Sessions/Playing/Progress", s.reportPlayback(playbackProgressed))
	s.private("POST /Sessions/Playing/Stopped", s.reportPlayback(playbackStopped))
	for _, pattern := range []string{"/UserPlayedItems/{id}", "/Users/{userId}/PlayedItems/{id}"} {
		s.private("POST "+pattern, s.markPlayed(true))
		s.private("DELETE "+pattern, s.markPlayed(false))
	}
	for _, pattern := range []string{"/UserFavoriteItems/{id}", "/Users/{userId}/FavoriteItems/{id}"} {
		s.private("POST "+pattern, s.favorite)
		s.private("DELETE "+pattern, s.favorite)
	}
	for _, pattern := range []string{"GET /Items/{id}/LocalTrailers", "GET /Items/{id}/SpecialFeatures"} {
		s.private(pattern, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, []Item{}) })
	}
	s.private("GET /Localization/Cultures", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, cultures) })
	for _, pattern := range []string{
		"GET /Items/{id}/Similar", "GET /Videos/{id}/AdditionalParts", "GET /Persons", "GET /LiveTv/Programs/Recommended",
		"GET /MediaSegments/{id}", "GET /Albums/{id}/Similar", "GET /Items/{id}/InstantMix",
	} {
		s.private(pattern, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, ItemsResult{Items: []Item{}}) })
	}
	s.private("GET /Items/Filters", s.filtersLegacy)
	s.private("GET /Items/Filters2", s.filters)
}

func writeJSON[T any](w http.ResponseWriter, value T) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode: %v", err)
	}
}

func fail(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	http.Error(w, err.Error(), http.StatusBadGateway)
}

func emptyObject(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{}"))
}

func noContent(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// displayPreferences answers Jellyfin's defaults: the proxy stores none.
func (s *Server) displayPreferences(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, DisplayPreferences{
		Id:              r.PathValue("id"),
		Client:          query(r).Get("client"),
		SortBy:          "SortName",
		SortOrder:       "Ascending",
		ScrollDirection: "Horizontal",
		ShowBackdrop:    true,
		CustomPrefs:     map[string]string{},
	})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, PublicSystemInfo{
		Id:                     s.serverID,
		ServerName:             s.serverName,
		Version:                jellyfinVersion,
		ProductName:            "Jellyfin Server",
		StartupWizardCompleted: true,
		LocalAddress:           map[bool]string{true: "https://", false: "http://"}[r.TLS != nil] + r.Host,
	})
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) {
	var request AuthenticateByName
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nameMatches := strings.EqualFold(request.Username, s.config.UserName)
	if !nameMatches || !hmac.Equal([]byte(request.Pw), []byte(s.config.Password)) {
		http.Error(w, "invalid user name or password", http.StatusUnauthorized)
		return
	}
	writeJSON(w, AuthenticationResult{User: s.user(), AccessToken: s.accessToken(), ServerId: s.serverID})
}
