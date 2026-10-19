package main

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

func TestPlaylistEditing(t *testing.T) {
	c := newClient(t)
	if c.live {
		t.Skip("would leave a playlist on the live server: the proxy deletes none")
	}
	tracks := c.items("/Items", url.Values{"IncludeItemTypes": {"Audio"}, "Recursive": {"true"}}).Items
	if len(tracks) < 2 {
		t.Fatalf("tracks %+v", tracks)
	}
	first, second := tracks[0].Id, tracks[1].Id
	entries := func(playlist string) ([]string, []string) {
		var ids, entryIDs []string
		for _, it := range c.items("/Playlists/"+playlist+"/Items", nil).Items {
			ids, entryIDs = append(ids, it.Id), append(entryIDs, it.PlaylistItemId)
		}
		return ids, entryIDs
	}

	var created PlaylistCreated
	c.expect(http.MethodPost, "/Playlists", nil, PlaylistChange{Name: "Road Trip", Ids: []string{first}}, http.StatusOK, &created)
	playlist := created.Id

	var user PlaylistUser
	c.expect(http.MethodGet, "/Playlists/"+playlist+"/Users/"+c.signedIn.Id, nil, nil, http.StatusOK, &user)
	var smart PlaylistUser
	c.expect(http.MethodGet, "/Playlists/"+EncodeID(KindPlaylist, "501")+"/Users/"+c.signedIn.Id, nil, nil, http.StatusOK, &smart)
	if !user.CanEdit || smart.CanEdit {
		t.Errorf("can edit: made %+v, smart %+v", user, smart)
	}

	c.expect(http.MethodPost, "/Playlists/"+playlist+"/Items", url.Values{"ids": {second + "," + first}}, nil, http.StatusNoContent, nil)
	ids, entryIDs := entries(playlist)
	var info PlaylistInfo
	c.expect(http.MethodGet, "/Playlists/"+playlist, nil, nil, http.StatusOK, &info)
	if !slices.Equal(ids, []string{first, second}) || !slices.Equal(info.ItemIds, ids) || slices.Contains(entryIDs, "") ||
		c.item(playlist).ChildCount != 2 {
		t.Fatalf("after adding: %v, entries %v, info %+v", ids, entryIDs, info)
	}

	c.expect(http.MethodDelete, "/Playlists/"+playlist+"/Items", url.Values{"entryIds": {entryIDs[0]}}, nil, http.StatusNoContent, nil)
	if ids, _ := entries(playlist); !slices.Equal(ids, []string{second}) {
		t.Errorf("after removing the first: %v", ids)
	}

	c.expect(http.MethodPost, "/Playlists/"+playlist, nil, PlaylistChange{Name: "Renamed"}, http.StatusNoContent, nil)
	c.expect(http.MethodPost, "/Playlists/"+playlist, nil, PlaylistChange{Ids: []string{first, second}}, http.StatusNoContent, nil)
	if ids, _ := entries(playlist); !slices.Equal(ids, []string{first, second}) || c.item(playlist).Name != "Renamed" {
		t.Errorf("after the update: %v named %q", ids, c.item(playlist).Name)
	}

	for _, tc := range []struct {
		name, method, path string
		query              url.Values
		body               any
	}{
		{"empty playlist", http.MethodPost, "/Playlists", nil, PlaylistChange{Name: "Empty"}},
		{"entry outside Plex's numbers", http.MethodDelete, "/Playlists/" + playlist + "/Items", url.Values{"entryIds": {"../1"}}, nil},
		{"addition to a track", http.MethodPost, "/Playlists/" + first + "/Items", url.Values{"ids": {second}}, nil},
	} {
		c.expect(tc.method, tc.path, tc.query, tc.body, http.StatusBadRequest, nil)
	}
}
