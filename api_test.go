// Scenarios over the Jellyfin API. They run against the proxy wired to a fake
// Plex, or, with JELLYFIN_URL set, against a live proxy
// and the Plex behind it, changing watch state on a test series and restoring it.
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testSeries        = "We Bare Bears"
	resumePositionMs  = 120_000
	watchedFraction   = 0.95
	streamProbeLength = 1024
)

type client struct {
	t        *testing.T
	base     string
	token    string
	live     bool
	signedIn User
}

const (
	fakePassword     = "secret"
	swiftfinSDKMajor = 12
)

func intOf(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func newFakeServer(t *testing.T) *Server {
	plexURL, _ := url.Parse(newFakePlex(t).URL)
	server, err := NewServer(NewPlex(plexURL, fakePlexToken), Config{UserName: defaultUserName, Password: fakePassword})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func newClient(t *testing.T) *client {
	base, password := os.Getenv("JELLYFIN_URL"), cmp.Or(os.Getenv("JELLYFIN_PASSWORD"), defaultPassword)
	c := &client{t: t, base: base, live: base != ""}
	if !c.live {
		proxy := httptest.NewServer(newFakeServer(t))
		t.Cleanup(proxy.Close)
		c.base, password = proxy.URL, fakePassword
	}
	var auth AuthenticationResult
	c.expect(http.MethodPost, "/Users/AuthenticateByName", nil,
		AuthenticateByName{Username: cmp.Or(os.Getenv("JELLYFIN_USERNAME"), defaultUserName), Pw: password},
		http.StatusOK, &auth)
	c.token = auth.AccessToken
	c.signedIn = auth.User
	return c
}

func (c *client) do(method, path string, q url.Values, body any) *http.Response {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, c.base+path+"?"+q.Encode(), reader)
	req.Header.Set("Content-Type", "application/json")
	fields := `MediaBrowser Client="Integration", Device="go test", DeviceId="jellyfin-to-plex-proxy-test", Version="1"`
	if c.token != "" {
		fields += ", Token=" + c.token
	}
	req.Header.Set("Authorization", fields)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func (c *client) expect(method, path string, q url.Values, body any, status int, out any) {
	c.t.Helper()
	resp := c.do(method, path, q, body)
	defer resp.Body.Close()
	if resp.StatusCode != status {
		text, _ := io.ReadAll(resp.Body)
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, status, text)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			c.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

func (c *client) items(path string, q url.Values) ItemsResult {
	c.t.Helper()
	var result ItemsResult
	c.expect(http.MethodGet, path, q, nil, http.StatusOK, &result)
	return result
}

func (c *client) item(id string) Item {
	c.t.Helper()
	var item Item
	c.expect(http.MethodGet, "/Items/"+id, nil, nil, http.StatusOK, &item)
	return item
}

func (c *client) report(path string, episode Item, positionMs int64) {
	c.t.Helper()
	c.expect(http.MethodPost, path, nil, reportOf(episode, positionMs), http.StatusNoContent, nil)
}

func reportOf(episode Item, positionMs int64) PlaybackReport {
	report := PlaybackReport{ItemId: episode.Id, PositionTicks: positionMs * ticksPerMillisecond}
	if len(episode.MediaSources) > 0 {
		report.MediaSourceId = episode.MediaSources[0].Id
	}
	return report
}

// try is expect without stopping the test, for cleanup steps that must all run.
func (c *client) try(method, path string, body any) {
	resp := c.do(method, path, nil, body)
	resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		c.t.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}
}

func (c *client) setPlayed(id string, played bool) UserData {
	c.t.Helper()
	method := http.MethodDelete
	if played {
		method = http.MethodPost
	}
	var data UserData
	c.expect(method, "/UserPlayedItems/"+id, nil, nil, http.StatusOK, &data)
	return data
}

func (c *client) series() Item {
	c.t.Helper()
	found := c.items("/Items", url.Values{"searchTerm": {testSeries}, "includeItemTypes": {"Series"}})
	i := slices.IndexFunc(found.Items, func(it Item) bool { return strings.EqualFold(it.Name, testSeries) })
	if i < 0 {
		c.t.Fatalf("series %q not found in %+v", testSeries, found.Items)
	}
	return found.Items[i]
}

func (c *client) episodes(series Item) []Item {
	c.t.Helper()
	return c.items("/Shows/"+series.Id+"/Episodes", nil).Items
}

// preserveWatchState puts back the watched flag and resume point of every
// episode the test changed, touching nothing else.
func (c *client) preserveWatchState(series Item) {
	before := c.episodes(series)
	c.t.Cleanup(func() {
		for i, now := range c.episodes(series) {
			was, is := before[i].UserData, now.UserData
			if is.Played != was.Played {
				method := map[bool]string{true: http.MethodPost, false: http.MethodDelete}[was.Played]
				c.try(method, "/UserPlayedItems/"+now.Id, nil)
			}
			if is.PlaybackPositionTicks != was.PlaybackPositionTicks && was.PlaybackPositionTicks > 0 {
				c.try(http.MethodPost, "/Sessions/Playing/Progress", reportOf(now, was.PlaybackPositionTicks/ticksPerMillisecond))
			}
		}
		for i, episode := range c.episodes(series) {
			if episode.UserData.Played != before[i].UserData.Played {
				c.t.Errorf("restore %s: played %v, was %v", episode.Name, episode.UserData.Played, before[i].UserData.Played)
			}
		}
	})
}

func TestPublicEndpoints(t *testing.T) {
	c := newClient(t)
	var info PublicSystemInfo
	c.expect(http.MethodGet, "/System/Info/Public", nil, nil, http.StatusOK, &info)
	if len(info.Id) != idLength || info.ServerName == "" || info.Version == "" {
		t.Errorf("system info %+v", info)
	}
	// Swiftfin warns about servers older than its SDK, 12.0.0.
	if major, _, _ := strings.Cut(info.Version, "."); cmp.Compare(intOf(major), swiftfinSDKMajor) < 0 {
		t.Errorf("version %s is older than Swiftfin's SDK", info.Version)
	}
	var users []User
	c.expect(http.MethodGet, "/Users/Public", nil, nil, http.StatusOK, &users)
	var quickConnect bool
	c.expect(http.MethodGet, "/QuickConnect/Enabled", nil, nil, http.StatusOK, &quickConnect)
	c.expect(http.MethodGet, "/Branding/Configuration", nil, nil, http.StatusOK, nil)
	if len(users) != 1 || quickConnect {
		t.Errorf("users %+v, quick connect %v", users, quickConnect)
	}
}

func TestAuthentication(t *testing.T) {
	c := newClient(t)
	anonymous := &client{t: t, base: c.base}
	anonymous.expect(http.MethodPost, "/Users/AuthenticateByName", nil,
		AuthenticateByName{Username: cmp.Or(os.Getenv("JELLYFIN_USERNAME"), defaultUserName), Pw: "wrong"}, http.StatusUnauthorized, nil)
	anonymous.expect(http.MethodGet, "/Users/Me", nil, nil, http.StatusUnauthorized, nil)
	var me User
	c.expect(http.MethodGet, "/Users/Me", nil, nil, http.StatusOK, &me)
	c.expect(http.MethodGet, "/UsErS/mE", nil, nil, http.StatusOK, nil)
	if me.Id == "" || me.Name == "" {
		t.Errorf("me %+v", me)
	}
	// Swiftfin shows no Play button without playback allowed, and rejects
	// a policy lacking its provider ids.
	for _, user := range []User{c.signedIn, me} {
		policy := user.Policy
		if !policy.EnableMediaPlayback || policy.IsAdministrator || policy.AuthenticationProviderId == "" || policy.PasswordResetProviderId == "" {
			t.Errorf("policy %+v", policy)
		}
	}
}

func TestLibraries(t *testing.T) {
	c := newClient(t)
	views := c.items("/UserViews", nil)
	if len(views.Items) == 0 {
		t.Fatal("no libraries")
	}
	for _, view := range views.Items {
		t.Run(view.Name, func(t *testing.T) {
			c := &client{t: t, base: c.base, token: c.token}
			if c.item(view.Id).CollectionType != view.CollectionType {
				t.Error("view lookup differs")
			}
			// Infuse reads these counts and treats a library of zero as empty.
			if view.ChildCount == 0 || view.RecursiveItemCount < view.ChildCount {
				t.Errorf("counts: %d children, %d recursive", view.ChildCount, view.RecursiveItemCount)
			}
			itemType := map[string]string{"movies": "Movie", "tvshows": "Series"}[view.CollectionType]
			q := url.Values{"parentId": {view.Id}, "includeItemTypes": {itemType}, "sortBy": {"SortName"}, "limit": {"5"}}
			first := c.items("/Items", q)
			q.Set("startIndex", "5")
			second := c.items("/Items", q)
			if len(first.Items) == 0 || first.TotalRecordCount < len(first.Items) {
				t.Fatalf("first page %d of %d", len(first.Items), first.TotalRecordCount)
			}
			if len(second.Items) > 0 && second.Items[0].Id == first.Items[0].Id {
				t.Error("pages overlap")
			}
			for _, item := range first.Items {
				if item.Type != itemType || item.UserData == nil || item.UserData.Key == "" {
					t.Errorf("item %+v", item)
				}
			}
			var latest []Item
			c.expect(http.MethodGet, "/Items/Latest", url.Values{"parentId": {view.Id}, "limit": {"3"}}, nil, http.StatusOK, &latest)
			if len(latest) == 0 || len(latest) > 3 {
				t.Errorf("latest %d items", len(latest))
			}
		})
	}
}

// TestLibraryTiles asks as Swiftfin's media screen does: any kind of video
// under each library, whose images make the library's tile.
func TestLibraryTiles(t *testing.T) {
	c := newClient(t)
	for _, view := range c.items("/UserViews", nil).Items {
		tile := c.items("/Items", url.Values{
			"parentId": {view.Id}, "recursive": {"true"}, "sortBy": {"Random"}, "limit": {"3"},
			"includeItemTypes": {"BoxSet", "Movie", "MusicVideo", "Series", "Video"},
		})
		if len(tile.Items) == 0 || tile.Items[0].ImageTags["Primary"] == "" {
			t.Errorf("%s tile: %+v", view.Name, tile.Items)
		}
		foreign := map[string]string{"movies": "Series", "tvshows": "Movie"}[view.CollectionType]
		if other := c.items("/Items", url.Values{"parentId": {view.Id}, "includeItemTypes": {foreign}}); len(other.Items) > 0 {
			t.Errorf("%s holds %d of %s", view.Name, len(other.Items), foreign)
		}
	}
}

func TestLibrarySorts(t *testing.T) {
	c := newClient(t)
	for _, view := range c.items("/UserViews", nil).Items {
		itemType := map[string]string{"movies": "Movie", "tvshows": "Series"}[view.CollectionType]
		for _, tc := range []struct {
			sortBy, sortOrder string
			key               func(Item) string
			descending        bool
		}{
			{"DateCreated", "Descending", func(it Item) string { return it.DateCreated }, true},
			{"ProductionYear", "Ascending", func(it Item) string { return fmt.Sprintf("%04d", it.ProductionYear) }, false},
		} {
			sorted := c.items("/Items", url.Values{
				"parentId": {view.Id}, "includeItemTypes": {itemType}, "sortBy": {tc.sortBy},
				"sortOrder": {tc.sortOrder}, "limit": {"20"},
			}).Items
			keys := []string{}
			for _, it := range sorted {
				if k := tc.key(it); k != "" && k != "0000" {
					keys = append(keys, k)
				}
			}
			if tc.descending {
				slices.Reverse(keys)
			}
			if !slices.IsSorted(keys) {
				t.Errorf("%s by %s %s: %v", view.Name, tc.sortBy, tc.sortOrder, keys)
			}
		}
	}
}

func TestRecentlyAddedAcrossLibraries(t *testing.T) {
	c := newClient(t)
	recent := c.items("/Items", url.Values{
		"includeItemTypes": {"Movie", "Series"}, "recursive": {"true"},
		"sortBy": {"DateCreated"}, "sortOrder": {"Descending"}, "limit": {"10"},
	})
	if len(recent.Items) == 0 {
		t.Fatal("nothing recently added")
	}
	for i := 1; i < len(recent.Items); i++ {
		if recent.Items[i].DateCreated > recent.Items[i-1].DateCreated {
			t.Errorf("%s after %s", recent.Items[i].DateCreated, recent.Items[i-1].DateCreated)
		}
	}
}

func TestSeriesNavigation(t *testing.T) {
	c := newClient(t)
	series := c.series()
	seasons := c.items("/Shows/"+series.Id+"/Seasons", nil)
	if len(seasons.Items) == 0 || seasons.Items[0].SeriesId != series.Id {
		t.Fatalf("seasons %+v", seasons.Items)
	}
	season := seasons.Items[0]
	episodes := c.items("/Shows/"+season.Id+"/Episodes", url.Values{"seasonId": {season.Id}})
	if len(episodes.Items) == 0 {
		t.Fatal("no episodes")
	}
	for _, episode := range episodes.Items {
		if episode.SeasonId != season.Id || episode.SeriesId != series.Id || episode.IndexNumber == nil {
			t.Errorf("episode %+v", episode)
		}
	}
	all := c.episodes(series)
	if count := c.item(series.Id).RecursiveItemCount; count != len(all) {
		t.Errorf("series counts %d episodes, lists %d", count, len(all))
	}
	if len(all) < len(episodes.Items) {
		t.Errorf("series has %d episodes, season %d", len(all), len(episodes.Items))
	}
	firstEpisode := c.items("/Items", url.Values{
		"parentId": {series.Id}, "includeItemTypes": {"Episode"}, "recursive": {"true"}, "limit": {"1"},
	})
	if len(firstEpisode.Items) != 1 {
		t.Errorf("first episode %+v", firstEpisode.Items)
	}
	byIDs := c.items("/Items", url.Values{"ids": {series.Id, season.Id}})
	if len(byIDs.Items) != 2 {
		t.Errorf("ids lookup %d items", len(byIDs.Items))
	}
}

// TestSeasonAndAdjacentEpisodes follows Swiftfin's season page, which
// enables Play only once it finds an episode to resume or start.
func TestSeasonAndAdjacentEpisodes(t *testing.T) {
	c := newClient(t)
	series := c.series()
	c.preserveWatchState(series)
	season := c.items("/Shows/"+series.Id+"/Seasons", nil).Items[0]
	inSeason := c.items("/Shows/"+season.Id+"/Episodes", url.Values{"seasonId": {season.Id}}).Items
	if len(inSeason) < 3 {
		t.Skip("needs a season of three episodes")
	}
	first := c.items("/Items", url.Values{
		"parentId": {season.Id}, "includeItemTypes": {"Episode"}, "recursive": {"true"},
		"isMissing": {"false"}, "sortOrder": {"Ascending"}, "limit": {"1"},
	})
	if len(first.Items) != 1 || first.Items[0].Id != inSeason[0].Id {
		t.Errorf("first episode of the season: %+v", first.Items)
	}

	// A resume point needs a version long enough to hold one.
	i := slices.IndexFunc(inSeason[1:], func(it Item) bool {
		return len(it.MediaSources) > 0 && it.MediaSources[0].RunTimeTicks >= minResumeDurationMs*ticksPerMillisecond
	})
	if i < 0 {
		t.Fatal("no episode long enough to resume")
	}
	resumed := inSeason[1+i]
	c.setPlayed(resumed.Id, false)
	c.report("/Sessions/Playing/Stopped", resumed, resumePositionMs)
	resume := c.items("/UserItems/Resume", url.Values{"parentId": {season.Id}, "limit": {"1"}})
	if len(resume.Items) != 1 || resume.Items[0].SeasonId != season.Id {
		t.Errorf("resume in the season: %+v", resume.Items)
	}

	for _, tc := range []struct {
		around int
		want   []int
	}{{1, []int{0, 1, 2}}, {0, []int{0, 1}}} {
		adjacent := c.items("/Shows/"+series.Id+"/Episodes", url.Values{"adjacentTo": {inSeason[tc.around].Id}})
		var got []string
		for _, it := range adjacent.Items {
			got = append(got, it.Id)
		}
		var want []string
		for _, i := range tc.want {
			want = append(want, inSeason[i].Id)
		}
		if !slices.Equal(got, want) {
			t.Errorf("adjacent to episode %d: %v, want %v", tc.around, got, want)
		}
	}
}

func TestImages(t *testing.T) {
	c := newClient(t)
	series := c.series()
	for _, tc := range []struct {
		kind   string
		status int
	}{{"Primary", http.StatusOK}, {"Backdrop", http.StatusOK}, {"Logo", http.StatusNotFound}} {
		resp := c.do(http.MethodGet, "/Items/"+series.Id+"/Images/"+tc.kind, url.Values{"maxWidth": {"300"}}, nil)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", tc.kind, resp.StatusCode, tc.status)
		}
		if tc.status == http.StatusOK && !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			t.Errorf("%s: content type %q", tc.kind, resp.Header.Get("Content-Type"))
		}
	}
}

