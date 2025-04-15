package profile

import (
	"os/exec"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// an ffmpeg transcoding profile
type Profile struct {
	Name   string
	Params map[string]string
}

func (p *Profile) Compile(input, output, progressSock string) *exec.Cmd {
	args := ffmpeg.KwArgs{}
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
