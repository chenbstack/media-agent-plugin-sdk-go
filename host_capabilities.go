package pluginsdk

import (
	"context"
	"encoding/json"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// HostWriteResult is returned by typed host capability writes. TargetID is
// stable and can be persisted by a plugin for later idempotent updates.
type HostWriteResult struct {
	TargetID string `json:"target_id"`
	Change   string `json:"change"` // created / updated / unchanged
}

// MediaIdentity identifies media without exposing the host database schema.
type MediaIdentity struct {
	MediaType     string `json:"media_type"` // movie / series
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          int    `json:"year,omitempty"`
	TMDBID        int64  `json:"tmdb_id,omitempty"`
	IMDBID        string `json:"imdb_id,omitempty"`
	TVDBID        int64  `json:"tvdb_id,omitempty"`
	DoubanID      string `json:"douban_id,omitempty"`
	PosterURL     string `json:"poster_url,omitempty"`
	BackdropURL   string `json:"backdrop_url,omitempty"`
	Overview      string `json:"overview,omitempty"`
}

type SiteAccountWrite struct {
	TargetID       string          `json:"target_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Name           string          `json:"name"`
	BaseURL        string          `json:"base_url"`
	Enabled        bool            `json:"enabled"`
	UserAgent      string          `json:"user_agent,omitempty"`
	Cookie         string          `json:"cookie,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

// SiteAccountInfo is a read-only snapshot of one host site account. Profile is
// the latest user data synced by the host (nil when never synced); credentials
// are never included.
type SiteAccountInfo struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	BaseURL         string                 `json:"base_url"`
	Enabled         bool                   `json:"enabled"`
	Profile         *providers.SiteProfile `json:"profile,omitempty"`
	ProfileSyncedAt string                 `json:"profile_synced_at,omitempty"`
}

// SiteAccounts exposes stable site-account operations to plugins. It is a
// domain capability, not a database facade.
type SiteAccounts interface {
	UpsertSiteAccount(ctx context.Context, input SiteAccountWrite) (HostWriteResult, error)
	// ListSiteAccounts returns every site account with its latest synced
	// profile. Requires host permission "site.accounts.read".
	ListSiteAccounts(ctx context.Context) ([]SiteAccountInfo, error)
}

type EpisodeSelection struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

type SubscriptionWrite struct {
	TargetID                string             `json:"target_id,omitempty"`
	IdempotencyKey          string             `json:"idempotency_key"`
	Media                   MediaIdentity      `json:"media"`
	Season                  int                `json:"season,omitempty"`
	TotalEpisodes           int                `json:"total_episodes,omitempty"`
	WantedEpisodes          []EpisodeSelection `json:"wanted_episodes,omitempty"`
	Status                  string             `json:"status,omitempty"`
	RuleProfileID           string             `json:"rule_profile_id,omitempty"`
	RuleProfileName         string             `json:"rule_profile_name,omitempty"`
	RuleProfileKey          string             `json:"rule_profile_key,omitempty"`
	Mode                    string             `json:"mode,omitempty"`
	UpgradeStrategy         string             `json:"upgrade_strategy,omitempty"`
	UpgradeThreshold        string             `json:"upgrade_threshold,omitempty"`
	SubscriptionSites       []string           `json:"subscription_sites,omitempty"`
	ReleaseGroups           []string           `json:"release_groups,omitempty"`
	AutoSubscribeNewSeasons bool               `json:"auto_subscribe_new_seasons,omitempty"`
	DownloaderName          string             `json:"downloader_name,omitempty"`
	MediaServerName         string             `json:"media_server_name,omitempty"`
	SavePath                string             `json:"save_path,omitempty"`
	SourceName              string             `json:"source_name,omitempty"`
	CreatedAt               string             `json:"created_at,omitempty"`
	Metadata                json.RawMessage    `json:"metadata,omitempty"`
}

// Subscriptions exposes idempotent subscription writes to plugins.
type Subscriptions interface {
	UpsertSubscription(ctx context.Context, input SubscriptionWrite) (HostWriteResult, error)
}

// RuleDimension selects ordered values from one host rule dimension. Values
// inside a dimension are alternatives ordered by preference; different
// prerequisite dimensions must all match.
type RuleDimension struct {
	ID       string   `json:"id"`
	Selected []string `json:"selected"`
}

// RuleSizeRange constrains the candidate size in GiB. Nil bounds are open.
type RuleSizeRange struct {
	MinGB *float64 `json:"min_gb,omitempty"`
	MaxGB *float64 `json:"max_gb,omitempty"`
}

// RulePrerequisites contains portable constraints used to decide whether a
// candidate can enter a rule profile. Keyword patterns use RE2 syntax.
type RulePrerequisites struct {
	Dimensions             []RuleDimension `json:"dimensions,omitempty"`
	Size                   RuleSizeRange   `json:"size,omitempty"`
	MinSeeders             *int            `json:"min_seeders,omitempty"`
	MinAgeMinutes          *int            `json:"min_age_minutes,omitempty"`
	MaxAgeMinutes          *int            `json:"max_age_minutes,omitempty"`
	IncludeKeywords        []string        `json:"include_keywords,omitempty"`
	ExcludeKeywords        []string        `json:"exclude_keywords,omitempty"`
	IncludeKeywordPattern  string          `json:"include_keyword_pattern,omitempty"`
	ExcludeKeywordPattern  string          `json:"exclude_keyword_pattern,omitempty"`
	IncludePatternAdvanced bool            `json:"include_pattern_advanced,omitempty"`
	ExcludePatternAdvanced bool            `json:"exclude_pattern_advanced,omitempty"`
}

// RuleProfileWrite is the host-neutral representation of one rule profile.
// IdempotencyKey is scoped to the calling plugin and can also be referenced by
// SubscriptionWrite.RuleProfileKey.
type RuleProfileWrite struct {
	TargetID        string            `json:"target_id,omitempty"`
	IdempotencyKey  string            `json:"idempotency_key"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Status          string            `json:"status,omitempty"`
	Priority        int               `json:"priority,omitempty"`
	MediaType       string            `json:"media_type,omitempty"`
	MediaCategory   string            `json:"media_category,omitempty"`
	MatchConditions []string          `json:"match_conditions,omitempty"`
	Prerequisites   RulePrerequisites `json:"prerequisites,omitempty"`
	Preferences     []RuleDimension   `json:"preferences,omitempty"`
	Fallback        string            `json:"fallback,omitempty"`
}

type RuleDimensionDefinition struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Options []string `json:"options"`
}

// RuleCatalog describes the rule vocabulary supported by the current host.
type RuleCatalog struct {
	Dimensions      []RuleDimensionDefinition `json:"dimensions"`
	SortOptions     []string                  `json:"sort_options"`
	IncludeKeywords []string                  `json:"include_keywords,omitempty"`
	ExcludeKeywords []string                  `json:"exclude_keywords,omitempty"`
}

type RuleSortWrite struct {
	Selected []string `json:"selected"`
}

type RuleSortResult struct {
	Selected []string `json:"selected"`
}

// RuleDefaultWrite selects a plugin-owned or existing rule profile as the
// default for a host workflow. Supported scopes are host-defined and returned
// as an error when unavailable.
type RuleDefaultWrite struct {
	Scope           string `json:"scope"`
	RuleProfileID   string `json:"rule_profile_id,omitempty"`
	RuleProfileName string `json:"rule_profile_name,omitempty"`
	RuleProfileKey  string `json:"rule_profile_key,omitempty"`
}

type RuleDefaultResult struct {
	Scope         string `json:"scope"`
	RuleProfileID string `json:"rule_profile_id"`
}

// Rules exposes stable rule-profile operations without exposing persistence
// tables or a source-system-specific rule language.
type Rules interface {
	GetRuleCatalog(ctx context.Context) (RuleCatalog, error)
	UpsertRuleProfile(ctx context.Context, input RuleProfileWrite) (HostWriteResult, error)
	SetRuleSort(ctx context.Context, input RuleSortWrite) (RuleSortResult, error)
	SetRuleDefault(ctx context.Context, input RuleDefaultWrite) (RuleDefaultResult, error)
}

type DownloadFileWrite struct {
	Index         int    `json:"index"`
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	Selected      bool   `json:"selected"`
	SeasonNumber  int    `json:"season_number,omitempty"`
	EpisodeNumber int    `json:"episode_number,omitempty"`
}

type DownloadWrite struct {
	TargetID       string              `json:"target_id,omitempty"`
	IdempotencyKey string              `json:"idempotency_key"`
	DownloaderName string              `json:"downloader_name,omitempty"`
	ExternalHash   string              `json:"external_hash,omitempty"`
	Name           string              `json:"name"`
	Status         string              `json:"status"`
	SavePath       string              `json:"save_path,omitempty"`
	AddedAt        string              `json:"added_at,omitempty"`
	CompletedAt    string              `json:"completed_at,omitempty"`
	Files          []DownloadFileWrite `json:"files,omitempty"`
	Metadata       json.RawMessage     `json:"metadata,omitempty"`
}

// Downloads exposes download-task operations independently of any downloader
// implementation or import source.
type Downloads interface {
	UpsertDownload(ctx context.Context, input DownloadWrite) (HostWriteResult, error)
	FindDownloadByHash(ctx context.Context, hash string) (HostWriteResult, bool, error)
}

type TransferWrite struct {
	TargetID       string          `json:"target_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Media          MediaIdentity   `json:"media"`
	DownloadHash   string          `json:"download_hash,omitempty"`
	SourcePath     string          `json:"source_path,omitempty"`
	TargetPath     string          `json:"target_path,omitempty"`
	Operation      string          `json:"operation"`
	Status         string          `json:"status"`
	Error          string          `json:"error,omitempty"`
	SizeBytes      int64           `json:"size_bytes,omitempty"`
	SeasonNumber   int             `json:"season_number,omitempty"`
	EpisodeNumber  int             `json:"episode_number,omitempty"`
	OccurredAt     string          `json:"occurred_at,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

// Transfers exposes completed or failed organization records to plugins.
type Transfers interface {
	UpsertTransfer(ctx context.Context, input TransferWrite) (HostWriteResult, error)
}
