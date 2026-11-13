package domain

// Video is a web-optimized, silent clip associated with an uploaded source file.
type Video struct {
	ID              string  `json:"id"`
	TripID          string  `json:"trip_id"`
	SourceFileID    string  `json:"source_file_id"`
	FileName        string  `json:"file_name"`
	Container       string  `json:"container"`
	Codec           string  `json:"codec"`
	FrameRate       float64 `json:"frame_rate"`
	SourcePath      string  `json:"-"`
	SourceBytes     int64   `json:"source_bytes"`
	SourceContainer string  `json:"source_container"`
	SourceCodec     string  `json:"source_codec"`
	SourceDuration  float64 `json:"source_duration"`
	SourceWidth     int     `json:"source_width"`
	SourceHeight    int     `json:"source_height"`
	SourceFrameRate float64 `json:"source_frame_rate"`
	SourceHasAudio  bool    `json:"source_has_audio"`
	SourceCreatedAt string  `json:"source_created_at,omitempty"`
	Rotation        int     `json:"rotation"`
	Path            string  `json:"-"`
	PosterPath      string  `json:"-"`
	OutputBytes     int64   `json:"output_bytes"`
	PosterBytes     int64   `json:"poster_bytes"`
	URL             string  `json:"url,omitempty"`
	PosterURL       string  `json:"poster_url,omitempty"`
	Duration        float64 `json:"duration"`
	StartTime       float64 `json:"start_time"`
	EndTime         float64 `json:"end_time"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	HasAudio        bool    `json:"has_audio"`
	Profile         string  `json:"profile"`
	Status          string  `json:"status"`
	Error           string  `json:"error,omitempty"`
	Selected        bool    `json:"selected"`
}
