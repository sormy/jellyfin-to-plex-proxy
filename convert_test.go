package main

import (
	"slices"
	"strconv"
	"testing"
)

func TestIDRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		kind IDKind
		key  string
	}{{KindItem, "1"}, {KindLibrary, "12"}, {KindPart, "18446744073709551615"}} {
		id := EncodeID(tc.kind, tc.key)
		kind, key, ok := DecodeID(id)
		if len(id) != idLength || !ok || kind != tc.kind || key != tc.key {
			t.Errorf("%c %s: encoded %q, decoded %c %q %v", tc.kind, tc.key, id, kind, key, ok)
		}
	}
}

func TestDecodeIDRejects(t *testing.T) {
	for _, id := range []string{"", "123", "0zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "000000000000000000000000000000001"} {
		if _, _, ok := DecodeID(id); ok {
			t.Errorf("%q decoded", id)
		}
	}
}

func TestEncodeIDRejectsNonNumericKey(t *testing.T) {
	if id := EncodeID(KindItem, "abc"); id != "" {
		t.Errorf("got %q", id)
	}
}

func TestDates(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{FormatDay("2024-05-01"), "2024-05-01T00:00:00.000Z"},
		{FormatDay("bad"), ""},
		{FormatUnix(1714521600), "2024-05-01T00:00:00.000Z"},
		{FormatUnix(0), ""},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestToItemSkipsUnknownTypes(t *testing.T) {
	if _, ok := ToItem("s", PlexMetadata{RatingKey: "1", Type: "photo"}); ok {
		t.Error("photo converted")
	}
}

func TestEpisode(t *testing.T) {
	item, ok := ToItem("server", PlexMetadata{
		RatingKey: "30", ParentRatingKey: "20", GrandparentRatingKey: "10", Type: "episode",
		Title: "Pilot", Index: 1, ParentIndex: 0, Duration: 1500, ViewOffset: 500,
		Thumb: "/library/metadata/30/thumb/111", GrandparentThumb: "/library/metadata/10/thumb/222",
		GrandparentArt: "/library/metadata/10/art/333",
	})
	switch {
	case !ok, item.Type != "Episode", item.MediaType != "Video":
		t.Fatalf("got %+v", item)
	case item.SeriesId != EncodeID(KindItem, "10"), item.SeasonId != EncodeID(KindItem, "20"):
		t.Errorf("parents %q %q", item.SeriesId, item.SeasonId)
	case item.ParentIndexNumber == nil || *item.ParentIndexNumber != 0:
		t.Error("season 0 dropped")
	case item.ImageTags["Primary"] != "111", item.SeriesPrimaryImageTag != "222", item.ParentBackdropImageTags[0] != "333":
		t.Errorf("images %+v", item)
	case item.RunTimeTicks != 15_000_000, item.UserData.PlaybackPositionTicks != 5_000_000:
		t.Errorf("ticks %d %d", item.RunTimeTicks, item.UserData.PlaybackPositionTicks)
	case item.UserData.Key != "30", item.UserData.Played:
		t.Errorf("user data %+v", item.UserData)
	}
}

func TestShowPlayedState(t *testing.T) {
	for _, tc := range []struct {
		leaves, viewed int
		played         bool
	}{{3, 3, true}, {3, 1, false}, {0, 0, false}} {
		item, _ := ToItem("s", PlexMetadata{RatingKey: "1", Type: "show", LeafCount: tc.leaves, ViewedLeafCount: tc.viewed})
		if item.UserData.Played != tc.played || *item.UserData.UnplayedItemCount != tc.leaves-tc.viewed {
			t.Errorf("%d/%d: %+v", tc.viewed, tc.leaves, item.UserData)
		}
	}
}

func TestMediaStreamsIndexExternalSubtitlesAfterEmbedded(t *testing.T) {
	item, _ := ToItem("s", PlexMetadata{RatingKey: "5", Type: "movie", Media: []PlexMedia{{Part: []PlexPart{{
		ID: 7, Key: "/library/parts/7/1/file.mkv",
		Stream: []PlexStream{
			{StreamType: 1, Index: 0, Codec: "hevc"},
			{StreamType: 2, Index: 1, Codec: "eac3", Selected: true},
			{StreamType: 3, Key: "/library/streams/90", Codec: "srt", Selected: true},
			{StreamType: 3, Index: 2, Codec: "pgs"},
		},
	}}}}})
	source := item.MediaSources[0]
	var got []string
	for _, s := range source.MediaStreams {
		got = append(got, s.Type+":"+s.DeliveryMethod)
	}
	want := []string{"Video:", "Audio:", "Subtitle:Embed", "Subtitle:External"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	external := source.MediaStreams[3]
	wantURL := "/Videos/" + item.Id + "/" + source.Id + "/Subtitles/3/0/Stream.srt"
	if external.Index != 3 || external.DeliveryUrl != wantURL || !external.IsTextSubtitleStream {
		t.Errorf("external %+v", external)
	}
	if *source.DefaultAudioStreamIndex != 1 || *source.DefaultSubtitleStreamIndex != 3 {
		t.Errorf("defaults %d %d", *source.DefaultAudioStreamIndex, *source.DefaultSubtitleStreamIndex)
	}
}

func TestDatedEpisodes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		season, idx int
		airDate     string
		index       *int
		premiere    string
	}{
		{"numbered", 2, 5, "2020-01-01", intPointer(5), "2020-01-01T00:00:00.000Z"},
		{"dated by Plex", 2023, 10601, "2023-01-06", nil, "2023-01-06T00:00:00.000Z"},
		{"dated by its number", 2023, 122501, "", nil, "2023-12-25T00:00:00.000Z"},
		{"undecodable number", 2023, 7, "", nil, ""},
	} {
		item, _ := ToItem("s", PlexMetadata{
			RatingKey: "9", Type: "episode", ParentIndex: tc.season, Index: tc.idx, OriginallyAvailableAt: tc.airDate,
		})
		sameIndex := (item.IndexNumber == nil) == (tc.index == nil) && (tc.index == nil || *item.IndexNumber == *tc.index)
		if !sameIndex || item.PremiereDate != tc.premiere || *item.ParentIndexNumber != tc.season {
			t.Errorf("%s: index %v, premiere %q", tc.name, item.IndexNumber, item.PremiereDate)
		}
	}
}

