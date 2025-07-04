package transcoding

import (
	"os/exec"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// an ffmpeg transcoding profile
type Profile struct {
	Name   string            `koanf:"name"`
	Params map[string]string `koanf:"params"`
}

func (p *Profile) Compile(input, output, progressSock string) *exec.Cmd {
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
		Compile()

	return cmd
}
