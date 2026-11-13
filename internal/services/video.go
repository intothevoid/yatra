package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karan/travel-diary/internal/config"
)

type VideoTrim struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type VideoProbe struct {
	Container    string
	Codec        string
	Duration     float64
	Width        int
	Height       int
	FrameRate    float64
	HasAudio     bool
	Rotation     int
	CreationTime string
}

type VideoProcessResult struct {
	Source      VideoProbe
	Processed   VideoProbe
	Duration    float64
	VideoBytes  int64
	PosterBytes int64
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		Tags         struct {
			Rotate string `json:"rotate"`
		} `json:"tags"`
		SideData []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
}

func probeVideo(ctx context.Context, cfg *config.Config, path string) (VideoProbe, error) {
	cmd := exec.CommandContext(ctx, cfg.FFprobePath, "-v", "error", "-show_streams", "-show_format", "-of", "json", path)
	data, err := cmd.Output()
	if err != nil {
		return VideoProbe{}, fmt.Errorf("inspect video with ffprobe: %w", err)
	}
	var out ffprobeOutput
	if err = json.Unmarshal(data, &out); err != nil {
		return VideoProbe{}, fmt.Errorf("read ffprobe metadata: %w", err)
	}
	meta := VideoProbe{Container: out.Format.FormatName, CreationTime: out.Format.Tags["creation_time"]}
	meta.Duration, _ = strconv.ParseFloat(out.Format.Duration, 64)
	for _, stream := range out.Streams {
		switch stream.CodecType {
		case "audio":
			meta.HasAudio = true
		case "video":
			if meta.Codec == "" {
				meta.Codec = stream.CodecName
				meta.Width = stream.Width
				meta.Height = stream.Height
				meta.FrameRate = parseFrameRate(stream.AvgFrameRate)
				if stream.Tags.Rotate != "" {
					meta.Rotation, _ = strconv.Atoi(stream.Tags.Rotate)
				}
				if len(stream.SideData) > 0 && stream.SideData[0].Rotation != 0 {
					meta.Rotation = int(stream.SideData[0].Rotation)
				}
			}
		}
	}
	if meta.Width <= 0 || meta.Height <= 0 || meta.Duration <= 0 {
		return VideoProbe{}, fmt.Errorf("file has no valid video stream or duration")
	}
	if meta.Rotation == 90 || meta.Rotation == 270 || meta.Rotation == -90 || meta.Rotation == -270 {
		meta.Width, meta.Height = meta.Height, meta.Width
	}
	return meta, nil
}

func parseFrameRate(value string) float64 {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		n, _ := strconv.ParseFloat(parts[0], 64)
		d, _ := strconv.ParseFloat(parts[1], 64)
		if d != 0 {
			return n / d
		}
	}
	n, _ := strconv.ParseFloat(value, 64)
	return n
}

