package domain

// Photo represents a processed image with metadata, derivatives, and AI analysis.
type Photo struct {
	ID             string      `json:"id"`
	SourceFileID   string      `json:"source_file_id"`
	EXIF           string      `json:"exif"`              // JSON from exiftool
	ThumbnailPath  string      `json:"thumbnail_path"`    // 400px long edge
	PreviewPath    string      `json:"preview_path"`      // 1200px long edge
	ExportPath     string      `json:"export_path"`       // 2560px long edge
	VLMResult      string      `json:"vlm_result"`        // JSON: VisionResult
	Status         PhotoStatus `json:"status"`
	RankingReasons string      `json:"ranking_reasons"`   // JSON: explainable factors
}

// PhotoStatus values from spec §6.
type PhotoStatus string

const (
	PhotoStatusUnknown     PhotoStatus = "unknown"
	PhotoStatusRecommended PhotoStatus = "recommended"
	PhotoStatusReview      PhotoStatus = "review"
	PhotoStatusNotSelected PhotoStatus = "not_selected"
)

// VisionResult is the structured output from the VLM adapter, matching spec §6.
type VisionResult struct {
	SchemaVersion int            `json:"schema_version"`
	PhotoID       string         `json:"photo_id"`
	Observed      ObservedResult `json:"observed"`
	Hypotheses    []Hypothesis   `json:"hypotheses"`
	QualityFlags  []string       `json:"quality_flags"`
	SafetyFlags   []string       `json:"safety_flags"`
}

// ObservedResult contains factual observations from the image.
type ObservedResult struct {
	Description  string   `json:"description"`
	Subjects     []string `json:"subjects"`
	Category     string   `json:"category"`
	VisibleText  []string `json:"visible_text"`
}

// Hypothesis is a candidate interpretation with heuristic confidence.
type Hypothesis struct {
	Type       string   `json:"type"`       // "place", "food", "landmark"
	Value      string   `json:"value"`
	Confidence float32  `json:"confidence"` // heuristic, not calibrated
}