func TestVersionsBestFirstAndNamed(t *testing.T) {
	version := func(id, height, bitrate int, file string) PlexMedia {
		return PlexMedia{Height: height, Bitrate: bitrate, VideoResolution: strconv.Itoa(height), VideoCodec: "h264",
			Part: []PlexPart{{ID: id, File: "/media/" + file}}}
	}
	item, _ := ToItem("s", PlexMetadata{RatingKey: "1", Type: "movie", Media: []PlexMedia{
		version(1, 1080, 2500, "Short.mkv"),
		version(2, 720, 9000, "Small.mkv"),
		version(3, 1080, 6500, "Episode.mkv"),
	}})
	var names []string
	for _, s := range item.MediaSources {
		names = append(names, s.Name)
	}
	want := []string{"1080p H264 · Episode", "1080p H264 · Short", "720p H264"}
	if !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestDuplicateTrackTitlesNumbered(t *testing.T) {
	item, _ := ToItem("s", PlexMetadata{RatingKey: "1", Type: "movie", Media: []PlexMedia{{Part: []PlexPart{{ID: 1, Stream: []PlexStream{
		{StreamType: 2, Index: 1, ExtendedDisplayTitle: "Surround 5.1 (Русский DTS)"},
		{StreamType: 2, Index: 2, ExtendedDisplayTitle: "Surround 5.1 (Русский DTS)"},
		{StreamType: 2, Index: 3, ExtendedDisplayTitle: "Stereo (English AAC)"},
		{StreamType: 3, Index: 4, ExtendedDisplayTitle: "Surround 5.1 (Русский DTS)"},
	}}}}}})
	var titles []string
	for _, s := range item.MediaStreams {
		titles = append(titles, s.DisplayTitle)
	}
	want := []string{"1. Surround 5.1 (Русский DTS)", "2. Surround 5.1 (Русский DTS)", "3. Stereo (English AAC)", "Surround 5.1 (Русский DTS)"}
	if !slices.Equal(titles, want) {
		t.Errorf("got %v, want %v", titles, want)
	}
}

func TestFavoriteIsTopRating(t *testing.T) {
	for _, tc := range []struct {
		rating   float64
		favorite bool
	}{{0, false}, {6, false}, {10, true}} {
		item, _ := ToItem("s", PlexMetadata{RatingKey: "1", Type: "track", UserRating: tc.rating})
		if item.UserData.IsFavorite != tc.favorite {
			t.Errorf("rating %v: favorite %v", tc.rating, item.UserData.IsFavorite)
		}
	}
}