func TestPlayback(t *testing.T) {
	c := newClient(t)
	episode := c.item(c.episodes(c.series())[0].Id)
	if len(episode.MediaSources) == 0 || len(episode.MediaStreams) == 0 || episode.MediaType != "Video" {
		t.Fatalf("episode %+v", episode)
	}
	var info PlaybackInfo
	c.expect(http.MethodPost, "/Items/"+episode.Id+"/PlaybackInfo", nil,
		map[string]string{"MediaSourceId": episode.MediaSources[0].Id}, http.StatusOK, &info)
	if info.PlaySessionId == "" || len(info.MediaSources) != 1 {
		t.Fatalf("playback info %+v", info)
	}
	source := info.MediaSources[0]
	streamPath := "/Videos/" + episode.Id + "/stream"
	bare := &client{t: t, base: c.base}
	for _, tc := range []struct {
		name    string
		session string
		status  int
	}{{"signed", info.PlaySessionId, http.StatusPartialContent}, {"unsigned", "forged", http.StatusUnauthorized}} {
		req, _ := http.NewRequest(http.MethodGet, bare.base+streamPath+"?"+url.Values{
			"static": {"true"}, "mediaSourceId": {source.Id}, "playSessionId": {tc.session},
		}.Encode(), nil)
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", streamProbeLength-1))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.status)
		}
		if tc.status == http.StatusPartialContent && len(body) != streamProbeLength {
			t.Errorf("%s: %d bytes", tc.name, len(body))
		}
	}
	for _, stream := range source.MediaStreams {
		if !stream.IsExternal {
			continue
		}
		resp := bare.do(http.MethodGet, stream.DeliveryUrl, nil, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("subtitle %s: status %d", stream.DeliveryUrl, resp.StatusCode)
		}
	}
}

