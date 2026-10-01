package main

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
)

const (
	posterAspectRatio    = 2.0 / 3.0
	squareAspectRatio    = 1.0
	landscapeAspectRatio = 16.0 / 9.0
	bitsPerKilobit       = 1000
)

var itemTypes = map[string]string{
	"movie":      "Movie",
	"show":       "Series",
	"season":     "Season",
	"episode":    "Episode",
	"collection": "BoxSet",
	"artist":     "MusicArtist",
	"album":      "MusicAlbum",
	"track":      "Audio",
	"playlist":   "Playlist",
}

var streamTypes = map[int]string{1: "Video", 2: "Audio", 3: "Subtitle"}

var textSubtitleCodecs = map[string]bool{
	"srt": true, "subrip": true, "ass": true, "ssa": true, "vtt": true, "webvtt": true, "mov_text": true,
}

// imageTag names an image version: Plex ends artwork paths with an update timestamp.
func imageTag(plexPath string) string {
	if plexPath == "" {
		return ""
	}
	return path.Base(plexPath)
}

// Favorites are Plex's top rating, which Plexamp shows as loved.
const (
	lovedRating   = 10
	clearedRating = -1
)

// Plex numbers the episodes of a season named by year as MMDDNN.
const (
	firstYearSeason     = 1000
	datedEpisodeMonthOf = 10_000
	datedEpisodeDayOf   = 100
)

func datedEpisodeDay(year, index int) string {
	month, day := index/datedEpisodeMonthOf, index/datedEpisodeDayOf%datedEpisodeDayOf
	return FormatDay(fmt.Sprintf("%04d-%02d-%02d", year, month, day))
}

func intPointer(n int) *int {
	return &n
}

func ToItem(serverID string, m PlexMetadata) (Item, bool) {
	kind, ok := itemTypes[m.Type]
	if !ok {
		return Item{}, false
	}
	item := Item{
		Id:              EncodeID(KindItem, m.RatingKey),
		ServerId:        serverID,
		Name:            m.Title,
		SortName:        m.TitleSort,
		Type:            kind,
		LocationType:    "FileSystem",
		Overview:        m.Summary,
		OfficialRating:  m.ContentRating,
		CommunityRating: m.AudienceRating,
		ProductionYear:  m.Year,
		PremiereDate:    FormatDay(m.OriginallyAvailableAt),
		DateCreated:     FormatUnix(m.AddedAt),
		RunTimeTicks:    m.Duration * ticksPerMillisecond,
		UserData:        toUserData(m),
	}
	if m.Tagline != "" {
		item.Taglines = []string{m.Tagline}
	}
	for _, genre := range m.Genre {
		item.Genres = append(item.Genres, genre.Tag)
	}
	if tag := imageTag(m.Thumb); tag != "" {
		item.ImageTags = map[string]string{"Primary": tag}
	}
	if tag := imageTag(m.Art); tag != "" {
		item.BackdropImageTags = []string{tag}
	}
	if m.LibrarySectionID > 0 {
		item.ParentId = EncodeID(KindLibrary, strconv.Itoa(m.LibrarySectionID))
	}
	switch m.Type {
	case "movie":
		item.MediaType = "Video"
		item.PrimaryImageAspectRatio = posterAspectRatio
	case "artist":
		item.IsFolder = true
		item.PrimaryImageAspectRatio = squareAspectRatio
	case "album":
		artist := NameID{Name: m.ParentTitle, Id: EncodeID(KindItem, m.ParentRatingKey)}
		item.IsFolder = true
		item.ChildCount = m.LeafCount
		item.ParentId = artist.Id
		item.AlbumArtist = artist.Name
		item.AlbumArtists = []NameID{artist}
		item.ArtistItems = []NameID{artist}
		item.Artists = []string{artist.Name}
		item.PrimaryImageAspectRatio = squareAspectRatio
	case "track":
		// Plex holds a track's own performer apart from the album's artist.
		artist := NameID{Name: m.GrandparentTitle, Id: EncodeID(KindItem, m.GrandparentRatingKey)}
		performer := NameID{Name: cmp.Or(m.OriginalTitle, artist.Name), Id: artist.Id}
		item.MediaType = "Audio"
		item.IndexNumber = intPointer(m.Index)
		item.ParentIndexNumber = intPointer(m.ParentIndex)
		item.AlbumId = EncodeID(KindItem, m.ParentRatingKey)
		item.ParentId = item.AlbumId
		item.ParentPrimaryImageItemId = item.AlbumId
		item.Album = m.ParentTitle
		item.AlbumPrimaryImageTag = imageTag(m.ParentThumb)
		item.AlbumArtist = artist.Name
		item.AlbumArtists = []NameID{artist}
		item.ArtistItems = []NameID{performer}
		item.Artists = []string{performer.Name}
		item.ProductionYear = cmp.Or(m.Year, m.ParentYear)
		item.PrimaryImageAspectRatio = squareAspectRatio
	case "playlist":
		item.Id = EncodeID(KindPlaylist, m.RatingKey)
		item.IsFolder = true
		item.ChildCount = m.LeafCount
		item.MediaType = map[bool]string{true: "Audio", false: "Video"}[m.PlaylistType == "audio"]
		if tag := imageTag(m.Composite); tag != "" {
			item.ImageTags = map[string]string{"Primary": tag}
		}
		item.PrimaryImageAspectRatio = squareAspectRatio
	case "collection":
		item.IsFolder = true
		item.ChildCount = m.ChildCount
		item.PrimaryImageAspectRatio = posterAspectRatio
	case "show":
		item.IsFolder = true
		item.ChildCount = m.ChildCount
		item.RecursiveItemCount = m.LeafCount
		item.PrimaryImageAspectRatio = posterAspectRatio
	case "season":
		item.IsFolder = true
		item.ChildCount = m.LeafCount
		item.RecursiveItemCount = m.LeafCount
		item.IndexNumber = intPointer(m.Index)
		item.SeriesId = EncodeID(KindItem, m.ParentRatingKey)
		item.SeriesName = m.ParentTitle
		item.ParentId = item.SeriesId
		item.PrimaryImageAspectRatio = posterAspectRatio
	case "episode":
		item.MediaType = "Video"
		item.IndexNumber = intPointer(m.Index)
		item.ParentIndexNumber = intPointer(m.ParentIndex)
		if m.ParentIndex >= firstYearSeason {
			// Jellyfin files dated episodes by air date, not by number.
			item.IndexNumber = nil
			item.PremiereDate = cmp.Or(item.PremiereDate, datedEpisodeDay(m.ParentIndex, m.Index))
		}
		item.SeasonId = EncodeID(KindItem, m.ParentRatingKey)
		item.ParentId = item.SeasonId
		item.SeasonName = m.ParentTitle
		item.SeriesId = EncodeID(KindItem, m.GrandparentRatingKey)
		item.SeriesName = m.GrandparentTitle
		item.SeriesPrimaryImageTag = imageTag(m.GrandparentThumb)
		if tag := imageTag(m.GrandparentArt); tag != "" {
			item.ParentBackdropItemId = item.SeriesId
			item.ParentBackdropImageTags = []string{tag}
		}
		item.PrimaryImageAspectRatio = landscapeAspectRatio
	}
	item.MediaSources = toMediaSources(item.Id, m)
	if len(item.MediaSources) > 0 {
		item.MediaStreams = item.MediaSources[0].MediaStreams
		item.Path = item.MediaSources[0].Path
	}
	return item, true
}

