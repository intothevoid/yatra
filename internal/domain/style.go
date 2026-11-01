package domain

// StyleProfile captures writing style characteristics.
type StyleProfile struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        int      `json:"version"`
	ParagraphLen   string   `json:"paragraph_len"`   // "short", "medium", "long"
	Perspective    string   `json:"perspective"`     // "first_person", "third_person"
	Tone           string   `json:"tone"`            // "warm", "formal", "humorous"
	Humour         string   `json:"humour"`          // "none", "subtle", "witty"
	SectionOrg     string   `json:"section_org"`     // "chronological", "thematic"
	CaptionStyle   string   `json:"caption_style"`   // "descriptive", "minimal"
	Representative string   `json:"representative"`  // excerpt showing style
}
