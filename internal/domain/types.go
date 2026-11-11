package domain

import (
	"context"
	"time"
)

type Provider struct {
	// Disabled keeps a provider's settings while hiding its models from the
	// studio; providers saved before this field existed stay enabled.
	Disabled          bool     `json:"disabled,omitempty"`
	DefaultResolution string   `json:"defaultResolution,omitempty"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	BaseURL           string   `json:"baseUrl"`
	APIKey            string   `json:"apiKey"`
	Models            []string `json:"models"`
	Resolutions       []string `json:"resolutions"`
	GenerationsPath   string   `json:"generationsPath"`
	EditsPath         string   `json:"editsPath"`
}
type ProxyConfig struct {
	Mode   string `json:"mode"`
	URL    string `json:"url"`
	Bypass string `json:"bypass"`
}
type GenerationPreferences struct {
	GIFFrameCount      int    `json:"gifFrameCount,omitempty"`
	GIFFrameDurationMS int    `json:"gifFrameDurationMs,omitempty"`
	GIFSize            string `json:"gifSize,omitempty"`
	OutputFormat       string `json:"outputFormat,omitempty"`
	ProviderID         string `json:"providerId"`
	Model              string `json:"model"`
	Ratio              string `json:"ratio"`
	Resolution         string `json:"resolution"`
}
type Config struct {
	GenerationPreferences GenerationPreferences `json:"generationPreferences"`
	Providers             []Provider            `json:"providers"`
	Proxy                 ProxyConfig           `json:"proxy"`
	Theme                 string                `json:"theme"`
	// Language is "system" (or empty) to follow the computer's language,
	// else a supported tag: "zh-Hans" or "en".
	Language string `json:"language,omitempty"`
	// ImageDirectory holds new works and their references when set; empty
	// keeps them in the data directory. Earlier choices stay readable so
	// works saved there still open and can be deleted.
	ImageDirectory           string   `json:"imageDirectory,omitempty"`
	PreviousImageDirectories []string `json:"previousImageDirectories,omitempty"`
}
type ImageRecord struct {
	// Requested GIF settings. Actual frame count and timing come from the file.
	GIFFrameCount      int      `json:"gifFrameCount,omitempty"`
	GIFFrameDurationMS int      `json:"gifFrameDurationMs,omitempty"`
	ID                 string   `json:"id"`
	Prompt             string   `json:"prompt"`
	ProviderID         string   `json:"providerId"`
	ProviderName       string   `json:"providerName"`
	Model              string   `json:"model"`
	Size               string   `json:"size"`
	CreatedAt          int64    `json:"createdAt"`
	ImagePath          string   `json:"imagePath"`
	MIMEType           string   `json:"mimeType"`
	Width              int      `json:"width"`
	Height             int      `json:"height"`
	ReferenceCount     int      `json:"referenceCount"`
	ReferencePaths     []string `json:"referencePaths"`
	Favorite           bool     `json:"favorite"`
}
type HistoryQuery struct {
	Page          int
	PageSize      int
	Search        string
	FavoritesOnly bool
	// All returns every matching record in one page, for the native
	// virtualized gallery; Page and PageSize are then ignored.
	All bool
}
type HistoryPage struct {
	Items         []ImageRecord `json:"items"`
	Total         int           `json:"total"`
	Page          int           `json:"page"`
	PageSize      int           `json:"pageSize"`
	TotalPages    int           `json:"totalPages"`
	FavoriteCount int           `json:"favoriteCount"`
}
type Progress struct {
	JobID    string `json:"jobId"`
	Progress int    `json:"progress"`
	Stage    string `json:"stage"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}
type Reference struct {
	ID      string
	Name    string
	DataURL string
}
type GenerateArgs struct {
	// Markup is a transparent PNG overlay aligned to the single source image.
	Markup          string   `json:"markup,omitempty"`
	FrameCount      int      `json:"frameCount,omitempty"`
	FrameDurationMS int      `json:"frameDurationMs,omitempty"`
	OutputFormat    string   `json:"outputFormat,omitempty"`
	JobID           string   `json:"jobId"`
	Prompt          string   `json:"prompt"`
	ProviderID      string   `json:"providerId"`
	Model           string   `json:"model"`
	Size            string   `json:"size"`
	ReferenceImages []string `json:"referenceImages"`
}
type ModelPricing struct {
	Model    string             `json:"model"`
	Currency string             `json:"currency"`
	Prices   map[string]float64 `json:"prices"`
}

// Store serializes read/modify/write operations and never clears corrupt data.
type Store interface {
	Root() string
	ReadConfig() (Config, error)
	UpdateConfig(func(*Config) error) (Config, error)
	ReadHistory() ([]ImageRecord, error)
	ChangeHistory(func(*[]ImageRecord) error) error
}

// Backend is the complete application capability surface consumed by native UI.
type Backend interface {
	LoadConfig() (Config, error)
	SaveProviders([]Provider) (Config, error)
	SaveProxy(ProxyConfig) (Config, error)
	SaveTheme(string) (Config, error)
	SaveLanguage(string) (Config, error)
	SaveImageDirectory(string) (Config, error)
	SavePreferences(GenerationPreferences) (Config, error)
	DiscoverModels(context.Context, Provider) ([]string, error)
	DiscoverPricing(context.Context, string) ([]ModelPricing, error)
	Generate(context.Context, GenerateArgs, func(Progress)) (ImageRecord, error)
	ListHistory(HistoryQuery) (HistoryPage, error)
	Favorite(string, bool) (ImageRecord, error)
	Delete(string) error
	ReadImage(string) ([]byte, error)
	// ReadThumbnail returns an encoded image no larger than w×h, cached on disk.
	ReadThumbnail(id string, w, h int) ([]byte, error)
	ReadReference(string, int) ([]byte, error)
}

// UpdateStatus is a snapshot of the native updater for the preferences UI.
type UpdateStatus struct {
	Version            string
	Supported          bool
	AutomaticChecks    bool
	AutomaticDownloads bool
	AvailableVersion   string
	LastCheck          time.Time
}

// Desktop contains OS operations only; UI never receives arbitrary disk access.
type Desktop interface {
	OpenImages(remaining int) ([]Reference, error)
	PasteImages(remaining int) ([]Reference, error)
	ImportFiles(paths []string, remaining int) ([]Reference, error)
	CopyImage(string) error
	CopyText(string) error
	ExportImage(string) (bool, error)
	SetTheme(string)
	// SetLanguage shows the language of tag, "system" for the computer's,
	// and returns the language the studio now shows.
	SetLanguage(tag string) string
	DataDir() string
	// ChooseDirectory asks for a folder; ok is false when the user cancels.
	ChooseDirectory(title, current string) (path string, ok bool, err error)
	UpdateStatus() UpdateStatus
	SetUpdatePreferences(checks, downloads bool) (UpdateStatus, error)
	CheckForUpdates() error
}

// RequestField is one form or JSON field of a planned generation request.
type RequestField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RequestImage describes one uploaded image without its bytes.
type RequestImage struct {
	Field    string `json:"field"`
	FileName string `json:"fileName"`
	MIMEType string `json:"mimeType"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Bytes    int    `json:"bytes"`
}

// RequestPlan is exactly what Generate sends for some arguments, with the
// API key masked and image bytes summarised.
type RequestPlan struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	ContentType string         `json:"contentType"`
	APIKey      string         `json:"apiKey"`
	Fields      []RequestField `json:"fields"`
	Images      []RequestImage `json:"images"`
}
