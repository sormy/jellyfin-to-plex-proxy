package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	allItems           = 100_000
	defaultLatestLimit = 16
	searchLimit        = 50
)

// Plex's stream types, and its id for no subtitle.
const (
	plexAudio    = 2
	plexSubtitle = 3
	noSubtitle   = 0
)

// Jellyfin's defaults for what a stopped position means.
const (
	minResumeFraction   = 0.05
	maxResumeFraction   = 0.9
	minResumeDurationMs = 300_000
	plexMinResumeMs     = 60_000
)

var jellyfinToPlexTypes = map[string]string{
	"movie": "movie", "series": "show", "season": "season", "episode": "episode",
	"musicartist": "artist", "musicalbum": "album", "audio": "track", "playlist": "playlist",
}

var plexTypeNumbers = map[string]string{
	"movie": "1", "show": "2", "season": "3", "episode": "4", "artist": "8", "album": "9", "track": "10",
}

// sectionHolds lists the item types a Plex section of each kind contains.
var sectionHolds = map[string][]string{
	"movie":  {"movie"},
	"show":   {"show", "season", "episode"},
	"artist": {"artist", "album", "track"},
}

// folderTypes are the items whose children are folders themselves.
var folderTypes = []string{"season", "album"}

var plexSorts = map[string]string{
	"sortname":             "titleSort",
	"name":                 "titleSort",
	"datecreated":          "addedAt",
	"datelastcontentadded": "addedAt",
	"premieredate":         "originallyAvailableAt",
	"productionyear":       "year",
	"communityrating":      "audienceRating",
	"criticrating":         "rating",
	"officialrating":       "contentRating",
	"dateplayed":           "lastViewedAt",
	"playcount":            "viewCount",
	"runtime":              "duration",
	"studio":               "studio",
	"videobitrate":         "mediaBitrate",
	"albumartist":          "artist.titleSort",
	"random":               "random",
}

// A show's content arrives as episodes, so it sorts by its newest one.
var plexShowSorts = map[string]string{"datelastcontentadded": "episode.addedAt"}

var plexFilters = map[string][2]string{
	"isplayed":    {"unwatched", "0"},
	"isunplayed":  {"unwatched", "1"},
	"isresumable": {"inProgress", "1"},
}

// Latest rows per library kind: movies by arrival, shows by newest episode.
var plexLatest = map[string]url.Values{
	"movie":  {"type": {"1"}, "sort": {"addedAt:desc"}},
	"show":   {"type": {"2"}, "sort": {"episode.addedAt:desc"}},
	"artist": {"type": {"9"}, "sort": {"addedAt:desc"}},
}

var errNotFound = errors.New("not found")

// query lowercases keys and splits comma lists, so exploded and joined
// array parameters read the same, whatever casing the client uses.
func query(r *http.Request) url.Values {
	return lowercaseKeys(r.URL.Query())
}

func lowercaseKeys(q url.Values) url.Values {
	values := url.Values{}
	for key, list := range q {
		for _, value := range list {
			values[strings.ToLower(key)] = append(values[strings.ToLower(key)], strings.Split(value, ",")...)
		}
	}
	return values
}

func intParam(q url.Values, key string) int {
	n, _ := strconv.Atoi(q.Get(key))
	return n
}

func paging(q url.Values) Page {
	page := Page{Start: intParam(q, "startindex"), Size: intParam(q, "limit")}
	if page.Size <= 0 {
		page.Size = allItems
	}
	return page
}

func paginate[T any](all []T, page Page) []T {
	start := min(page.Start, len(all))
	return all[start:min(start+page.Size, len(all))]
}

func plexTypes(q url.Values) []string {
	var types []string
	for _, name := range q["includeitemtypes"] {
		if plexType, ok := jellyfinToPlexTypes[strings.ToLower(name)]; ok {
			types = append(types, plexType)
		}
	}
	return types
}