func TestMarkPlayed(t *testing.T) {
	c := newClient(t)
	series := c.series()
	c.preserveWatchState(series)
	episode := c.episodes(series)[0]
	c.setPlayed(episode.Id, false)
	unplayed := *c.item(series.Id).UserData.UnplayedItemCount
	for _, played := range []bool{true, false, true} {
		data := c.setPlayed(episode.Id, played)
		if data.Played != played || data.ItemId != episode.Id || c.item(episode.Id).UserData.Played != played {
			t.Errorf("set played %v: %+v", played, data)
		}
	}
	if after := *c.item(series.Id).UserData.UnplayedItemCount; after != unplayed-1 {
		t.Errorf("series unplayed %d → %d after marking an episode played", unplayed, after)
	}
}

func TestMarkSeriesPlayed(t *testing.T) {
	c := newClient(t)
	series := c.series()
	c.preserveWatchState(series)
	for _, played := range []bool{true, false} {
		data := c.setPlayed(series.Id, played)
		if data.Played != played || data.ItemId != series.Id {
			t.Errorf("series played %v: %+v", played, data)
		}
		for _, episode := range c.episodes(series) {
			if episode.UserData.Played != played {
				t.Fatalf("series played %v: %s played %v", played, episode.Name, episode.UserData.Played)
			}
		}
	}
}

