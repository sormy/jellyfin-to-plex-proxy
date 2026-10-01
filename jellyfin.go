package main

import (
	"fmt"
	"strconv"
	"time"
)

// Jellyfin clients only parse dates carrying milliseconds and a zone.
const jellyfinTimeLayout = "2006-01-02T15:04:05.000Z07:00"

const ticksPerMillisecond = 10_000

// Jellyfin ids are 32 hex digits; the first one says what the rest points at.
type IDKind byte

const (
	KindItem    IDKind = '0'
	KindLibrary IDKind = '1'
	KindPart    IDKind = '2'
	KindGenre   IDKind = '3'
)

func EncodeID(kind IDKind, plexKey string) string {
	n, err := strconv.ParseUint(plexKey, 10, 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%c%031x", kind, n)
}

func DecodeID(id string) (IDKind, string, bool) {
	if len(id) != 32 {
		return 0, "", false
	}
	n, err := strconv.ParseUint(id[1:], 16, 64)
	if err != nil {
		return 0, "", false
	}
	return IDKind(id[0]), strconv.FormatUint(n, 10), true
}

func FormatUnix(seconds int64) string {
	if seconds == 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(jellyfinTimeLayout)
}

func FormatDay(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return ""
	}
	return t.Format(jellyfinTimeLayout)
}

// UserData must name its item: Swiftfin applies a change to its lists by ItemId.
type UserData struct {
	Key                   string  `json:"Key"`
	ItemId                string  `json:"ItemId"`
	PlaybackPositionTicks int64   `json:"PlaybackPositionTicks"`
	PlayedPercentage      float64 `json:"PlayedPercentage,omitempty"`
	Played                bool    `json:"Played"`
	PlayCount             int     `json:"PlayCount"`
	IsFavorite            bool    `json:"IsFavorite"`
	UnplayedItemCount     *int    `json:"UnplayedItemCount,omitempty"`
	LastPlayedDate        string  `json:"LastPlayedDate,omitempty"`
}

type MediaStream struct {
	Type                 string  `json:"Type"`
	Index                int     `json:"Index"`
	Codec                string  `json:"Codec,omitempty"`
	Language             string  `json:"Language,omitempty"`
	Title                string  `json:"Title,omitempty"`
	DisplayTitle         string  `json:"DisplayTitle,omitempty"`
	Profile              string  `json:"Profile,omitempty"`
	IsDefault            bool    `json:"IsDefault"`
	IsForced             bool    `json:"IsForced"`
	IsExternal           bool    `json:"IsExternal"`
	IsTextSubtitleStream bool    `json:"IsTextSubtitleStream"`
	DeliveryMethod       string  `json:"DeliveryMethod,omitempty"`
	DeliveryUrl          string  `json:"DeliveryUrl,omitempty"`
	Channels             int     `json:"Channels,omitempty"`
	Width                int     `json:"Width,omitempty"`
	Height               int     `json:"Height,omitempty"`
	BitRate              int     `json:"BitRate,omitempty"`
	AverageFrameRate     float64 `json:"AverageFrameRate,omitempty"`
	SampleRate           int     `json:"SampleRate,omitempty"`
	BitDepth             int     `json:"BitDepth,omitempty"`
	// Finamp requires these.
	IsInterlaced           bool `json:"IsInterlaced"`
	SupportsExternalStream bool `json:"SupportsExternalStream"`
}