// plexQuery translates Jellyfin sorting and filters into Plex's.
func plexQuery(q url.Values, plexType string) url.Values {
	v := url.Values{}
	if plexType != "" {
		v.Set("type", plexTypeNumbers[plexType])
	}
	sortBy := strings.ToLower(q.Get("sortby"))
	if sort, ok := plexSorts[sortBy]; ok {
		if plexType == "show" {
			sort = cmp.Or(plexShowSorts[sortBy], sort)
		}
		if strings.EqualFold(q.Get("sortorder"), "Descending") {
			sort += ":desc"
		}
		v.Set("sort", sort)
	}
	for _, filter := range q["filters"] {
		if f, ok := plexFilters[strings.ToLower(filter)]; ok {
			v.Set(f[0], f[1])
		}
	}
	if favoritesOnly(q) {
		v.Set("userRating", strconv.Itoa(lovedRating))
	}
	var genres []string
	for _, id := range q["genreids"] {
		if kind, key, ok := DecodeID(id); ok && kind == KindGenre {
			genres = append(genres, key)
		}
	}
	if len(genres) > 0 {
		v.Set("genre", strings.Join(genres, ","))
	}
	if ratings := valuesOf(q, "officialratings"); len(ratings) > 0 {
		v.Set("contentRating", strings.Join(ratings, ","))
	}
	switch q.Get("isplayed") {
	case "true":
		v.Set("unwatched", "0")
	case "false":
		v.Set("unwatched", "1")
	}
	if years := q["years"]; len(years) > 0 {
		v.Set("year", strings.Join(years, ","))
	}
	return v
}

func favoritesOnly(q url.Values) bool {
	return q.Get("isfavorite") == "true" || slices.ContainsFunc(q["filters"], func(f string) bool { return strings.EqualFold(f, "IsFavorite") })
}

func (s *Server) result(found []PlexMetadata, page Page) ItemsResult {
	items := ToItems(s.serverID, s.withAlbumTotals(found))
	return ItemsResult{Items: paginate(items, page), TotalRecordCount: len(items), StartIndex: page.Start}
}

func ofTypes(found []PlexMetadata, types []string) []PlexMetadata {
	if len(types) == 0 {
		return found
	}
	return slices.DeleteFunc(found, func(m PlexMetadata) bool { return !slices.Contains(types, m.Type) })
}

func respond[T any](w http.ResponseWriter, value T, err error) {
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, value)
	}
}

func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	result, err := s.queryItems(query(r))
	respond(w, result, err)
}

func (s *Server) queryItems(q url.Values) (ItemsResult, error) {
	page := paging(q)
	types := plexTypes(q)
	empty := ItemsResult{Items: []Item{}, StartIndex: page.Start}
	switch {
	case len(q["includeitemtypes"]) > 0 && len(types) == 0:
		return empty, nil
	case len(q["ids"]) > 0:
		return s.byIDs(q["ids"])
	case slices.Contains(types, "playlist"):
		playlists, err := s.plex.Playlists()
		return s.result(playlists, page), err
	case q.Get("searchterm") != "":
		if page.Start > 0 {
			return empty, nil
		}
		found, err := s.plex.Search(q.Get("searchterm"), searchLimit)
		return s.result(ofTypes(found, types), page), err
	}
	// Music apps name an artist or album to list what it holds.
	parent := cmp.Or(q.Get("albumartistids"), q.Get("artistids"), q.Get("albumids"), q.Get("parentid"))
	recursive := q.Get("recursive") == "true" || q.Get("parentid") == ""
	kind, key, ok := DecodeID(parent)
	switch {
	case ok && kind == KindLibrary && key == collectionsKey:
		sections, err := s.plex.Sections()
		if err != nil {
			return ItemsResult{}, err
		}
		collections, err := s.collections(sections)
		return s.result(collections, page), err
	case ok && kind == KindLibrary:
		section, err := s.section(key)
		if err != nil {
			return ItemsResult{}, err
		}
		held := slices.DeleteFunc(slices.Clone(types), func(t string) bool { return !slices.Contains(sectionHolds[section.Type], t) })
		if len(types) > 0 && len(held) == 0 {
			return empty, nil
		}
		plexFilter := plexQuery(q, firstOr(held, ""))
		named, err := s.genreKeys(q)
		if err != nil {
			return ItemsResult{}, err
		}
		if len(named) > 0 {
			plexFilter.Set("genre", strings.Join(append(named, valuesOf(plexFilter, "genre")...), ","))
		} else if len(valuesOf(q, "genres")) > 0 {
			return empty, nil
		}
		c, err := s.plex.SectionItems(key, plexFilter, page)
		return ItemsResult{Items: ToItems(s.serverID, s.withAlbumTotals(c.Metadata)), TotalRecordCount: c.TotalSize, StartIndex: page.Start}, err
	case ok && kind == KindPlaylist:
		found, err := s.plex.PlaylistItems(key)
		return s.result(ofTypes(found, types), page), err
	case ok && kind == KindItem:
		children := s.plex.Children
		if (slices.Contains(types, "episode") || slices.Contains(types, "track")) && recursive {
			children = s.episodesUnder
		}
		found, err := children(key)
		if favoritesOnly(q) {
			found = slices.DeleteFunc(found, func(m PlexMetadata) bool { return m.UserRating < lovedRating })
		}
		return s.result(ofTypes(found, types), page), err
	default:
		return s.acrossLibraries(q, types, page)
	}
}

