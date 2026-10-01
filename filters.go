package main

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// QueryFiltersLegacy and QueryFilters are what a library offers to filter
// by, as Jellyfin's two filter endpoints shape them.
type QueryFiltersLegacy struct {
	Genres          []string `json:"Genres"`
	Tags            []string `json:"Tags"`
	OfficialRatings []string `json:"OfficialRatings"`
	Years           []int    `json:"Years"`
}

type QueryFilters struct {
	Genres            []NameID    `json:"Genres"`
	Tags              []string    `json:"Tags"`
	AudioLanguages    []NameValue `json:"AudioLanguages"`
	SubtitleLanguages []NameValue `json:"SubtitleLanguages"`
}

type NameValue struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

// videoKinds are the sections whose items carry audio and subtitle languages.
var videoKinds = []string{"movie", "show"}

// filterValues gathers a field's values across the libraries a query names,
// of the given kinds of section, every kind when none is given.
func (s *Server) filterValues(q url.Values, field string, kinds ...string) ([]PlexDirectory, error) {
	sections, err := s.plex.Sections()
	if err != nil {
		return nil, err
	}
	if kind, key, ok := DecodeID(q.Get("parentid")); ok && kind == KindLibrary {
		sections = slices.DeleteFunc(sections, func(d PlexDirectory) bool { return d.Key != key })
	}
	types := plexTypes(q)
	var values []PlexDirectory
	for _, section := range sections {
		plexType, ok := genreTypes[section.Type]
		if !ok || len(kinds) > 0 && !slices.Contains(kinds, section.Type) {
			continue
		}
		if held := slices.DeleteFunc(slices.Clone(types), func(t string) bool { return !slices.Contains(sectionHolds[section.Type], t) }); len(held) > 0 {
			plexType = plexTypeNumbers[held[0]]
		}
		found, err := s.plex.SectionValues(section.Key, field, plexType)
		if err != nil {
			return nil, err
		}
		for _, v := range found {
			if !slices.ContainsFunc(values, func(o PlexDirectory) bool { return o.Key == v.Key }) {
				values = append(values, v)
			}
		}
	}
	return values, nil
}

func titles(values []PlexDirectory) []string {
	names := []string{}
	for _, v := range values {
		names = append(names, v.Title)
	}
	return names
}

func (s *Server) filtersLegacy(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	genres, err := s.filterValues(q, "genre")
	if err != nil {
		fail(w, err)
		return
	}
	ratings, err := s.filterValues(q, "contentRating")
	if err != nil {
		fail(w, err)
		return
	}
	years, err := s.filterValues(q, "year")
	if err != nil {
		fail(w, err)
		return
	}
	result := QueryFiltersLegacy{Genres: titles(genres), Tags: []string{}, OfficialRatings: titles(ratings), Years: []int{}}
	for _, y := range years {
		if year, err := strconv.Atoi(y.Key); err == nil {
			result.Years = append(result.Years, year)
		}
	}
	writeJSON(w, result)
}

func (s *Server) filters(w http.ResponseWriter, r *http.Request) {
	q := query(r)
	genres, err := s.filterValues(q, "genre")
	if err != nil {
		fail(w, err)
		return
	}
	audio, err := s.filterValues(q, "audioLanguage", videoKinds...)
	if err != nil {
		fail(w, err)
		return
	}
	subtitles, err := s.filterValues(q, "subtitleLanguage", videoKinds...)
	if err != nil {
		fail(w, err)
		return
	}
	result := QueryFilters{Genres: []NameID{}, Tags: []string{}, AudioLanguages: languages(audio), SubtitleLanguages: languages(subtitles)}
	for _, g := range genres {
		result.Genres = append(result.Genres, NameID{Name: g.Title, Id: EncodeID(KindGenre, g.Key)})
	}
	writeJSON(w, result)
}

// languages pairs each language's name with Plex's code, which a client
// sends back to filter by and the proxy hands straight to Plex.
func languages(values []PlexDirectory) []NameValue {
	pairs := []NameValue{}
	for _, v := range values {
		pairs = append(pairs, NameValue{Name: v.Title, Value: v.Key})
	}
	return pairs
}

// firstCharacter is Plex's bucket for a letter filter: a letter, or "#" for
// titles that start with anything else, which clients ask as "before A".
func firstCharacter(q url.Values) string {
	if letter := q.Get("namestartswith"); letter != "" {
		return strings.ToUpper(letter[:1])
	}
	if strings.EqualFold(q.Get("namelessthan"), "A") {
		return "#"
	}
	return ""
}

// valuesOf reads a list parameter, whose values Jellyfin may join with "|".
func valuesOf(q url.Values, key string) []string {
	var values []string
	for _, v := range q[key] {
		values = append(values, strings.Split(v, "|")...)
	}
	return slices.DeleteFunc(values, func(v string) bool { return v == "" })
}

// genreKeys turns genre names, as Swiftfin filters by, into Plex's keys.
func (s *Server) genreKeys(q url.Values) ([]string, error) {
	names := valuesOf(q, "genres")
	if len(names) == 0 {
		return nil, nil
	}
	genres, err := s.filterValues(q, "genre")
	var keys []string
	for _, g := range genres {
		if slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, g.Title) }) {
			keys = append(keys, g.Key)
		}
	}
	return keys, err
}
