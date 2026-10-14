package main

import (
	"strconv"
	"strings"
)

// doviRangeTypes names a Dolby Vision stream as Jellyfin does: by profile,
// then by the format its base layer falls back to.
var doviRangeTypes = map[int]map[int]string{
	5:  {0: "DOVI"},
	7:  {6: "DOVIWithEL"},
	8:  {1: "DOVIWithHDR10", 2: "DOVIWithSDR", 4: "DOVIWithHLG"},
	10: {0: "DOVI", 1: "DOVIWithHDR10", 2: "DOVIWithSDR", 4: "DOVIWithHLG"},
}

// transferRangeTypes names an HDR stream by its transfer function.
var transferRangeTypes = map[string]string{
	"smpte2084":    "HDR10",
	"arib-std-b67": "HLG",
}

const sdr = "SDR"

// VideoRange returns Jellyfin's VideoRange and VideoRangeType for a video stream.
func VideoRange(s PlexStream) (videoRange, rangeType string) {
	rangeType = sdr
	if named, ok := doviRangeTypes[s.DOVIProfile][s.DOVIBLCompatID]; s.DOVIPresent && ok {
		rangeType = named
	} else if named, ok := transferRangeTypes[s.ColorTrc]; ok {
		rangeType = named
	}
	if rangeType == sdr {
		return sdr, sdr
	}
	return "HDR", rangeType
}

// setDolbyVision copies a Dolby Vision stream's configuration record.
func setDolbyVision(stream *MediaStream, s PlexStream) {
	if !s.DOVIPresent {
		return
	}
	major, minor, _ := strings.Cut(s.DOVIVersion, ".")
	stream.DvVersionMajor = atoiPointer(major)
	stream.DvVersionMinor = atoiPointer(minor)
	stream.DvProfile = intPointer(s.DOVIProfile)
	stream.DvLevel = intPointer(s.DOVILevel)
	stream.DvBlSignalCompatibilityId = intPointer(s.DOVIBLCompatID)
	stream.RpuPresentFlag = flag(s.DOVIRPUPresent)
	stream.ElPresentFlag = flag(s.DOVIELPresent)
	stream.BlPresentFlag = flag(s.DOVIBLPresent)
}

func atoiPointer(s string) *int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

// flag is Jellyfin's 0 or 1 for a present layer.
func flag(present bool) *int {
	if present {
		return intPointer(1)
	}
	return intPointer(0)
}