func firstOr(list []string, fallback string) string {
	if len(list) == 0 {
		return fallback
	}
	return list[0]
}

// acrossLibraries serves queries without a parent, such as the home
// screen's recently added row, by merging each requested type.
func (s *Server) acrossLibraries(q url.Values, types []string, page Page) (ItemsResult, error) {
	var found []PlexMetadata
	for _, plexType := range types {
		c, err := s.plex.LibraryItems(plexQuery(q, plexType), Page{Size: page.Start + page.Size})
		if err != nil {
			return ItemsResult{}, err
		}
		found = append(found, c.Metadata...)
	}
	if len(types) > 1 {
		sortMerged(found, plexQuery(q, "").Get("sort"))
	}
	return s.result(found, page), nil
}

func sortMerged(found []PlexMetadata, sort string) {
	field, order, _ := strings.Cut(sort, ":")
	compare := func(a, b PlexMetadata) int { return cmp.Compare(a.TitleSort+a.Title, b.TitleSort+b.Title) }
	if field == "addedAt" {
		compare = func(a, b PlexMetadata) int { return cmp.Compare(a.AddedAt, b.AddedAt) }
	}
	slices.SortStableFunc(found, func(a, b PlexMetadata) int {
		if order == "desc" {
			return compare(b, a)
		}
		return compare(a, b)
	})
}

func (s *Server) byIDs(ids []string) (ItemsResult, error) {
	var found []PlexMetadata
	for _, id := range ids {
		m, err := s.plexItem(id)
		if err != nil {
			return ItemsResult{}, err
		}
		found = append(found, m)
	}
	return s.result(found, Page{Size: allItems}), nil
}

// episodesUnder lists what plays under a show, season, artist or album:
// Plex's allLeaves answers only above the folders, so a season's or an
// album's children are its leaves.
func (s *Server) episodesUnder(key string) ([]PlexMetadata, error) {
	children, err := s.plex.Children(key)
	if err != nil || !slices.ContainsFunc(children, func(m PlexMetadata) bool { return slices.Contains(folderTypes, m.Type) }) {
		return children, err
	}
	return s.plex.AllLeaves(key)
}

func (s *Server) plexItem(id string) (PlexMetadata, error) {
	kind, key, ok := DecodeID(strings.ReplaceAll(id, "-", ""))
	switch {
	case ok && kind == KindItem:
		return s.plex.Item(key)
	case ok && kind == KindPlaylist:
		return s.plex.Playlist(key)
	default:
		return PlexMetadata{}, errNotFound
	}
}

func (s *Server) playlistItems(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	q["parentid"] = []string{r.PathValue("id")}
	result, err := s.queryItems(q)
	respond(w, result, err)
}

// A library counts its top-level items, and everything playable in it.
var sectionCounts = map[string][2]string{
	"movie": {"movie", "movie"}, "show": {"show", "episode"}, "artist": {"artist", "track"},
}

func (s *Server) libraries() ([]Item, error) {
	sections, err := s.plex.Sections()
	views := []Item{}
	for _, section := range sections {
		collection, ok := collectionTypes[section.Type]
		if !ok {
			continue
		}
		counts := sectionCounts[section.Type]
		children, countErr := s.count(section.Key, counts[0])
		playable, playableErr := s.count(section.Key, counts[1])
		views = append(views, Item{
			Id:                 EncodeID(KindLibrary, section.Key),
			ServerId:           s.serverID,
			Name:               section.Title,
			Type:               "CollectionFolder",
			CollectionType:     collection,
			IsFolder:           true,
			ChildCount:         children,
			RecursiveItemCount: playable,
		})
		err = cmp.Or(err, countErr, playableErr)
	}
	collections, collectionsErr := s.collections(sections)
	if len(collections) > 0 {
		movies := 0
		for _, c := range collections {
			movies += c.ChildCount
		}
		views = append(views, Item{
			Id:                 EncodeID(KindLibrary, collectionsKey),
			ServerId:           s.serverID,
			Name:               collectionsName,
			Type:               "CollectionFolder",
			CollectionType:     "boxsets",
			IsFolder:           true,
			ChildCount:         len(collections),
			RecursiveItemCount: movies,
		})
	}
	return views, cmp.Or(err, collectionsErr)
}

