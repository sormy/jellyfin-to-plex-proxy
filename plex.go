package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const plexLibraryIdentifier = "com.plexapp.plugins.library"

type PlexTag struct {
	Tag string `json:"tag"`
}

type PlexStream struct {
	ID                   int     `json:"id"`
	StreamType           int     `json:"streamType"`
	Index                int     `json:"index"`
	Key                  string  `json:"key"`
	Codec                string  `json:"codec"`
	LanguageCode         string  `json:"languageCode"`
	Title                string  `json:"title"`
	DisplayTitle         string  `json:"displayTitle"`
	ExtendedDisplayTitle string  `json:"extendedDisplayTitle"`
	Profile              string  `json:"profile"`
	Selected             bool    `json:"selected"`
	Default              bool    `json:"default"`
	Forced               bool    `json:"forced"`
	Channels             int     `json:"channels"`
	Width                int     `json:"width"`
	Height               int     `json:"height"`
	Bitrate              int     `json:"bitrate"`
	FrameRate            float64 `json:"frameRate"`
}

type PlexPart struct {
	ID        int          `json:"id"`
	Key       string       `json:"key"`
	File      string       `json:"file"`
	Size      int64        `json:"size"`
	Container string       `json:"container"`
	Duration  int64        `json:"duration"`
	Stream    []PlexStream `json:"Stream"`
}

type PlexMedia struct {
	Duration        int64      `json:"duration"`
	Bitrate         int        `json:"bitrate"`
	Height          int        `json:"height"`
	VideoCodec      string     `json:"videoCodec"`
	VideoResolution string     `json:"videoResolution"`
	Part            []PlexPart `json:"Part"`
}

type PlexMetadata struct {
	RatingKey             string      `json:"ratingKey"`
	Type                  string      `json:"type"`
	Title                 string      `json:"title"`
	TitleSort             string      `json:"titleSort"`
	Summary               string      `json:"summary"`
	Tagline               string      `json:"tagline"`
	Year                  int         `json:"year"`
	OriginallyAvailableAt string      `json:"originallyAvailableAt"`
	ContentRating         string      `json:"contentRating"`
	AudienceRating        float64     `json:"audienceRating"`
	Duration              int64       `json:"duration"`
	ViewOffset            int64       `json:"viewOffset"`
	ViewCount             int         `json:"viewCount"`
	LastViewedAt          int64       `json:"lastViewedAt"`
	AddedAt               int64       `json:"addedAt"`
	LeafCount             int         `json:"leafCount"`
	ViewedLeafCount       int         `json:"viewedLeafCount"`
	ChildCount            int         `json:"childCount"`
	Index                 int         `json:"index"`
	ParentIndex           int         `json:"parentIndex"`
	ParentRatingKey       string      `json:"parentRatingKey"`
	GrandparentRatingKey  string      `json:"grandparentRatingKey"`
	ParentTitle           string      `json:"parentTitle"`
	GrandparentTitle      string      `json:"grandparentTitle"`
	LibrarySectionID      int         `json:"librarySectionID"`
	Thumb                 string      `json:"thumb"`
	Art                   string      `json:"art"`
	GrandparentThumb      string      `json:"grandparentThumb"`
	GrandparentArt        string      `json:"grandparentArt"`
	Genre                 []PlexTag   `json:"Genre"`
	Media                 []PlexMedia `json:"Media"`
}

