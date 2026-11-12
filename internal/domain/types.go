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
	// Batch keeps the batch panel's choices between launches.
	Batch BatchPreferences `json:"batch,omitzero"`
}

// BatchPreferences are the batch panel's remembered choices. Zero values
// mean the defaults: two requests at a time, sizes following each source.
type BatchPreferences struct {
	// Concurrency is how many requests run at once, 1 to 16.
	Concurrency int `json:"concurrency,omitempty"`
	// SizeMode is "source" to follow each image's aspect, or "fixed" to
	// use Ratio for every image.
	SizeMode string `json:"sizeMode,omitempty"`
	Ratio    string `json:"ratio,omitempty"`
	// OutputDirectory receives a copy of every result; empty keeps
	// results in the library only.
	OutputDirectory string `json:"outputDirectory,omitempty"`
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
	// BatchID names the batch this work came from, if any.
	BatchID string `json:"batchId,omitempty"`
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
	// BatchID is recorded on the saved work; it does not change the request.
	BatchID string `json:"batchId,omitempty"`
}

// Batch is one batch run: its settings and every source image, kept so the
// library can show it as one card and reopen it after a restart.
type Batch struct {
	ID              string `json:"id"`
	CreatedAt       int64  `json:"createdAt"`
	Prompt          string `json:"prompt"`
	ProviderID      string `json:"providerId"`
	ProviderName    string `json:"providerName"`
	Model           string `json:"model"`
	SizeMode        string `json:"sizeMode"`
	Ratio           string `json:"ratio"`
	Resolution      string `json:"resolution"`
	OutputFormat    string `json:"outputFormat,omitempty"`
	Concurrency     int    `json:"concurrency"`
	OutputDirectory string `json:"outputDirectory,omitempty"`
	// SharedNames and SharedFiles are the shared references, sent after
	// each source; files are names in the data directory's batch folder.
	SharedNames []string    `json:"sharedNames"`
	SharedFiles []string    `json:"sharedFiles"`
	Items       []BatchItem `json:"items"`
}

// BatchItem is one source image of a batch and what became of it.
type BatchItem struct {
	ID         string `json:"id"`
	SourcePath string `json:"sourcePath"`
	Name       string `json:"name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	// Size is the size requested for this image.
	Size string `json:"size"`
	// Status is queued, done, error or canceled. Queued items found after a
	// restart were interrupted.
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	RecordID   string `json:"recordId,omitempty"`
	OutputPath string `json:"outputPath,omitempty"`
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
	SaveBatchPreferences(BatchPreferences) (Config, error)
	// ListBatches returns every batch, newest first.
	ListBatches() ([]Batch, error)
	// CreateBatch saves a new batch and its shared references, and returns
	// it with its ID, time and stored reference files.
	CreateBatch(batch Batch, shared [][]byte) (Batch, error)
	// UpdateBatchItems replaces the items of batch id with the same IDs.
	UpdateBatchItems(id string, items []BatchItem) error
	// ReadBatchReference returns shared reference index of batch id.
	ReadBatchReference(id string, index int) ([]byte, error)
	// DeleteBatch removes a batch, its shared references and its works.
	DeleteBatch(id string) error
	// PlanGeneration validates args as Generate does and returns the
	// request it would send, with the API key masked.
	PlanGeneration(GenerateArgs) (RequestPlan, error)
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
	Ready              bool
}

// UpdateDraft restores the composer after an unattended update restart.
type UpdateDraft struct {
	Prompt       string
	References   []Reference
	Preferences  GenerationPreferences
	Size, Markup string
	FailedJobs   []UpdateFailedJob
}

type UpdateFailedJob struct {
	Args         GenerateArgs
	Progress     Progress
	ProviderName string
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
	InstallUpdate(context.Context) error
	Relaunch()
	SaveUpdateDraft(UpdateDraft) error
	LoadUpdateDraft() (*UpdateDraft, error)
	ClearUpdateDraft() error
	// OpenBatchImages asks for images to process; none when canceled.
	OpenBatchImages() ([]string, error)
	// CollectImages returns the image files among paths and directly inside
	// the folders among them, in order and without repeats.
	CollectImages(paths []string) ([]string, error)
	// SaveBatchResult copies artwork id into dir, named after stem, and
	// never replaces a file. It returns the path written.
	SaveBatchResult(id, dir, stem string) (string, error)
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