// collectionsKey names the library of every Plex collection; no Plex
// section has key 0.
const (
	collectionsKey  = "0"
	collectionsName = "Collections"
)

func (s *Server) collections(sections []PlexDirectory) ([]PlexMetadata, error) {
	var found []PlexMetadata
	for _, section := range sections {
		if _, ok := collectionTypes[section.Type]; !ok {
			continue
		}
		collections, err := s.plex.Collections(section.Key)
		if err != nil {
			return nil, err
		}
		found = append(found, collections...)
	}
	return found, nil
}

// count asks Plex for a total alone: a page of no items still carries it.
func (s *Server) count(section, plexType string) (int, error) {
	c, err := s.plex.SectionItems(section, url.Values{"type": {plexTypeNumbers[plexType]}}, Page{})
	return c.TotalSize, err
}

func (s *Server) views(w http.ResponseWriter, r *http.Request) {
	views, err := s.libraries()
	respond(w, ItemsResult{Items: views, TotalRecordCount: len(views)}, err)
}

// groupingOptions names the libraries a client may group by; Infuse asks first.
func (s *Server) groupingOptions(w http.ResponseWriter, r *http.Request) {
	views, err := s.libraries()
	options := []NameID{}
	for _, view := range views {
		options = append(options, NameID{Name: view.Name, Id: view.Id})
	}
	respond(w, options, err)
}

func (s *Server) virtualFolders(w http.ResponseWriter, r *http.Request) {
	sections, err := s.plex.Sections()
	folders := []VirtualFolder{}
	for _, section := range sections {
		collection, ok := collectionTypes[section.Type]
		if !ok {
			continue
		}
		folder := VirtualFolder{
			Name: section.Title, ItemId: EncodeID(KindLibrary, section.Key), CollectionType: collection, Locations: []string{},
		}
		for _, location := range section.Location {
			folder.Locations = append(folder.Locations, location.Path)
		}
		folders = append(folders, folder)
	}
	// Collections live in the libraries above; Infuse skips a folder without locations.
	if collections, collectionsErr := s.collections(sections); len(collections) > 0 {
		folder := VirtualFolder{Name: collectionsName, ItemId: EncodeID(KindLibrary, collectionsKey), CollectionType: "boxsets"}
		for _, f := range folders {
			folder.Locations = append(folder.Locations, f.Locations...)
		}
		folders = append(folders, folder)
	} else {
		err = cmp.Or(err, collectionsErr)
	}
	respond(w, folders, err)
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	item, err := s.lookup(r.PathValue("id"))
	respond(w, item, err)
}

func (s *Server) lookup(id string) (Item, error) {
	if kind, _, ok := DecodeID(id); ok && kind == KindLibrary {
		views, err := s.libraries()
		if i := slices.IndexFunc(views, func(v Item) bool { return v.Id == id }); i >= 0 {
			return views[i], err
		}
		return Item{}, errNotFound
	}
	m, err := s.plexItem(id)
	if err != nil {
		return Item{}, err
	}
	item, ok := ToItem(s.serverID, s.withAlbumTotals([]PlexMetadata{m})[0])
	if !ok {
		return Item{}, errNotFound
	}
	return item, nil
}

func (s *Server) latest(w http.ResponseWriter, r *http.Request) {
	items, err := s.latestItems(query(r))
	respond(w, items, err)
}

func (s *Server) latestItems(q url.Values) ([]Item, error) {
	kind, key, ok := DecodeID(q.Get("parentid"))
	if !ok || kind != KindLibrary || key == collectionsKey {
		return []Item{}, nil
	}
	section, err := s.section(key)
	if err != nil {
		return nil, err
	}
	limit := intParam(q, "limit")
	if limit <= 0 {
		limit = defaultLatestLimit
	}
	c, err := s.plex.SectionItems(key, plexLatest[section.Type], Page{Size: limit})
	return ToItems(s.serverID, s.withAlbumTotals(c.Metadata)), err
}