func TestMarkSeasonPlayed(t *testing.T) {
	c := newClient(t)
	series := c.series()
	c.preserveWatchState(series)
	season := c.items("/Shows/"+series.Id+"/Seasons", nil).Items[0]
	for _, played := range []bool{true, false} {
		c.setPlayed(season.Id, played)
		for _, episode := range c.items("/Shows/"+season.Id+"/Episodes", url.Values{"seasonId": {season.Id}}).Items {
			if episode.UserData.Played != played {
				t.Errorf("season played %v: %s played %v", played, episode.Name, episode.UserData.Played)
			}
		}
	}
}

// TestResumeAndNextUp works at the series' end, where nothing later is
// watched, so next up follows from these steps alone.
func TestResumeAndNextUp(t *testing.T) {
	c := newClient(t)
	series := c.series()
	c.preserveWatchState(series)
	episodes := c.episodes(series)
	if len(episodes) < 3 {
		t.Skip("needs three episodes")
	}
	tail := episodes[len(episodes)-3:]
	for _, episode := range tail {
		c.setPlayed(episode.Id, false)
	}
	c.setPlayed(tail[0].Id, true)
	c.expectNextUp(series, tail[1])

	resumed := tail[1]
	runtimeMs := resumed.RunTimeTicks / ticksPerMillisecond
	c.report("/Sessions/Playing", resumed, 0)
	c.report("/Sessions/Playing/Progress", resumed, resumePositionMs)
	c.report("/Sessions/Playing/Stopped", resumed, resumePositionMs)
	data := c.item(resumed.Id).UserData
	if data.Played || data.PlaybackPositionTicks/ticksPerMillisecond != resumePositionMs {
		t.Errorf("after stopping at %d ms: %+v", resumePositionMs, data)
	}
	resume := c.items("/UserItems/Resume", url.Values{"parentId": {series.Id}})
	if !slices.ContainsFunc(resume.Items, func(it Item) bool { return it.Id == resumed.Id }) {
		t.Errorf("resume lacks %s", resumed.Name)
	}

	c.report("/Sessions/Playing/Stopped", resumed, runtimeMs/100)
	if data := c.item(resumed.Id).UserData; data.Played || data.PlaybackPositionTicks != 0 {
		t.Errorf("stopping in the first 5%% kept %+v", data)
	}

	c.report("/Sessions/Playing/Progress", resumed, runtimeMs*95/100)
	if c.item(resumed.Id).UserData.Played {
		t.Error("a progress report marked the item played")
	}
	c.report("/Sessions/Playing/Stopped", resumed, runtimeMs*95/100)
	if !c.item(resumed.Id).UserData.Played {
		t.Errorf("stopping at 95%% did not mark %s played", resumed.Name)
	}
	c.expectNextUp(series, tail[2])
}

