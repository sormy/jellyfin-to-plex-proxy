package main

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultImageSize    = 1920
	maxImageSize        = 3840
	maxBitrateTestBytes = 100_000_000
)

// Only these client headers reach Plex; they make seeking and caching work.
var proxiedHeaders = []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match"}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request, target *url.URL) {
	s.proxyAs(w, r, target, http.Header{}, "")
}

// proxyAs relays a Plex response, sending Plex extra headers and, when Plex
// cannot tell, the content type a player needs.
func (s *Server) proxyAs(w http.ResponseWriter, r *http.Request, target *url.URL, header http.Header, contentType string) {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = target
			pr.Out.Host = target.Host
			pr.Out.Header = header.Clone()
			for _, name := range proxiedHeaders {
				if value := pr.In.Header.Get(name); value != "" {
					pr.Out.Header.Set(name, value)
				}
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if contentType != "" {
				resp.Header.Set("Content-Type", contentType)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if !errors.Is(err, context.Canceled) {
				log.Printf("proxy %s: %v", r.URL.Path, withoutURL(err))
			}
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	m, err := s.plexItem(r.PathValue("id"))
	if err != nil {
		respond(w, "", err)
		return
	}
	artwork := map[string]string{
		"primary":  cmp.Or(m.Thumb, m.Composite),
		"backdrop": m.Art,
		"thumb":    cmp.Or(m.Art, m.Thumb),
	}[r.PathValue("type")]
	if artwork == "" {
		http.NotFound(w, r)
		return
	}
	width, height := imageBox(query(r))
	s.proxy(w, r, s.plex.URL("/photo/:/transcode", url.Values{
		"url":     {artwork},
		"width":   {strconv.Itoa(width)},
		"height":  {strconv.Itoa(height)},
		"upscale": {"1"},
	}))
}

// imageBox is the box Plex fits an image within; a side the client left
// open gets a bound large enough not to constrain.
func imageBox(q url.Values) (int, int) {
	width := cmp.Or(intParam(q, "maxwidth"), intParam(q, "width"), intParam(q, "fillwidth"))
	height := cmp.Or(intParam(q, "maxheight"), intParam(q, "height"), intParam(q, "fillheight"))
	if width == 0 && height == 0 {
		return defaultImageSize, defaultImageSize
	}
	return cmp.Or(width, maxImageSize), cmp.Or(height, maxImageSize)
}

// part finds the file a media source names, or the item's first one.
func (s *Server) part(itemID, sourceID string) (PlexPart, error) {
	m, err := s.plexItem(itemID)
	if err != nil {
		return PlexPart{}, err
	}
	if part, ok := findPart(m, sourceID); ok {
		return part, nil
	}
	return PlexPart{}, errNotFound
}

func findPart(m PlexMetadata, sourceID string) (PlexPart, bool) {
	for _, media := range m.Media {
		for _, part := range media.Part {
			if sourceID == "" || EncodeID(KindPart, strconv.Itoa(part.ID)) == sourceID {
				return part, true
			}
		}
	}
	return PlexPart{}, false
}

// stream accepts a play session instead of a token: players fetch it bare.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	file, id := r.PathValue("file"), r.PathValue("id")
	if file != "stream" && !strings.HasPrefix(file, "stream.") {
		http.NotFound(w, r)
		return
	}
	q := query(r)
	if q.Get("playsessionid") != s.playSession(id) && !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	part, err := s.part(id, q.Get("mediasourceid"))
	if err != nil {
		respond(w, "", err)
		return
	}
	s.proxy(w, r, s.plex.URL(part.Key, nil))
}

func (s *Server) subtitle(w http.ResponseWriter, r *http.Request) {
	part, err := s.part(r.PathValue("id"), r.PathValue("source"))
	if err != nil {
		respond(w, "", err)
		return
	}
	index, _ := strconv.Atoi(r.PathValue("index"))
	for position, stream := range ExternalSubtitles(part.Stream) {
		if ExternalSubtitleIndex(part.Stream, position) == index {
			s.proxy(w, r, s.plex.URL(stream.Key, nil))
			return
		}
	}
	http.NotFound(w, r)
}

// bitrateTest sends the bytes a client times to pick a streaming bitrate.
func (s *Server) bitrateTest(w http.ResponseWriter, r *http.Request) {
	size := min(max(int64(intParam(query(r), "size")), 0), maxBitrateTestBytes)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	io.CopyN(w, zeros{}, size)
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
