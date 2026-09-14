package auth

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

type ClientConfig struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}
type GitHubConfig struct {
	ClientConfig
	AppType string `json:"appType,omitempty"`
}
type Selector struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Note     string `json:"note"`
}
type Config struct {
	Google      ClientConfig `json:"google"`
	GitHub      GitHubConfig `json:"github"`
	Superadmins []Selector   `json:"superadmins"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return c, err
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 65536 {
		return c, errors.New("auth.json must be a private regular file (0600), at most 64 KiB")
	}
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return Config{}, errors.New("auth.json has invalid configuration")
	}
	if d.Decode(new(any)) != io.EOF {
		return Config{}, errors.New("auth.json must contain one JSON object")
	}
	if c.GitHub.AppType != "" && c.GitHub.AppType != "oauth-app" && c.GitHub.AppType != "github-app" {
		return Config{}, errors.New("github appType must be oauth-app or github-app")
	}
	for _, client := range []ClientConfig{c.Google, c.GitHub.ClientConfig} {
		if (client.ClientID == "") != (client.ClientSecret == "") {
			return Config{}, errors.New("each OAuth provider requires both clientId and clientSecret")
		}
	}
	return c, nil
}
