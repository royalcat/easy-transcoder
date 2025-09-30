package transcoding

import (
	"encoding/json"
	"fmt"
	"strings"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

var VideoExtensions = []string{
	".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ts", ".mpg", ".mpeg",
}

type CodecFilter struct {
	Codecs []string `koanf:"codecs"`
}

func (f CodecFilter) Matches(path string) (bool, error) {
	val, err := Probe(path)
	if err != nil {
		return false, fmt.Errorf("failed to probe file: %w", err)
	}
	for _, stream := range val.Streams {
		for _, codec := range f.Codecs {
			if strings.Contains(stream.CodecName, codec) {
				return true, nil
			}
		}
	}
	// return false, fmt.Errorf("no matching filters found")
	return false, nil
}

type FFProbeData struct {
	Format  FFProbeFormat   `json:"format"`
	Streams []FFProbeStream `json:"streams"`
}

type FFProbeFormat struct {
	Filename       string            `json:"filename"`
	NBStreams      int               `json:"nb_streams"`
	NBPrograms     int               `json:"nb_programs"`
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	Duration       string            `json:"duration"`
	Size           string            `json:"size"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags"`
}

type FFProbeStream struct {
	Index          int               `json:"index"`
	CodecName      string            `json:"codec_name"`
	CodecLongName  string            `json:"codec_long_name"`
	CodecType      string            `json:"codec_type"`
	CodecTagString string            `json:"codec_tag_string"`
	CodecTag       string            `json:"codec_tag"`
	Width          int               `json:"width,omitempty"`
	Height         int               `json:"height,omitempty"`
	SampleRate     string            `json:"sample_rate,omitempty"`
	Channels       int               `json:"channels,omitempty"`
	ChannelLayout  string            `json:"channel_layout,omitempty"`
	BitsPerSample  int               `json:"bits_per_sample,omitempty"`
	RFrameRate     string            `json:"r_frame_rate"`
	AvgFrameRate   string            `json:"avg_frame_rate"`
	TimeBase       string            `json:"time_base"`
	DurationTs     int64             `json:"duration_ts"`
	Duration       string            `json:"duration"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags"`
}

func Probe(path string) (FFProbeData, error) {
	probeJSON, err := ffmpeg.Probe(path)
	if err != nil {
		return FFProbeData{}, fmt.Errorf("failed to probe file: %w", err)
	}
	data := FFProbeData{}
	err = json.Unmarshal([]byte(probeJSON), &data)
	if err != nil {
		return FFProbeData{}, fmt.Errorf("failed to unmarshal ffprobe data: %w", err)
	}
	return data, nil
}