type MediaSource struct {
	Id                         string        `json:"Id"`
	ETag                       string        `json:"ETag"`
	Name                       string        `json:"Name,omitempty"`
	Path                       string        `json:"Path,omitempty"`
	Protocol                   string        `json:"Protocol"`
	Type                       string        `json:"Type"`
	Container                  string        `json:"Container,omitempty"`
	Size                       int64         `json:"Size,omitempty"`
	Bitrate                    int           `json:"Bitrate,omitempty"`
	RunTimeTicks               int64         `json:"RunTimeTicks,omitempty"`
	IsRemote                   bool          `json:"IsRemote"`
	SupportsDirectPlay         bool          `json:"SupportsDirectPlay"`
	SupportsDirectStream       bool          `json:"SupportsDirectStream"`
	SupportsTranscoding        bool          `json:"SupportsTranscoding"`
	MediaStreams               []MediaStream `json:"MediaStreams"`
	DefaultAudioStreamIndex    *int          `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int          `json:"DefaultSubtitleStreamIndex,omitempty"`
	// Finamp requires these.
	IsInfiniteStream      bool `json:"IsInfiniteStream"`
	RequiresOpening       bool `json:"RequiresOpening"`
	RequiresClosing       bool `json:"RequiresClosing"`
	RequiresLooping       bool `json:"RequiresLooping"`
	SupportsProbing       bool `json:"SupportsProbing"`
	ReadAtNativeFramerate bool `json:"ReadAtNativeFramerate"`
	IgnoreDts             bool `json:"IgnoreDts"`
	IgnoreIndex           bool `json:"IgnoreIndex"`
	GenPtsInput           bool `json:"GenPtsInput"`
}

type Item struct {
	Id                       string            `json:"Id"`
	ServerId                 string            `json:"ServerId"`
	Name                     string            `json:"Name"`
	SortName                 string            `json:"SortName,omitempty"`
	Type                     string            `json:"Type"`
	Path                     string            `json:"Path,omitempty"`
	ParentId                 string            `json:"ParentId,omitempty"`
	IsFolder                 bool              `json:"IsFolder"`
	MediaType                string            `json:"MediaType,omitempty"`
	CollectionType           string            `json:"CollectionType,omitempty"`
	LocationType             string            `json:"LocationType,omitempty"`
	Overview                 string            `json:"Overview,omitempty"`
	Taglines                 []string          `json:"Taglines,omitempty"`
	OfficialRating           string            `json:"OfficialRating,omitempty"`
	CommunityRating          float64           `json:"CommunityRating,omitempty"`
	ProductionYear           int               `json:"ProductionYear,omitempty"`
	PremiereDate             string            `json:"PremiereDate,omitempty"`
	DateCreated              string            `json:"DateCreated,omitempty"`
	RunTimeTicks             int64             `json:"RunTimeTicks,omitempty"`
	Genres                   []string          `json:"Genres,omitempty"`
	ChildCount               int               `json:"ChildCount,omitempty"`
	RecursiveItemCount       int               `json:"RecursiveItemCount,omitempty"`
	IndexNumber              *int              `json:"IndexNumber,omitempty"`
	ParentIndexNumber        *int              `json:"ParentIndexNumber,omitempty"`
	SeriesId                 string            `json:"SeriesId,omitempty"`
	SeriesName               string            `json:"SeriesName,omitempty"`
	SeriesPrimaryImageTag    string            `json:"SeriesPrimaryImageTag,omitempty"`
	SeasonId                 string            `json:"SeasonId,omitempty"`
	SeasonName               string            `json:"SeasonName,omitempty"`
	Album                    string            `json:"Album,omitempty"`
	AlbumId                  string            `json:"AlbumId,omitempty"`
	AlbumArtist              string            `json:"AlbumArtist,omitempty"`
	AlbumArtists             []NameID          `json:"AlbumArtists,omitempty"`
	ArtistItems              []NameID          `json:"ArtistItems,omitempty"`
	Artists                  []string          `json:"Artists,omitempty"`
	AlbumPrimaryImageTag     string            `json:"AlbumPrimaryImageTag,omitempty"`
	ParentPrimaryImageItemId string            `json:"ParentPrimaryImageItemId,omitempty"`
	ParentBackdropItemId     string            `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags  []string          `json:"ParentBackdropImageTags,omitempty"`
	ImageTags                map[string]string `json:"ImageTags,omitempty"`
	BackdropImageTags        []string          `json:"BackdropImageTags,omitempty"`
	PrimaryImageAspectRatio  float64           `json:"PrimaryImageAspectRatio,omitempty"`
	UserData                 *UserData         `json:"UserData,omitempty"`
	MediaSources             []MediaSource     `json:"MediaSources,omitempty"`
	MediaStreams             []MediaStream     `json:"MediaStreams,omitempty"`
}

type NameID struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

type VirtualFolder struct {
	Name           string   `json:"Name"`
	ItemId         string   `json:"ItemId"`
	CollectionType string   `json:"CollectionType"`
	Locations      []string `json:"Locations"`
}