func (c *client) expectNextUp(series, want Item) {
	c.t.Helper()
	nextUp := c.items("/Shows/NextUp", url.Values{"seriesId": {series.Id}})
	if len(nextUp.Items) != 1 || nextUp.Items[0].Id != want.Id {
		names := []string{}
		for _, it := range nextUp.Items {
			names = append(names, it.Name)
		}
		c.t.Errorf("next up %v, want %s", names, want.Name)
	}
}

func TestHomeRowsAndUserRoutes(t *testing.T) {
	c := newClient(t)
	series := c.series()
	c.preserveWatchState(series)
	episodes := c.episodes(series)
	tail := episodes[len(episodes)-2:]
	var me User
	c.expect(http.MethodGet, "/Users/Me", nil, nil, http.StatusOK, &me)
	user := "/Users/" + me.Id

	var played UserData
	c.expect(http.MethodPost, user+"/PlayedItems/"+tail[0].Id, nil, nil, http.StatusOK, &played)
	c.setPlayed(tail[1].Id, false)
	c.report("/Sessions/Playing/Stopped", tail[1], resumePositionMs)
	if !played.Played {
		t.Errorf("legacy played route: %+v", played)
	}

	for _, path := range []string{"/UserItems/Resume", user + "/Items/Resume"} {
		resume := c.items(path, url.Values{"mediaTypes": {"Video"}, "limit": {"50"}})
		if !slices.ContainsFunc(resume.Items, func(it Item) bool { return it.Id == tail[1].Id }) {
			t.Errorf("%s lacks %s", path, tail[1].Name)
		}
		for _, it := range resume.Items {
			if it.UserData.PlaybackPositionTicks == 0 {
				t.Errorf("%s holds %s without a position", path, it.Name)
			}
		}
	}
	for _, it := range c.items("/Shows/NextUp", url.Values{"limit": {"50"}}).Items {
		if it.Type != "Episode" || it.UserData.PlaybackPositionTicks != 0 {
			t.Errorf("next up holds %s %s at %d", it.Type, it.Name, it.UserData.PlaybackPositionTicks)
		}
	}

	views := c.items(user+"/Views", nil)
	var preferences DisplayPreferences
	c.expect(http.MethodGet, "/DisplayPreferences/usersettings", url.Values{"userId": {me.Id}, "client": {"emby"}}, nil, http.StatusOK, &preferences)
	c.expect(http.MethodPost, "/DisplayPreferences/usersettings", url.Values{"client": {"emby"}}, preferences, http.StatusNoContent, nil)
	if preferences.Id != "usersettings" || preferences.Client != "emby" || preferences.CustomPrefs == nil {
		t.Errorf("display preferences %+v", preferences)
	}
	var folders []VirtualFolder
	c.expect(http.MethodGet, "/Library/VirtualFolders", nil, nil, http.StatusOK, &folders)
	mediaFolders := c.items("/Library/MediaFolders", nil)
	if len(folders) != len(views.Items) || folders[0].ItemId != views.Items[0].Id || folders[0].CollectionType == "" ||
		len(mediaFolders.Items) != len(views.Items) {
		t.Errorf("virtual folders %+v, media folders %d", folders, len(mediaFolders.Items))
	}
	for _, path := range []string{"/UserViews/GroupingOptions", user + "/GroupingOptions"} {
		var options []NameID
		c.expect(http.MethodGet, path, nil, nil, http.StatusOK, &options)
		if len(options) != len(views.Items) || options[0].Id != views.Items[0].Id {
			t.Errorf("%s: %+v", path, options)
		}
	}
	var latest []Item
	c.expect(http.MethodGet, user+"/Items/Latest", url.Values{"parentId": {views.Items[0].Id}}, nil, http.StatusOK, &latest)
	var item Item
	c.expect(http.MethodGet, user+"/Items/"+series.Id, nil, nil, http.StatusOK, &item)
	listed := c.items(user+"/Items", url.Values{"parentId": {views.Items[0].Id}, "limit": {"1"}})
	var favorite UserData
	c.expect(http.MethodPost, user+"/FavoriteItems/"+series.Id, nil, nil, http.StatusOK, &favorite)
	if len(views.Items) == 0 || item.Id != series.Id || len(listed.Items) != 1 || !favorite.IsFavorite {
		t.Errorf("user routes: %d views, item %q, %d listed, favorite %v", len(views.Items), item.Name, len(listed.Items), favorite.IsFavorite)
	}
}