func transcodeVideo(ctx context.Context, cfg *config.Config, source, destination, poster string, trim VideoTrim, profileName string) (VideoProcessResult, error) {
	if !cfg.VideoEnabled {
		return VideoProcessResult{}, fmt.Errorf("video processing is disabled")
	}
	sourceMeta, err := probeVideo(ctx, cfg, source)
	if err != nil {
		return VideoProcessResult{}, err
	}
	if trim.Start < 0 || trim.Start >= sourceMeta.Duration {
		return VideoProcessResult{}, fmt.Errorf("trim start must be between 0 and the source duration")
	}
	end := trim.End
	if end <= 0 {
		end = cfg.VideoDefaultDurationSec
		if end <= 0 || end > sourceMeta.Duration {
			end = sourceMeta.Duration
		}
	}
	if end > sourceMeta.Duration {
		return VideoProcessResult{}, fmt.Errorf("trim end exceeds source duration")
	}
	if end <= trim.Start {
		return VideoProcessResult{}, fmt.Errorf("trim end must be after trim start and within the source duration")
	}
	profile := cfg.VideoStandard
	if profileName == "lightweight" {
		profile = cfg.VideoLightweight
	} else if profileName != "standard" {
		return VideoProcessResult{}, fmt.Errorf("profile must be standard or lightweight")
	}
	if profile.MaxWidth < 2 || profile.MaxHeight < 2 || profile.FPS < 1 || profile.CRF < 0 || profile.CRF > 51 || profile.Preset == "" {
		return VideoProcessResult{}, fmt.Errorf("invalid %s video profile settings", profileName)
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return VideoProcessResult{}, err
	}
	if err = os.MkdirAll(filepath.Dir(poster), 0755); err != nil {
		return VideoProcessResult{}, err
	}
	tmpVideo, err := os.CreateTemp(filepath.Dir(destination), ".video-*.mp4")
	if err != nil {
		return VideoProcessResult{}, err
	}
	tmpVideoPath := tmpVideo.Name()
	tmpVideo.Close()
	defer os.Remove(tmpVideoPath)
	tmpPoster, err := os.CreateTemp(filepath.Dir(poster), ".poster-*.webp")
	if err != nil {
		return VideoProcessResult{}, err
	}
	tmpPosterPath := tmpPoster.Name()
	tmpPoster.Close()
	_ = os.Remove(tmpPosterPath)
	defer os.Remove(tmpPosterPath)
	tmpPosterPNG := tmpPosterPath + ".png"
	defer os.Remove(tmpPosterPNG)
	filter := videoScaleFilter(profile)
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-ss", strconv.FormatFloat(trim.Start, 'f', 3, 64), "-i", source, "-t", strconv.FormatFloat(end-trim.Start, 'f', 3, 64), "-map", "0:v:0", "-an", "-sn", "-dn", "-map_metadata", "-1", "-vf", filter, "-c:v", "libx264", "-preset", profile.Preset, "-crf", strconv.Itoa(profile.CRF), "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-tag:v", "avc1", "-f", "mp4", tmpVideoPath}
	cmd := exec.CommandContext(ctx, cfg.FFmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return VideoProcessResult{}, fmt.Errorf("transcode video with ffmpeg: %w: %s", err, strings.TrimSpace(string(output)))
	}
	// Extract the poster one second into the selected segment, or halfway through very short clips.
	posterOffset := 1.0
	if end-trim.Start < 1 {
		posterOffset = (end - trim.Start) / 2
	}
	posterFilter := fmt.Sprintf("scale=w='min(iw,1280)':h=-2:force_divisible_by=2")
	posterArgs := []string{"-hide_banner", "-loglevel", "error", "-y", "-ss", strconv.FormatFloat(posterOffset, 'f', 3, 64), "-i", tmpVideoPath, "-frames:v", "1", "-vf", posterFilter, "-an", "-map_metadata", "-1", "-c:v", "png", tmpPosterPNG}
	posterCmd := exec.CommandContext(ctx, cfg.FFmpegPath, posterArgs...)
	if output, err := posterCmd.CombinedOutput(); err != nil {
		return VideoProcessResult{}, fmt.Errorf("generate video poster with ffmpeg: %w: %s", err, strings.TrimSpace(string(output)))
	}
	webpCmd := exec.CommandContext(ctx, cfg.VIPSPath, "copy", tmpPosterPNG, tmpPosterPath+fmt.Sprintf("[Q=%d]", cfg.VideoPosterQuality))
	if output, err := webpCmd.CombinedOutput(); err != nil {
		return VideoProcessResult{}, fmt.Errorf("encode WebP poster with vips (install libvips-tools or set VIPS_PATH): %w: %s", err, strings.TrimSpace(string(output)))
	}
	processedMeta, err := probeVideo(ctx, cfg, tmpVideoPath)
	if err != nil {
		return VideoProcessResult{}, fmt.Errorf("validate encoded video: %w", err)
	}
	if processedMeta.HasAudio {
		return VideoProcessResult{}, fmt.Errorf("encoded video unexpectedly contains audio")
	}
	if processedMeta.Codec != "h264" {
		return VideoProcessResult{}, fmt.Errorf("encoded video codec is %s, expected h264", processedMeta.Codec)
	}
	videoInfo, err := os.Stat(tmpVideoPath)
	if err != nil {
		return VideoProcessResult{}, err
	}
	posterInfo, err := os.Stat(tmpPosterPath)
	if err != nil {
		return VideoProcessResult{}, err
	}
	if err = os.Rename(tmpVideoPath, destination); err != nil {
		return VideoProcessResult{}, fmt.Errorf("finalize video: %w", err)
	}
	if err = os.Rename(tmpPosterPath, poster); err != nil {
		_ = os.Remove(destination)
		return VideoProcessResult{}, fmt.Errorf("finalize poster: %w", err)
	}
	return VideoProcessResult{Source: sourceMeta, Processed: processedMeta, Duration: end - trim.Start, VideoBytes: videoInfo.Size(), PosterBytes: posterInfo.Size()}, nil
}

func videoScaleFilter(profile config.VideoProfile) string {
	return fmt.Sprintf("scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease:force_divisible_by=2,fps=%d,format=yuv420p", profile.MaxWidth, profile.MaxHeight, profile.FPS)
}
