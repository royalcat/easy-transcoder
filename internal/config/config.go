package config

import (
	"errors"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/royalcat/easy-transcoder/internal/profile"
)

type Config struct {
	TempDir  string            `koanf:"tempdir"`
	Profiles []profile.Profile `koanf:"profiles"`
}

func ParseConfig(p string) (Config, error) {
	var k = koanf.NewWithConf(koanf.Conf{
		Delim:       ".",
		StrictMerge: true,
	})

	if err := k.Load(structs.Provider(DefaultConfig, "koanf"), nil); err != nil {
		return Config{}, err
	}

	if err := k.Load(file.Provider(p), yaml.Parser()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	if err := k.Load(env.Provider("EASY_TRANSCODER_", ".", cleanEnvVar), nil); err != nil {
		return Config{}, err
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

func cleanEnvVar(s string) string {
	return strings.Replace(strings.ToLower(strings.TrimPrefix(s, "EASY_TRANSCODER_")), "_", ".", -1)
}