func TestMediaVersions(t *testing.T) {
	c := newClient(t)
	candidates := c.episodes(c.series())
	for _, view := range c.items("/UserViews", nil).Items {
		candidates = append(candidates, c.items("/Items", url.Values{"parentId": {view.Id}, "limit": {"200"}}).Items...)
	}
	var versioned Item
	if i := slices.IndexFunc(candidates, func(it Item) bool { return len(it.MediaSources) > 1 }); i >= 0 {
		versioned = candidates[i]
	}
	if versioned.Id == "" {
		t.Skip("no item with two versions")
	}
	second := versioned.MediaSources[1]
	var info PlaybackInfo
	c.expect(http.MethodPost, "/Items/"+versioned.Id+"/PlaybackInfo", nil,
		PlaybackReport{MediaSourceId: second.Id}, http.StatusOK, &info)
	if len(info.MediaSources) != 1 || info.MediaSources[0].Id != second.Id {
		t.Fatalf("playback info %+v", info.MediaSources)
	}
	req, _ := http.NewRequest(http.MethodGet, c.base+"/Videos/"+versioned.Id+"/stream?"+url.Values{
		"mediaSourceId": {second.Id}, "playSessionId": {info.PlaySessionId},
	}.Encode(), nil)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if total := fmt.Sprintf("/%d", second.Size); resp.StatusCode != http.StatusPartialContent ||
		!strings.HasSuffix(resp.Header.Get("Content-Range"), total) {
		t.Errorf("second version: status %d, range %q, want size %d", resp.StatusCode, resp.Header.Get("Content-Range"), second.Size)
	}
}

