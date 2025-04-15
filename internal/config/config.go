package config

import "github.com/royalcat/easy-transcoder/internal/profile"

type Config struct {
	Profiles []profile.Profile
	TempDir  string
}
