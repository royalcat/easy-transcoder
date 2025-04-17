package config

import "github.com/royalcat/easy-transcoder/internal/profile"

var DefaultConfig = Config{
	Profiles: []profile.Profile{
		{
			Name: "H264 Ultra Fast",
			Params: map[string]string{
				"c:v":    "libx264",
				"preset": "ultrafast",
				"c:a":    "copy",
			},
		},
		{
			Name: "H264 Slow",
			Params: map[string]string{
				"c:v":    "libx264",
				"preset": "slow",
				"c:a":    "copy",
			},
		},
	},
}