func TestBitrateTest(t *testing.T) {
	c := newClient(t)
	for _, tc := range []struct {
		size string
		want int64
	}{{"5000", 5000}, {"0", 0}, {"-1", 0}, {"1000000000", maxBitrateTestBytes}} {
		if tc.want == maxBitrateTestBytes && c.live {
			continue
		}
		resp := c.do(http.MethodGet, "/Playback/BitrateTest", url.Values{"size": {tc.size}}, nil)
		n, _ := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || n != tc.want {
			t.Errorf("size %s: status %d, %d bytes, want %d", tc.size, resp.StatusCode, n, tc.want)
		}
	}
}

func TestFavoriteIsAcknowledged(t *testing.T) {
	c := newClient(t)
	id := c.series().Id
	for _, tc := range []struct {
		method   string
		favorite bool
	}{{http.MethodPost, true}, {http.MethodDelete, false}} {
		var data UserData
		c.expect(tc.method, "/UserFavoriteItems/"+id, nil, nil, http.StatusOK, &data)
		if data.IsFavorite != tc.favorite || data.Key == "" {
			t.Errorf("%s: %+v", tc.method, data)
		}
	}
}

func TestSearch(t *testing.T) {
	c := newClient(t)
	for _, tc := range []struct {
		types []string
		want  string
	}{{[]string{"Series"}, "Series"}, {[]string{"Movie"}, "Movie"}} {
		found := c.items("/Items", url.Values{"searchTerm": {"the"}, "includeItemTypes": tc.types, "limit": {"20"}})
		for _, item := range found.Items {
			if item.Type != tc.want {
				t.Errorf("%v search returned %s %q", tc.types, item.Type, item.Name)
			}
		}
	}
}

