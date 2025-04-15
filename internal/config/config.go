package config

import "github.com/royalcat/easy-transcode/internal/profile"

type Config struct {
	Profiles []profile.Profile
	TempDir  string
}
