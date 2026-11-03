package domain

import (
	"testing"
)

func TestSourceFileIsSupportedImage(t *testing.T) {
	tests := []struct {
		mime string
		want bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/heic", true},
		{"image/heif", true},
		{"image/gif", false},
		{"text/plain", false},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			sf := SourceFile{MIME: tt.mime}
			got := sf.IsSupportedImage()
			if got != tt.want {
				t.Errorf("IsSupportedImage(%q) = %v, want %v", tt.mime, got, tt.want)
			}
		})
	}
}

func TestGenerateSourceFileID(t *testing.T) {
	id := GenerateSourceFileID("trip_1", "sample.jpg")
	if id != "trip_1_sample.jpg" {
		t.Errorf("GenerateSourceFileID() = %q, want %q", id, "trip_1_sample.jpg")
	}
}