func TestStubs(t *testing.T) {
	c := newClient(t)
	id := c.series().Id
	for _, path := range []string{
		"/Items/" + id + "/LocalTrailers", "/Items/" + id + "/SpecialFeatures", "/Items/" + id + "/Similar",
		"/Videos/" + id + "/AdditionalParts", "/Persons", "/Items/Filters", "/Items/Filters2",
	} {
		c.expect(http.MethodGet, path, nil, nil, http.StatusOK, nil)
	}
	var languages []Culture
	c.expect(http.MethodGet, "/Localization/Cultures", nil, nil, http.StatusOK, &languages)
	for _, want := range []string{"eng", "rus"} {
		if !slices.ContainsFunc(languages, func(l Culture) bool { return slices.Contains(l.ThreeLetterISOLanguageNames, want) }) {
			t.Errorf("cultures lack %s", want)
		}
	}
	c.expect(http.MethodGet, "/LiveTv/Programs/Recommended", nil, nil, http.StatusOK, nil)
	for _, path := range []string{"/Sessions/Capabilities", "/Sessions/Capabilities/Full"} {
		c.expect(http.MethodPost, path, nil, map[string]string{}, http.StatusNoContent, nil)
	}
	c.expect(http.MethodDelete, "/Auth/Keys/"+c.token, nil, nil, http.StatusNoContent, nil)
	c.expect(http.MethodGet, "/Items/"+strings.Repeat("9", idLength), nil, nil, http.StatusNotFound, nil)
}

func TestDiscovery(t *testing.T) {
	c := newClient(t)
	target := strings.TrimPrefix(c.base, "http://")
	if c.live {
		host, _, _ := net.SplitHostPort(target)
		target = net.JoinHostPort(host, strings.TrimPrefix(discoveryAddress, ":"))
	} else {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		go newFakeServer(t).answerDiscovery(listener, "8096")
		target = listener.LocalAddr().String()
	}
	conn, err := net.Dial("udp", target)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	for _, probe := range []string{"hello", "Who is JellyfinServer?"} {
		conn.Write([]byte(probe))
	}
	buffer := make([]byte, discoveryBuffer)
	n, err := conn.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	var reply DiscoveryReply
	if err := json.Unmarshal(buffer[:n], &reply); err != nil || reply.Id == "" || !strings.HasPrefix(reply.Address, "http://") {
		t.Errorf("reply %s: %v", buffer[:n], err)
	}
}