type DisplayPreferences struct {
	Id                 string            `json:"Id"`
	Client             string            `json:"Client"`
	SortBy             string            `json:"SortBy"`
	SortOrder          string            `json:"SortOrder"`
	ScrollDirection    string            `json:"ScrollDirection"`
	ShowBackdrop       bool              `json:"ShowBackdrop"`
	ShowSidebar        bool              `json:"ShowSidebar"`
	RememberIndexing   bool              `json:"RememberIndexing"`
	RememberSorting    bool              `json:"RememberSorting"`
	PrimaryImageHeight int               `json:"PrimaryImageHeight"`
	PrimaryImageWidth  int               `json:"PrimaryImageWidth"`
	CustomPrefs        map[string]string `json:"CustomPrefs"`
}

type ItemsResult struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
	StartIndex       int    `json:"StartIndex"`
}

// UserConfiguration carries every field Finamp requires once it is present.
type UserConfiguration struct {
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	OrderedViews               []string `json:"OrderedViews"`
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	SubtitleMode               string   `json:"SubtitleMode"`
	DisplayCollectionsView     bool     `json:"DisplayCollectionsView"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
	EnableNextEpisodeAutoPlay  bool     `json:"EnableNextEpisodeAutoPlay"`
}

type User struct {
	Id                        string            `json:"Id"`
	Name                      string            `json:"Name"`
	ServerId                  string            `json:"ServerId"`
	HasPassword               bool              `json:"HasPassword"`
	HasConfiguredPassword     bool              `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool              `json:"HasConfiguredEasyPassword"`
	Configuration             UserConfiguration `json:"Configuration"`
	Policy                    UserPolicy        `json:"Policy"`
}

// UserPolicy gates client features: Swiftfin hides Play without
// EnableMediaPlayback, Infuse shows no library without EnableAllFolders,
// and Finamp requires every field once the policy is present.
type UserPolicy struct {
	IsAdministrator                 bool   `json:"IsAdministrator"`
	IsHidden                        bool   `json:"IsHidden"`
	IsDisabled                      bool   `json:"IsDisabled"`
	EnableUserPreferenceAccess      bool   `json:"EnableUserPreferenceAccess"`
	EnableRemoteControlOfOtherUsers bool   `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl       bool   `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess              bool   `json:"EnableRemoteAccess"`
	EnableLiveTvManagement          bool   `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess              bool   `json:"EnableLiveTvAccess"`
	EnableMediaPlayback             bool   `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding  bool   `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding  bool   `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing          bool   `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion           bool   `json:"EnableContentDeletion"`
	EnableContentDownloading        bool   `json:"EnableContentDownloading"`
	EnableSyncTranscoding           bool   `json:"EnableSyncTranscoding"`
	EnableMediaConversion           bool   `json:"EnableMediaConversion"`
	EnableAllDevices                bool   `json:"EnableAllDevices"`
	EnableAllChannels               bool   `json:"EnableAllChannels"`
	EnableAllFolders                bool   `json:"EnableAllFolders"`
	EnablePublicSharing             bool   `json:"EnablePublicSharing"`
	InvalidLoginAttemptCount        int    `json:"InvalidLoginAttemptCount"`
	RemoteClientBitrateLimit        int    `json:"RemoteClientBitrateLimit"`
	SyncPlayAccess                  string `json:"SyncPlayAccess"`
	AuthenticationProviderId        string `json:"AuthenticationProviderId"`
	PasswordResetProviderId         string `json:"PasswordResetProviderId"`
}

type PublicSystemInfo struct {
	Id                     string `json:"Id"`
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
	LocalAddress           string `json:"LocalAddress,omitempty"`
}

type AuthenticationResult struct {
	User        User   `json:"User"`
	AccessToken string `json:"AccessToken"`
	ServerId    string `json:"ServerId"`
}

type AuthenticateByName struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

type PlaybackInfo struct {
	MediaSources  []MediaSource `json:"MediaSources"`
	PlaySessionId string        `json:"PlaySessionId"`
}

// PlaybackReport carries the playing tracks; a subtitle index of -1 means none.
type PlaybackReport struct {
	ItemId              string `json:"ItemId"`
	MediaSourceId       string `json:"MediaSourceId"`
	PositionTicks       int64  `json:"PositionTicks"`
	IsPaused            bool   `json:"IsPaused"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex,omitempty"`
}

type DiscoveryReply struct {
	Address string `json:"Address"`
	Id      string `json:"Id"`
	Name    string `json:"Name"`
}
