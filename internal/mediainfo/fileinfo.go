package mediainfo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// HashFile returns the SHA-256 of a file as lowercase hex, and its size.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// Info is what ffprobe reports about a media file.
type Info struct {
	Format        string            `json:"format,omitempty"`
	FormatName    string            `json:"format_name,omitempty"`
	Duration      float64           `json:"duration_seconds,omitempty"`
	BitRate       int64             `json:"bit_rate,omitempty"`
	Codec         string            `json:"codec,omitempty"`
	SampleRate    int               `json:"sample_rate,omitempty"`
	Channels      int               `json:"channels,omitempty"`
	ChannelLayout string            `json:"channel_layout,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
}

type fullProbe struct {
	Format struct {
		FormatName     string            `json:"format_name"`
		FormatLongName string            `json:"format_long_name"`
		Duration       string            `json:"duration"`
		BitRate        string            `json:"bit_rate"`
		Tags           map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType     string            `json:"codec_type"`
		CodecLongName string            `json:"codec_long_name"`
		CodecName     string            `json:"codec_name"`
		SampleRate    string            `json:"sample_rate"`
		Channels      int               `json:"channels"`
		ChannelLayout string            `json:"channel_layout"`
		Tags          map[string]string `json:"tags"`
	} `json:"streams"`
}

// Probe reads format, audio stream and tag details with ffprobe.
func Probe(ctx context.Context, path string) (*Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "quiet", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return nil, err
	}
	return ParseProbe(out)
}

// ParseProbe turns ffprobe -show_format -show_streams JSON into Info. Tags
// from the format and the audio stream are merged; format tags win.
func ParseProbe(raw []byte) (*Info, error) {
	var p fullProbe
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	info := &Info{
		Format:     p.Format.FormatLongName,
		FormatName: p.Format.FormatName,
		Tags:       map[string]string{},
	}
	info.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	info.BitRate, _ = strconv.ParseInt(p.Format.BitRate, 10, 64)
	for k, v := range p.Format.Tags {
		info.Tags[k] = v
	}
	for _, s := range p.Streams {
		if s.CodecType != "audio" {
			continue
		}
		info.Codec = s.CodecLongName
		if info.Codec == "" {
			info.Codec = s.CodecName
		}
		info.SampleRate, _ = strconv.Atoi(s.SampleRate)
		info.Channels = s.Channels
		info.ChannelLayout = s.ChannelLayout
		for k, v := range s.Tags {
			if _, ok := info.Tags[k]; !ok {
				info.Tags[k] = v
			}
		}
		break
	}
	if len(info.Tags) == 0 {
		info.Tags = nil
	}
	return info, nil
}
