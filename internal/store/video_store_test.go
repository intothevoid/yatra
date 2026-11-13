package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/karan/travel-diary/internal/domain"
)

func TestVideoStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	trip := domain.Trip{ID: "trip-video", Name: "Video trip", Status: domain.StatusImported}
	if err = s.Trips.Save(ctx, trip); err != nil {
		t.Fatal(err)
	}
	want := domain.Video{ID: "v_test", TripID: trip.ID, SourceFileID: "source", FileName: "clip.mov", Container: "mp4", Codec: "h264", FrameRate: 25, SourcePath: "/data/original.mov", SourceBytes: 4096, SourceContainer: "mov", SourceCodec: "hevc", SourceDuration: 12, SourceWidth: 3840, SourceHeight: 2160, SourceFrameRate: 30, SourceHasAudio: true, SourceCreatedAt: "2026-11-13T00:00:00Z", Rotation: 90, Path: "/data/clip.mp4", PosterPath: "/data/poster.webp", OutputBytes: 1024, PosterBytes: 128, Duration: 6, StartTime: 2, EndTime: 8, Width: 1920, Height: 1080, Profile: "standard", Status: "completed", Selected: true}
	if err = s.Videos.Upsert(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Videos.Get(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got != want {
		t.Fatalf("video round trip mismatch\n got: %#v\nwant: %#v", *got, want)
	}
}
