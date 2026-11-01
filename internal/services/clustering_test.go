package services

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

func TestHammingDistance(t *testing.T) {
	tests := []struct {
		a, b uint64
		want int
	}{
		{0, 0, 0},
		{0xFFFFFFFFFFFFFFFF, 0, 64},
		{0, 0xFFFFFFFFFFFFFFFF, 64},
		{0x8000000000000000, 0, 1},
		{0x0000000000000001, 0, 1},
		{0xFF, 0x0F, 4},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := hammingDistance(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("hammingDistance(0x%016x, 0x%016x) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestPerceptualHashConsistent(t *testing.T) {
	img := createTestImage(t)
	defer os.Remove(img)

	ctx := context.Background()
	h1, err := perceptualHash(ctx, img)
	if err != nil {
		t.Fatalf("perceptualHash: %v", err)
	}
	h2, err := perceptualHash(ctx, img)
	if err != nil {
		t.Fatalf("perceptualHash second call: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash not consistent: 0x%016x vs 0x%016x", h1, h2)
	}
}

func TestFindExactDuplicates(t *testing.T) {
	c := &Clusterer{}
	photos := []domain.Photo{
		{ID: "p1", SourceFileID: "trip1_file_a.jpg"},
		{ID: "p2", SourceFileID: "trip1_file_a.jpg"},
		{ID: "p3", SourceFileID: "trip1_file_b.jpg"},
	}
	groups := c.findExactDuplicates(photos)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if len(groups[0].Photos) != 2 {
		t.Errorf("group has %d photos, want 2", len(groups[0].Photos))
	}
}

func TestMergeGroups(t *testing.T) {
	exact := []store.PhotoGroup{
		{Type: "duplicate", Photos: []string{"p1", "p2"}},
	}
	similar := []store.PhotoGroup{
		{Type: "similar", Photos: []string{"p2", "p3"}},
		{Type: "similar", Photos: []string{"p4", "p5"}},
	}
	result := mergeGroups(exact, similar, "trip1")

	for _, g := range result {
		if g.Type == "similar" {
			for _, id := range g.Photos {
				if id == "p2" {
					t.Error("p2 should not appear in similar groups")
				}
			}
		}
	}

	if len(result) != 2 {
		t.Errorf("got %d groups, want 2", len(result))
	}
}

func createTestImage(t *testing.T) string {
	t.Helper()
	// Create a simple 10x10 red PNG
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	f, err := os.CreateTemp("", "test-img-*.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}