func ToItems(serverID string, found []PlexMetadata) []Item {
	items := []Item{}
	for _, m := range found {
		if item, ok := ToItem(serverID, m); ok {
			items = append(items, item)
		}
	}
	return items
}

func toUserData(m PlexMetadata) *UserData {
	data := &UserData{
		IsFavorite:            m.UserRating >= lovedRating,
		Key:                   m.RatingKey,
		ItemId:                EncodeID(KindItem, m.RatingKey),
		PlaybackPositionTicks: m.ViewOffset * ticksPerMillisecond,
		PlayCount:             m.ViewCount,
		Played:                m.ViewCount > 0,
		LastPlayedDate:        FormatUnix(m.LastViewedAt),
	}
	if m.ViewOffset > 0 && m.Duration > 0 {
		data.PlayedPercentage = float64(m.ViewOffset) * 100 / float64(m.Duration)
	}
	if slices.Contains([]string{"show", "season", "artist", "album"}, m.Type) {
		unplayed := m.LeafCount - m.ViewedLeafCount
		data.UnplayedItemCount = &unplayed
		data.Played = m.LeafCount > 0 && unplayed == 0
	}
	return data
}

// resolutionLabels spells Plex's resolutions as players show them.
var resolutionLabels = map[string]string{"4k": "4K", "sd": "SD", "1080": "1080p", "720": "720p", "576": "576p", "480": "480p"}

