package main

import (
	"net/url"
	"testing"
)

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
