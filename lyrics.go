package main

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// lyricStreamType is Plex's stream type for lyrics, from LyricFind or a sidecar file.
const lyricStreamType = 4

type Lyrics struct {
	Metadata LyricMetadata `json:"Metadata"`
	Lyrics   []LyricLine   `json:"Lyrics"`
}

type LyricMetadata struct {
	IsSynced bool `json:"IsSynced"`
}

type LyricLine struct {
	Text  string `json:"Text"`
	Start *int64 `json:"Start,omitempty"`
}

func lyricStream(streams []PlexStream) (PlexStream, bool) {
	i := slices.IndexFunc(streams, func(s PlexStream) bool { return s.StreamType == lyricStreamType && s.Key != "" })
	if i < 0 {
		return PlexStream{}, false
	}
	return streams[i], true
}

func (s *Server) lyrics(w http.ResponseWriter, r *http.Request) {
	part, err := s.part(r.PathValue("id"), query(r).Get("mediasourceid"))
	if err != nil {
		respond(w, "", err)
		return
	}
	stream, ok := lyricStream(part.Stream)
	if !ok {
		respond(w, "", errNotFound)
		return
	}
	text, err := s.plex.Text(stream.Key)
	respond(w, parseLyrics(text), err)
}

// parseLyrics times each LRC line by its [mm:ss.xx] tags, skipping other
// tags; text without a timed line reads as unsynced, line by line.
func parseLyrics(text string) Lyrics {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	var synced []LyricLine
	for _, line := range lines {
		starts, words := lrcStarts(line)
		for _, start := range starts {
			synced = append(synced, LyricLine{Text: words, Start: &start})
		}
	}
	if len(synced) == 0 {
		plain := make([]LyricLine, 0, len(lines))
		for _, line := range lines {
			plain = append(plain, LyricLine{Text: line})
		}
		return Lyrics{Lyrics: plain}
	}
	slices.SortStableFunc(synced, func(a, b LyricLine) int { return cmp.Compare(*a.Start, *b.Start) })
	return Lyrics{Metadata: LyricMetadata{IsSynced: true}, Lyrics: synced}
}

// lrcStarts reads a line's leading time tags, in ticks, and the words after them.
func lrcStarts(line string) ([]int64, string) {
	var starts []int64
	for strings.HasPrefix(line, "[") {
		tag, rest, ok := strings.Cut(line[1:], "]")
		minutes, seconds, isTime := strings.Cut(tag, ":")
		m, minutesErr := strconv.Atoi(minutes)
		sec, secondsErr := strconv.ParseFloat(seconds, 64)
		if !ok || !isTime || minutesErr != nil || secondsErr != nil {
			break
		}
		starts = append(starts, int64((float64(m)*60+sec)*1000)*ticksPerMillisecond)
		line = rest
	}
	return starts, strings.TrimSpace(line)
}