func (s *Server) section(key string) (PlexDirectory, error) {
	sections, err := s.plex.Sections()
	if err != nil {
		return PlexDirectory{}, err
	}
	i := slices.IndexFunc(sections, func(d PlexDirectory) bool { return d.Key == key })
	if i < 0 {
		return PlexDirectory{}, errNotFound
	}
	return sections[i], nil
}

func inProgress(m PlexMetadata) bool { return m.ViewOffset > 0 }

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	var found []PlexMetadata
	var err error
	if kind, key, ok := DecodeID(q.Get("parentid")); ok && kind == KindItem {
		found, err = s.episodesUnder(key)
	} else {
		found, err = s.plex.OnDeck()
	}
	found = slices.DeleteFunc(found, func(m PlexMetadata) bool { return !inProgress(m) })
	respond(w, s.result(found, paging(q)), err)
}

func (s *Server) nextUp(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	var found []PlexMetadata
	var err error
	if seriesID := q.Get("seriesid"); seriesID != "" {
		found, err = s.nextEpisode(seriesID)
	} else {
		found, err = s.plex.OnDeck()
		found = slices.DeleteFunc(found, func(m PlexMetadata) bool { return m.Type != "episode" || inProgress(m) })
	}
	respond(w, s.result(found, paging(q)), err)
}

// nextEpisode is the first unwatched episode after the last watched one.
func (s *Server) nextEpisode(seriesID string) ([]PlexMetadata, error) {
	_, key, ok := DecodeID(seriesID)
	if !ok {
		return nil, errNotFound
	}
	episodes, err := s.episodesUnder(key)
	lastWatched := -1
	for i, m := range episodes {
		if m.ViewCount > 0 {
			lastWatched = i
		}
	}
	if lastWatched < 0 {
		return nil, err
	}
	for _, m := range episodes[lastWatched+1:] {
		if m.ViewCount == 0 {
			return []PlexMetadata{m}, err
		}
	}
	return nil, err
}

func (s *Server) seasons(w http.ResponseWriter, r *http.Request) {
	_, key, _ := DecodeID(r.PathValue("id"))
	found, err := s.plex.Children(key)
	respond(w, s.result(ofTypes(found, []string{"season"}), Page{Size: allItems}), err)
}

// episodes keys off seasonId: Swiftfin puts the season id in the path too.
func (s *Server) episodes(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	parent := cmp.Or(q.Get("seasonid"), r.PathValue("id"))
	_, key, _ := DecodeID(parent)
	found, err := s.episodesUnder(key)
	if _, adjacent, ok := DecodeID(q.Get("adjacentto")); ok {
		found = adjacentTo(found, adjacent)
	}
	respond(w, s.result(found, paging(q)), err)
}

// adjacentTo keeps an episode and its neighbours, as Jellyfin's adjacentTo does.
func adjacentTo(episodes []PlexMetadata, ratingKey string) []PlexMetadata {
	i := slices.IndexFunc(episodes, func(m PlexMetadata) bool { return m.RatingKey == ratingKey })
	if i < 0 {
		return nil
	}
	return episodes[max(i-1, 0):min(i+2, len(episodes))]
}

// playbackInfo also saves the tracks a client asks for, so a choice made
// before playing reaches Plex even if playback stops at once.
func (s *Server) playbackInfo(w http.ResponseWriter, r *http.Request) {
	var request PlaybackReport
	json.NewDecoder(r.Body).Decode(&request)
	if m, err := s.plexItem(r.PathValue("id")); err == nil {
		if err := s.rememberTracks(m, request); err != nil {
			log.Printf("remember tracks: %v", err)
		}
	}
	item, err := s.lookup(r.PathValue("id"))
	sources := item.MediaSources
	if request.MediaSourceId != "" {
		sources = slices.DeleteFunc(sources, func(m MediaSource) bool { return m.Id != request.MediaSourceId })
	}
	if err == nil && len(sources) == 0 {
		err = errNotFound
	}
	respond(w, PlaybackInfo{MediaSources: sources, PlaySessionId: s.playSession(item.Id)}, err)
}

