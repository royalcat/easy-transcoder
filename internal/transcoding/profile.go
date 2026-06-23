package transcoding

import (
	"os/exec"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// an ffmpeg transcoding profile
type Profile struct {
	Name string `koanf:"name"`

	Params map[string]string `koanf:"params"`

	BatchExcludeFilter *CodecFilter `koanf:"batch_exclude_filter"`
}

func (p *Profile) Compile(ffmpegPath, input, output, progressSock string) *exec.Cmd {
	args := ffmpeg.KwArgs{
		"map": "0",
	}
	for k, v := range p.Params {
		args[k] = v
	}

	cmd := ffmpeg.Input(input).
		Output(output, args).
		GlobalArgs("-progress", "unix://"+progressSock).
		OverWriteOutput().
		SetFfmpegPath(ffmpegPath).
		Compile()

	return cmd
}

// CompilePipe builds an exec.Cmd for pipe-based transcoding.
// Input is read from stdin (pipe:0) and output is written to stdout (pipe:1).
// No progress socket is used — progress must be tracked out-of-band.
// Defaults to mp4 output format; profiles can override via "f" param.
func (p *Profile) CompilePipe(ffmpegPath string) *exec.Cmd {
	args := ffmpeg.KwArgs{
		"map": "0",
		"f":   "mp4",
	}
	for k, v := range p.Params {
		args[k] = v
	}

	cmd := ffmpeg.Input("pipe:0").
		Output("pipe:1", args).
		SetFfmpegPath(ffmpegPath).
		Compile()

	return cmd
}

// PipeArgs returns the FFmpeg CLI arguments for pipe-based transcoding
// as a string slice, suitable for sending to a remote worker.
func (p *Profile) PipeArgs(ffmpegPath string) []string {
	return p.CompilePipe(ffmpegPath).Args
}
