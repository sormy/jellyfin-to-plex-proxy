package main

import (
	"cmp"
	"net/url"
	"slices"
	"strconv"
	"testing"
)

func TestPlexQuerySorts(t *testing.T) {
	for _, tc := range []struct {
		sortBy, sortOrder, plexType, want string
	}{
		{"SortName", "Ascending", "movie", "titleSort"},
		{"Name", "Descending", "show", "titleSort:desc"},
		{"DateCreated", "Descending", "movie", "addedAt:desc"},
		{"DateLastContentAdded", "Descending", "show", "episode.addedAt:desc"},
		{"DateLastContentAdded", "Descending", "movie", "addedAt:desc"},
		{"PremiereDate", "Ascending", "show", "originallyAvailableAt"},
		{"ProductionYear", "Ascending", "movie", "year"},
		{"CommunityRating", "Descending", "movie", "audienceRating:desc"},
		{"CriticRating", "Descending", "show", "rating:desc"},
		{"OfficialRating", "Ascending", "movie", "contentRating"},
		{"DatePlayed", "Descending", "movie", "lastViewedAt:desc"},
		{"PlayCount", "Descending", "movie", "viewCount:desc"},
		{"Runtime", "Ascending", "movie", "duration"},
		{"Studio", "Ascending", "show", "studio"},
		{"VideoBitRate", "Descending", "movie", "mediaBitrate:desc"},
		{"Random", "Ascending", "show", "random"},
		{"IsFavoriteOrLiked", "Ascending", "movie", ""},
	} {
		q := url.Values{"sortby": {tc.sortBy}, "sortorder": {tc.sortOrder}}
		if got := plexQuery(q, tc.plexType).Get("sort"); got != tc.want {
			t.Errorf("%s %s on %s: %q, want %q", tc.sortBy, tc.sortOrder, tc.plexType, got, tc.want)
		}
	}
}

func TestPlayState(t *testing.T) {
	const episodeMs, shortMs = 1_200_000, 120_000
	for _, tc := range []struct {
		name                   string
		positionMs, durationMs int64
		played                 bool
		resumeMs               int64
	}{
		{"not started", 0, episodeMs, false, 0},
		{"under 5%", 50_000, episodeMs, false, 0},
		{"first minute of a long item", 60_000, 3 * episodeMs, false, 0},
		{"just past the first minute", 61_000, 1_000_000, false, 61_000},
		{"midway", 600_000, episodeMs, false, 600_000},
		{"at 90%", 1_080_000, episodeMs, false, 1_080_000},
		{"past 90%", 1_090_000, episodeMs, true, 0},
		{"past the end", 1_300_000, episodeMs, true, 0},
		{"short item midway", 90_000, shortMs, true, 0},
		{"short item first minute", 30_000, shortMs, false, 0},
		{"unknown runtime", 60_000, 0, true, 0},
	} {
		played, resumeMs := PlayState(tc.positionMs, tc.durationMs)
		if played != tc.played || resumeMs != tc.resumeMs {
			t.Errorf("%s: played %v resume %d, want %v %d", tc.name, played, resumeMs, tc.played, tc.resumeMs)
		}
	}
}

func TestImageBox(t *testing.T) {
	for _, tc := range []struct {
		query         string
		width, height int
	}{
		{"", defaultImageSize, defaultImageSize},
		{"maxWidth=300", 300, maxImageSize},
		{"maxHeight=450", maxImageSize, 450},
		{"fillWidth=640&fillHeight=360", 640, 360},
		{"maxWidth=300&width=500", 300, maxImageSize},
	} {
		q, _ := url.ParseQuery(tc.query)
		width, height := imageBox(lowercaseKeys(q))
		if width != tc.width || height != tc.height {
			t.Errorf("%q: %dx%d, want %dx%d", tc.query, width, height, tc.width, tc.height)
		}
	}
}

func TestSortMergedRandomShuffles(t *testing.T) {
	const items, runs = 20, 20
	var titles []PlexMetadata
	for i := range items {
		titles = append(titles, PlexMetadata{Title: strconv.Itoa(100 + i)})
	}
	byTitle := func(a, b PlexMetadata) int { return cmp.Compare(a.Title, b.Title) }
	for range runs {
		merged := slices.Clone(titles)
		sortMerged(merged, "random")
		if !slices.IsSortedFunc(merged, byTitle) {
			slices.SortFunc(merged, byTitle)
			if !slices.EqualFunc(merged, titles, func(a, b PlexMetadata) bool { return a.Title == b.Title }) {
				t.Fatalf("shuffle lost or duplicated items: %v", merged)
			}
			return
		}
	}
	t.Errorf("random merge came out in title order %d times in a row", runs)
}