type PlexDirectory struct {
	Key      string         `json:"key"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Location []PlexLocation `json:"Location"`
}

type PlexLocation struct {
	Path string `json:"path"`
}

type PlexContainer struct {
	Size              int             `json:"size"`
	LibrarySectionID  int             `json:"librarySectionID"`
	TotalSize         int             `json:"totalSize"`
	Offset            int             `json:"offset"`
	MachineIdentifier string          `json:"machineIdentifier"`
	FriendlyName      string          `json:"friendlyName"`
	Version           string          `json:"version"`
	Metadata          []PlexMetadata  `json:"Metadata"`
	Directory         []PlexDirectory `json:"Directory"`
	Hub               []PlexHub       `json:"Hub"`
}

type PlexHub struct {
	Type     string         `json:"type"`
	Metadata []PlexMetadata `json:"Metadata"`
}

type plexEnvelope struct {
	MediaContainer PlexContainer `json:"MediaContainer"`
}

type Page struct {
	Start int
	Size  int
}

type Plex struct {
	base   *url.URL
	token  string
	client *http.Client
}

func NewPlex(base *url.URL, token string) *Plex {
	return &Plex{base: base, token: token, client: http.DefaultClient}
}

// URL builds an authenticated Plex URL; callers must never hand it to a client.
func (p *Plex) URL(path string, query url.Values) *url.URL {
	u := p.base.JoinPath()
	u.Path = path
	if query == nil {
		query = url.Values{}
	}
	query.Set("X-Plex-Token", p.token)
	u.RawQuery = query.Encode()
	return u
}

func (p Page) header() http.Header {
	return http.Header{
		"X-Plex-Container-Start": {strconv.Itoa(p.Start)},
		"X-Plex-Container-Size":  {strconv.Itoa(p.Size)},
	}
}

func (p *Plex) send(method, path string, query url.Values, header http.Header) (*http.Response, error) {
	req, err := http.NewRequest(method, p.URL(path, query).String(), nil)
	if err != nil {
		return nil, err
	}
	if header != nil {
		req.Header = header.Clone()
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex %s: %w", path, withoutURL(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("plex %s: %s", path, resp.Status)
	}
	return resp, nil
}

func (p *Plex) get(path string, query url.Values, header http.Header) (PlexContainer, error) {
	resp, err := p.send(http.MethodGet, path, query, header)
	if err != nil {
		return PlexContainer{}, err
	}
	defer resp.Body.Close()
	var envelope plexEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return PlexContainer{}, fmt.Errorf("plex %s: %w", path, err)
	}
	// Plex names the section once per listing, not on each item.
	c := envelope.MediaContainer
	for i := range c.Metadata {
		c.Metadata[i].LibrarySectionID = cmp.Or(c.Metadata[i].LibrarySectionID, c.LibrarySectionID)
	}
	return c, nil
}

func (p *Plex) call(method, path string, query url.Values, header http.Header) error {
	resp, err := p.send(method, path, query, header)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (p *Plex) Identity() (PlexContainer, error) {
	return p.get("/", nil, nil)
}

func (p *Plex) Sections() ([]PlexDirectory, error) {
	c, err := p.get("/library/sections", nil, nil)
	return c.Directory, err
}

func (p *Plex) Item(ratingKey string) (PlexMetadata, error) {
	c, err := p.get("/library/metadata/"+ratingKey, nil, nil)
	if err != nil {
		return PlexMetadata{}, err
	}
	if len(c.Metadata) == 0 {
		return PlexMetadata{}, fmt.Errorf("plex item %s: not found", ratingKey)
	}
	return c.Metadata[0], nil
}

func (p *Plex) Children(ratingKey string) ([]PlexMetadata, error) {
	c, err := p.get("/library/metadata/"+ratingKey+"/children", nil, nil)
	return c.Metadata, err
}

func (p *Plex) AllLeaves(ratingKey string) ([]PlexMetadata, error) {
	c, err := p.get("/library/metadata/"+ratingKey+"/allLeaves", nil, nil)
	return c.Metadata, err
}

// SectionItems lists one Plex type (1 movie, 2 show, 4 episode) of a section.
func (p *Plex) SectionItems(section string, query url.Values, page Page) (PlexContainer, error) {
	return p.get("/library/sections/"+section+"/all", query, page.header())
}

func (p *Plex) OnDeck() ([]PlexMetadata, error) {
	c, err := p.get("/library/onDeck", nil, nil)
	return c.Metadata, err
}

func (p *Plex) Collections(section string) ([]PlexMetadata, error) {
	c, err := p.get("/library/sections/"+section+"/collections", nil, nil)
	return c.Metadata, err
}

func (p *Plex) LibraryItems(query url.Values, page Page) (PlexContainer, error) {
	return p.get("/library/all", query, page.header())
}

func (p *Plex) Search(term string, limit int) ([]PlexMetadata, error) {
	c, err := p.get("/hubs/search", url.Values{"query": {term}, "limit": {strconv.Itoa(limit)}}, nil)
	if err != nil {
		return nil, err
	}
	var found []PlexMetadata
	for _, hub := range c.Hub {
		found = append(found, hub.Metadata...)
	}
	return found, nil
}

// SetProgress keeps a resume point; Plex ignores zero and never marks
// an item watched from here.
func (p *Plex) SetProgress(ratingKey string, positionMs int64) error {
	return p.call(http.MethodGet, "/:/progress", url.Values{
		"key":        {ratingKey},
		"identifier": {plexLibraryIdentifier},
		"time":       {strconv.FormatInt(positionMs, 10)},
		"state":      {"stopped"},
	}, nil)
}

func (p *Plex) SetPlayed(ratingKey string, played bool) error {
	path := "/:/unscrobble"
	if played {
		path = "/:/scrobble"
	}
	return p.call(http.MethodGet, path, url.Values{"key": {ratingKey}, "identifier": {plexLibraryIdentifier}}, nil)
}

// SelectStreams makes Plex remember a file's audio and subtitle choice; a
// subtitle id of 0 means none. Nil leaves that choice as it is.
func (p *Plex) SelectStreams(partID int, audioID, subtitleID *int) error {
	query := url.Values{"allParts": {"1"}}
	if audioID != nil {
		query.Set("audioStreamID", strconv.Itoa(*audioID))
	}
	if subtitleID != nil {
		query.Set("subtitleStreamID", strconv.Itoa(*subtitleID))
	}
	return p.call(http.MethodPut, "/library/parts/"+strconv.Itoa(partID), query, nil)
}

// withoutURL drops the request URL from a transport error: it carries the token.
func withoutURL(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return urlError.Err
	}
	return err
}
