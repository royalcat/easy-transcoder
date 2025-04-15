package config

import "github.com/royalcat/easy-transcoder/internal/profile"

var DefaultConfig = Config{
	TempDir: "./media",
	Profiles: []profile.Profile{
		{
			Name: "H264 Ultrafast",
			Params: map[string]string{
				"c:v":    "libx264",
				"preset": "ultrafast",
				"c:a":    "copy",
			},
		},
	},
}
