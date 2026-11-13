package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/karan/travel-diary/internal/config"
)

func videoTools(t *testing.T) *config.Config {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is unavailable")
	}
	vips, _ := exec.LookPath("vips")
	if vips == "" {
		vips = "vips"
	}
	return &config.Config{VideoEnabled: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, VIPSPath: vips, VideoDefaultDurationSec: 6, VideoPosterQuality: 80, VideoStandard: config.VideoProfile{MaxWidth: 1920, MaxHeight: 1080, FPS: 25, CRF: 24, Preset: "veryfast"}, VideoLightweight: config.VideoProfile{MaxWidth: 1280, MaxHeight: 720, FPS: 25, CRF: 26, Preset: "veryfast"}}
}

func requireVips(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("vips"); err != nil {
		t.Skip("vips is unavailable; WebP poster integration test skipped")
	}
}

func createVideoFixture(t *testing.T, cfg *config.Config, size string, withAudio bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.mkv")
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=" + size + ":rate=30"}
	if withAudio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100")
	}
	args = append(args, "-t", "2", "-c:v", "ffv1", "-pix_fmt", "yuv444p")
	if withAudio {
		args = append(args, "-c:a", "pcm_s16le", "-shortest")
	} else {
		args = append(args, "-an")
	}
	args = append(args, path)
	if output, err := exec.Command(cfg.FFmpegPath, args...).CombinedOutput(); err != nil {
		t.Fatalf("create ffmpeg fixture: %v: %s", err, output)
	}
	return path
}

func TestVideoScaleFilterOddLandscapePortraitAndSquare(t *testing.T) {
	cfg := videoTools(t)
	for _, size := range []string{"1280x853", "320x481", "481x481"} {
		t.Run(size, func(t *testing.T) {
			source := createVideoFixture(t, cfg, size, false)
			output := filepath.Join(t.TempDir(), "out.mp4")
			filter := videoScaleFilter(cfg.VideoLightweight)
			args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", source, "-map", "0:v:0", "-an", "-vf", filter, "-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-tag:v", "avc1", output}
			if out, err := exec.Command(cfg.FFmpegPath, args...).CombinedOutput(); err != nil {
				t.Fatalf("transcode odd-sized source: %v: %s", err, out)
			}
			probe, err := probeVideo(context.Background(), cfg, output)
			if err != nil {
				t.Fatal(err)
			}
			if probe.Width%2 != 0 || probe.Height%2 != 0 {
				t.Fatalf("odd output resolution %dx%d", probe.Width, probe.Height)
			}
			if probe.Width > cfg.VideoLightweight.MaxWidth || probe.Height > cfg.VideoLightweight.MaxHeight {
				t.Fatalf("exceeded profile dimensions: %dx%d", probe.Width, probe.Height)
			}
			if probe.HasAudio {
				t.Fatal("output contains audio")
			}
		})
	}
}

func TestTranscodeVideoEvenDimensionsSilencePosterAndTrim(t *testing.T) {
	requireVips(t)
	cfg := videoTools(t)
	source := createVideoFixture(t, cfg, "1280x853", true)
	dir := t.TempDir()
	output := filepath.Join(dir, "out.mp4")
	poster := filepath.Join(dir, "out.webp")
	result, err := transcodeVideo(context.Background(), cfg, source, output, poster, VideoTrim{Start: .25, End: 1.25}, "standard")
	if err != nil {
		t.Fatal(err)
	}
	if result.Duration < .99 || result.Duration > 1.01 {
		t.Fatalf("duration %.3f, want 1 second", result.Duration)
	}
	if result.Source.Width != 1280 || result.Source.Height != 853 || !result.Source.HasAudio {
		t.Fatalf("source metadata not detected: %+v", result.Source)
	}
	if result.Processed.HasAudio || result.Processed.Codec != "h264" {
		t.Fatalf("unexpected output metadata: %+v", result.Processed)
	}
	if result.Processed.Width%2 != 0 || result.Processed.Height%2 != 0 {
		t.Fatalf("output dimensions are odd: %dx%d", result.Processed.Width, result.Processed.Height)
	}
	if _, err := os.Stat(poster); err != nil {
		t.Fatalf("poster missing: %v", err)
	}
	if result.VideoBytes <= 0 || result.PosterBytes <= 0 {
		t.Fatalf("invalid output sizes: %+v", result)
	}
}

func TestTranscodeVideoPortraitAndLightweightDoesNotUpscale(t *testing.T) {
	requireVips(t)
	cfg := videoTools(t)
	source := createVideoFixture(t, cfg, "320x481", false)
	dir := t.TempDir()
	result, err := transcodeVideo(context.Background(), cfg, source, filepath.Join(dir, "out.mp4"), filepath.Join(dir, "poster.webp"), VideoTrim{End: 1}, "lightweight")
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed.Width > result.Source.Width || result.Processed.Height > result.Source.Height {
		t.Fatalf("video was upscaled: source %dx%d output %dx%d", result.Source.Width, result.Source.Height, result.Processed.Width, result.Processed.Height)
	}
	if result.Processed.Width%2 != 0 || result.Processed.Height%2 != 0 {
		t.Fatalf("output dimensions are odd: %dx%d", result.Processed.Width, result.Processed.Height)
	}
	if result.Processed.HasAudio {
		t.Fatal("silent input unexpectedly has audio")
	}
}

func TestTranscodeVideoRejectsInvalidTrimAndCorruptInput(t *testing.T) {
	cfg := videoTools(t)
	source := createVideoFixture(t, cfg, "640x480", false)
	dir := t.TempDir()
	if _, err := transcodeVideo(context.Background(), cfg, source, filepath.Join(dir, "bad.mp4"), filepath.Join(dir, "bad.webp"), VideoTrim{Start: 1, End: 1}, "standard"); err == nil {
		t.Fatal("expected invalid trim error")
	}
	corrupt := filepath.Join(dir, "corrupt.mov")
	if err := os.WriteFile(corrupt, []byte("not a video"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transcodeVideo(context.Background(), cfg, corrupt, filepath.Join(dir, "corrupt.mp4"), filepath.Join(dir, "corrupt.webp"), VideoTrim{}, "standard"); err == nil {
		t.Fatal("expected corrupt input error")
	}
}