// PlayState is Jellyfin's reading of a playback position: whether the item
// counts as watched, and where it resumes (0 for nowhere). Plex keeps no
// resume point in the first minute, so neither does this.
func PlayState(positionMs, durationMs int64) (played bool, resumeMs int64) {
	if durationMs <= 0 {
		return true, 0
	}
	fraction := float64(positionMs) / float64(durationMs)
	switch {
	case positionMs <= plexMinResumeMs:
		return false, 0
	case fraction < minResumeFraction:
		return false, 0
	case fraction > maxResumeFraction || positionMs >= durationMs:
		return true, 0
	case durationMs < minResumeDurationMs:
		return true, 0
	default:
		return false, positionMs
	}
}

// PlaybackEvent is which report a client sends: playback started, moved on, or stopped.
type PlaybackEvent int

const (
	playbackStarted PlaybackEvent = iota
	playbackProgressed
	playbackStopped
)

// reportPlayback records a report in Plex. A song counts as played when it
// starts and keeps no resume point, as Jellyfin has it; anything else follows
// PlayState: progress moves the resume point, a stop may also mark it watched.
func (s *Server) reportPlayback(event PlaybackEvent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var report PlaybackReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m, err := s.plexItem(report.ItemId)
		if err != nil {
			respond(w, "", err)
			return
		}
		durationMs := m.Duration
		if part, ok := findPart(m, report.MediaSourceId); ok && part.Duration > 0 {
			durationMs = part.Duration
		}
		played, resumeMs := PlayState(report.PositionTicks/ticksPerMillisecond, durationMs)
		switch {
		case m.Type == "track" && event == playbackStarted:
			err = s.plex.SetPlayed(m.RatingKey, true)
		case m.Type == "track":
		case resumeMs > 0:
			err = s.plex.SetProgress(m.RatingKey, resumeMs)
		case event != playbackStopped:
		case played:
			err = s.plex.SetPlayed(m.RatingKey, true)
		case m.ViewCount == 0 && m.ViewOffset > 0:
			// Plex drops a resume point only by unscrobbling; harmless when unwatched.
			err = s.plex.SetPlayed(m.RatingKey, false)
		}
		if err == nil {
			err = s.rememberTracks(m, report)
		}
		if err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// rememberTracks saves the tracks a report names on the file, as Plex apps
// do, when they differ from Plex's choice.
func (s *Server) rememberTracks(m PlexMetadata, report PlaybackReport) error {
	part, ok := findPart(m, report.MediaSourceId)
	if !ok {
		return nil
	}
	var audioID, subtitleID *int
	if report.AudioStreamIndex != nil {
		if stream, ok := StreamAt(part.Stream, *report.AudioStreamIndex); ok && stream.StreamType == plexAudio && !stream.Selected {
			audioID = &stream.ID
		}
	}
	if index := report.SubtitleStreamIndex; index != nil {
		stream, ok := StreamAt(part.Stream, *index)
		switch {
		case *index < 0 && slices.ContainsFunc(part.Stream, func(s PlexStream) bool { return s.StreamType == plexSubtitle && s.Selected }):
			subtitleID = intPointer(noSubtitle)
		case ok && stream.StreamType == plexSubtitle && !stream.Selected:
			subtitleID = &stream.ID
		}
	}
	if audioID == nil && subtitleID == nil {
		return nil
	}
	return s.plex.SelectStreams(part.ID, audioID, subtitleID)
}

func (s *Server) markPlayed(played bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, err := s.plexItem(r.PathValue("id"))
		if err == nil {
			err = s.plex.SetPlayed(m.RatingKey, played)
		}
		if err != nil {
			respond(w, UserData{}, err)
			return
		}
		item, err := s.lookup(r.PathValue("id"))
		respond(w, item.UserData, err)
	}
}

// favorite stores a favorite as Plex's top rating; unmarking clears only
// that rating, so stars given in Plex survive.
func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	m, err := s.plexItem(r.PathValue("id"))
	switch {
	case err != nil:
	case r.Method == http.MethodPost:
		err = s.plex.Rate(m.RatingKey, lovedRating)
	case m.UserRating >= lovedRating:
		err = s.plex.Rate(m.RatingKey, clearedRating)
	}
	if err != nil {
		respond(w, UserData{}, err)
		return
	}
	item, err := s.lookup(r.PathValue("id"))
	respond(w, item.UserData, err)
}
