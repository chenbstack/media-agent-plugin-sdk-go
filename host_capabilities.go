package pluginsdk

import (
	"context"
	"encoding/json"
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

// SiteAccounts exposes stable site-account operations to plugins. It is a
// domain capability, not a database facade.
type SiteAccounts interface {
	UpsertSiteAccount(ctx context.Context, input SiteAccountWrite) (HostWriteResult, error)
}

type EpisodeSelection struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

type SubscriptionWrite struct {
	TargetID       string             `json:"target_id,omitempty"`
	IdempotencyKey string             `json:"idempotency_key"`
	Media          MediaIdentity      `json:"media"`
	Season         int                `json:"season,omitempty"`
	TotalEpisodes  int                `json:"total_episodes,omitempty"`
	WantedEpisodes []EpisodeSelection `json:"wanted_episodes,omitempty"`
	Status         string             `json:"status,omitempty"`
	SourceName     string             `json:"source_name,omitempty"`
	CreatedAt      string             `json:"created_at,omitempty"`
	Metadata       json.RawMessage    `json:"metadata,omitempty"`
}

// Subscriptions exposes idempotent subscription writes to plugins.
type Subscriptions interface {
	UpsertSubscription(ctx context.Context, input SubscriptionWrite) (HostWriteResult, error)
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