// toMediaSources lists versions best first, as Plex picks at full quality,
// and names each by its file where two would read the same.
func toMediaSources(itemID string, m PlexMetadata) []MediaSource {
	media := slices.Clone(m.Media)
	slices.SortStableFunc(media, func(a, b PlexMedia) int {
		return cmp.Or(cmp.Compare(b.Height, a.Height), cmp.Compare(b.Bitrate, a.Bitrate))
	})
	var sources []MediaSource
	for _, version := range media {
		if len(version.Part) == 0 {
			continue
		}
		part := version.Part[0]
		sourceID := EncodeID(KindPart, strconv.Itoa(part.ID))
		streams, audio, subtitle := toMediaStreams(itemID, sourceID, part.Stream)
		resolution := cmp.Or(resolutionLabels[strings.ToLower(version.VideoResolution)], version.VideoResolution)
		sources = append(sources, MediaSource{
			Id:                         sourceID,
			ETag:                       sourceID,
			Name:                       strings.TrimSpace(resolution + " " + strings.ToUpper(version.VideoCodec)),
			Path:                       part.File,
			Protocol:                   "File",
			Type:                       "Default",
			Container:                  part.Container,
			Size:                       part.Size,
			Bitrate:                    version.Bitrate * bitsPerKilobit,
			RunTimeTicks:               version.Duration * ticksPerMillisecond,
			SupportsDirectPlay:         true,
			SupportsDirectStream:       true,
			SupportsProbing:            true,
			MediaStreams:               streams,
			DefaultAudioStreamIndex:    audio,
			DefaultSubtitleStreamIndex: subtitle,
		})
	}
	labels := map[string]int{}
	for _, source := range sources {
		labels[source.Name]++
	}
	for i, source := range sources {
		if labels[source.Name] > 1 {
			file := path.Base(source.Path)
			sources[i].Name += " · " + strings.TrimSuffix(file, path.Ext(file))
		}
	}
	return sources
}

// StreamAt finds the Plex stream behind a Jellyfin stream index.
func StreamAt(streams []PlexStream, index int) (PlexStream, bool) {
	for _, s := range streams {
		if s.Key == "" && s.Index == index {
			return s, true
		}
	}
	for position, s := range ExternalSubtitles(streams) {
		if ExternalSubtitleIndex(streams, position) == index {
			return s, true
		}
	}
	return PlexStream{}, false
}

// ExternalSubtitleIndex numbers external subtitles after every embedded
// stream, as Jellyfin does; clients map embedded tracks by order.
func ExternalSubtitleIndex(streams []PlexStream, position int) int {
	next := 0
	for _, s := range streams {
		if s.Key == "" && s.Index >= next {
			next = s.Index + 1
		}
	}
	return next + position
}

func ExternalSubtitles(streams []PlexStream) []PlexStream {
	var external []PlexStream
	for _, s := range streams {
		if s.StreamType == 3 && s.Key != "" {
			external = append(external, s)
		}
	}
	return external
}

// toMediaStreams never returns nil: Finamp fails on a null stream list, and
// Plex lists items without their streams.
func toMediaStreams(itemID, sourceID string, plexStreams []PlexStream) ([]MediaStream, *int, *int) {
	streams := []MediaStream{}
	var audio, subtitle *int
	add := func(s PlexStream, index int) {
		kind, ok := streamTypes[s.StreamType]
		if !ok {
			return
		}
		stream := MediaStream{
			Type:             kind,
			Index:            index,
			Codec:            s.Codec,
			Language:         s.LanguageCode,
			Title:            s.Title,
			DisplayTitle:     s.DisplayTitle,
			Profile:          s.Profile,
			IsDefault:        s.Default,
			IsForced:         s.Forced,
			Channels:         s.Channels,
			Width:            s.Width,
			Height:           s.Height,
			BitRate:          s.Bitrate * bitsPerKilobit,
			AverageFrameRate: s.FrameRate,
			SampleRate:       s.SamplingRate,
			BitDepth:         s.BitDepth,
		}
		if s.ExtendedDisplayTitle != "" {
			stream.DisplayTitle = s.ExtendedDisplayTitle
		}
		if kind == "Subtitle" {
			stream.IsTextSubtitleStream = textSubtitleCodecs[s.Codec]
			stream.DeliveryMethod = "Embed"
			if s.Key != "" {
				stream.IsExternal = true
				stream.DeliveryMethod = "External"
				stream.DeliveryUrl = fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.%s", itemID, sourceID, index, s.Codec)
			}
		}
		if s.Selected && kind == "Audio" {
			audio = intPointer(index)
		}
		if s.Selected && kind == "Subtitle" {
			subtitle = intPointer(index)
		}
		streams = append(streams, stream)
	}
	for _, s := range plexStreams {
		if s.Key == "" {
			add(s, s.Index)
		}
	}
	for position, s := range ExternalSubtitles(plexStreams) {
		add(s, ExternalSubtitleIndex(plexStreams, position))
	}
	numberDuplicateTitles(streams)
	return streams, audio, subtitle
}

// numberDuplicateTitles tells apart tracks that would read the same in a picker.
func numberDuplicateTitles(streams []MediaStream) {
	seen := map[string]int{}
	for _, s := range streams {
		seen[s.Type+s.DisplayTitle]++
	}
	occurrence := map[string]int{}
	for i, s := range streams {
		key := s.Type + s.DisplayTitle
		if seen[key] > 1 {
			occurrence[key]++
			streams[i].DisplayTitle = fmt.Sprintf("%s #%d", s.DisplayTitle, occurrence[key])
		}
	}
}
