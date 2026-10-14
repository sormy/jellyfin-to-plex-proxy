package main

import "testing"

func TestVideoRange(t *testing.T) {
	dovi := func(profile, compat int, trc string) PlexStream {
		return PlexStream{DOVIPresent: true, DOVIProfile: profile, DOVIBLCompatID: compat, ColorTrc: trc}
	}
	for _, tc := range []struct {
		name                     string
		stream                   PlexStream
		wantRange, wantRangeType string
	}{
		{"profile 8 over HDR10", dovi(8, 1, "smpte2084"), "HDR", "DOVIWithHDR10"},
		{"profile 8 over SDR", dovi(8, 2, ""), "HDR", "DOVIWithSDR"},
		{"profile 8 over HLG", dovi(8, 4, "arib-std-b67"), "HDR", "DOVIWithHLG"},
		{"profile 7 with enhancement layer", dovi(7, 6, "smpte2084"), "HDR", "DOVIWithEL"},
		{"profile 5, no fallback", dovi(5, 0, ""), "HDR", "DOVI"},
		{"profile 10 over HDR10", dovi(10, 1, "smpte2084"), "HDR", "DOVIWithHDR10"},
		{"unnamed Dolby Vision falls back to its transfer", dovi(8, 6, "smpte2084"), "HDR", "HDR10"},
		{"HDR10", PlexStream{ColorTrc: "smpte2084"}, "HDR", "HDR10"},
		{"HLG", PlexStream{ColorTrc: "arib-std-b67"}, "HDR", "HLG"},
		{"SDR", PlexStream{ColorTrc: "bt709"}, "SDR", "SDR"},
		{"no color data", PlexStream{}, "SDR", "SDR"},
	} {
		videoRange, rangeType := VideoRange(tc.stream)
		if videoRange != tc.wantRange || rangeType != tc.wantRangeType {
			t.Errorf("%s: %s %s, want %s %s", tc.name, videoRange, rangeType, tc.wantRange, tc.wantRangeType)
		}
	}
}

func TestMediaStreamsCarryColorAndDolbyVision(t *testing.T) {
	item, _ := ToItem("s", PlexMetadata{RatingKey: "5", Type: "movie", Media: []PlexMedia{{Part: []PlexPart{{
		ID: 7, Key: "/library/parts/7/1/file.mkv",
		Stream: []PlexStream{
			{
				StreamType: 1, Index: 0, Codec: "hevc", ColorPrimaries: "bt2020", ColorSpace: "bt2020nc", ColorTrc: "smpte2084",
				DOVIPresent: true, DOVIVersion: "1.0", DOVIProfile: 5, DOVILevel: 6, DOVIBLCompatID: 0,
				DOVIRPUPresent: true, DOVIBLPresent: true,
			},
			{StreamType: 2, Index: 1, Codec: "truehd", AudioChannelLayout: "7.1"},
		},
	}}}}})
	streams := item.MediaSources[0].MediaStreams
	video, audio := streams[0], streams[1]
	if video.VideoRange != "HDR" || video.VideoRangeType != "DOVI" || video.ColorTransfer != "smpte2084" ||
		video.ColorPrimaries != "bt2020" || video.ColorSpace != "bt2020nc" {
		t.Errorf("video color %+v", video)
	}
	// Zeros are meaningful: profile 5 has no fallback layer and no enhancement layer.
	got := map[string]*int{
		"DvVersionMajor": video.DvVersionMajor, "DvVersionMinor": video.DvVersionMinor, "DvProfile": video.DvProfile,
		"DvLevel": video.DvLevel, "DvBlSignalCompatibilityId": video.DvBlSignalCompatibilityId,
		"RpuPresentFlag": video.RpuPresentFlag, "ElPresentFlag": video.ElPresentFlag, "BlPresentFlag": video.BlPresentFlag,
	}
	want := map[string]int{
		"DvVersionMajor": 1, "DvVersionMinor": 0, "DvProfile": 5, "DvLevel": 6,
		"DvBlSignalCompatibilityId": 0, "RpuPresentFlag": 1, "ElPresentFlag": 0, "BlPresentFlag": 1,
	}
	for name, value := range want {
		if got[name] == nil || *got[name] != value {
			t.Errorf("%s = %v, want %d", name, got[name], value)
		}
	}
	if audio.ChannelLayout != "7.1" || audio.VideoRange != "" || audio.DvProfile != nil {
		t.Errorf("audio %+v", audio)
	}
}

func TestStreamsWithoutDolbyVisionOmitItsRecord(t *testing.T) {
	item, _ := ToItem("s", PlexMetadata{RatingKey: "5", Type: "movie", Media: []PlexMedia{{Part: []PlexPart{{
		ID: 7, Stream: []PlexStream{{StreamType: 1, Index: 0, Codec: "hevc", ColorTrc: "smpte2084"}},
	}}}}})
	video := item.MediaSources[0].MediaStreams[0]
	if video.VideoRangeType != "HDR10" || video.DvProfile != nil || video.RpuPresentFlag != nil {
		t.Errorf("HDR10 stream %+v", video)
	}
}
