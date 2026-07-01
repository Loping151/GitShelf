// Package config loads and validates the GitShelf configuration.
//
// Everything in GitShelf is config-driven: there must be no hard-coded paths,
// usernames or secrets anywhere in the product. Secrets may be supplied via
// environment variables referenced from the config.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration object.
type Config struct {
	Server    Server       `toml:"server"`
	Sources   []RepoSource `toml:"repo_source"`
	Metadata  Metadata     `toml:"metadata"`
	Renderers Renderers    `toml:"renderers"`
	Auth      Auth         `toml:"auth"`
	Cache     Cache        `toml:"cache"`
}

// Server holds HTTP server settings.
type Server struct {
	Bind     string `toml:"bind"`      // default 127.0.0.1:8888 (local only)
	BasePath string `toml:"base_path"` // reverse-proxy sub-path, e.g. "/git"
	Theme    string `toml:"theme"`     // auto | light | dark
	SiteName string `toml:"site_name"` // branding
}

// RepoSource describes one place to discover repositories.
type RepoSource struct {
	Type      string `toml:"type"`      // "directory"
	Path      string `toml:"path"`      // directory holding *.git mirrors/bare repos
	Glob      string `toml:"glob"`      // default "*.git"
	Namespace string `toml:"namespace"` // "flat" | "owner"
}

// Metadata configures the issues/PRs/releases provider.
type Metadata struct {
	Provider string `toml:"provider"`  // "" (none) | "json-export" | "github-api"
	Path     string `toml:"path"`      // for json-export
	TokenEnv string `toml:"token_env"` // for github-api
}

// Renderers configures the file render pipeline.
type Renderers struct {
	MaxRenderBytes int64    `toml:"max_render_bytes"` // text/code above this is truncated
	MaxRawBytes    int64    `toml:"max_raw_bytes"`    // above this, no inline preview at all
	PDFEngine      string   `toml:"pdf_engine"`       // pdfjs | native
	Disable        []string `toml:"disable"`          // renderer names to disable
}

// Auth configures optional access control.
type Auth struct {
	Enabled bool `toml:"enabled"` // when true, a first-run setup wizard sets the admin password
}

// Cache configures on-disk state (sessions, admin password, render cache).
type Cache struct {
	Dir string `toml:"dir"`
}

// Default returns a Config populated with sane defaults.
func Default() Config {
	return Config{
		Server: Server{
			Bind:     "127.0.0.1:8888",
			BasePath: "",
			Theme:    "auto",
			SiteName: "GitShelf",
		},
		Metadata: Metadata{Provider: ""},
		Renderers: Renderers{
			MaxRenderBytes: 1 << 20,  // 1 MiB
			MaxRawBytes:    50 << 20, // 50 MiB
			PDFEngine:      "native",
		},
		Auth:  Auth{Enabled: true}, // secure by default: first visit runs /setup
		Cache: Cache{Dir: defaultCacheDir()},
	}
}

func defaultCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "gitshelf")
	}
	return filepath.Join(os.TempDir(), "gitshelf")
}

// Load reads, defaults and validates a TOML config file.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		md, err := toml.DecodeFile(path, &cfg)
		if err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
		if undec := md.Undecoded(); len(undec) > 0 {
			keys := make([]string, len(undec))
			for i, k := range undec {
				keys[i] = k.String()
			}
			return cfg, fmt.Errorf("unknown config keys: %s", strings.Join(keys, ", "))
		}
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) normalize() {
	if c.Server.Bind == "" {
		c.Server.Bind = "127.0.0.1:8888"
	}
	if c.Server.Theme == "" {
		c.Server.Theme = "auto"
	}
	if c.Server.SiteName == "" {
		c.Server.SiteName = "GitShelf"
	}
	c.Server.BasePath = strings.TrimRight(c.Server.BasePath, "/")
	if c.Server.BasePath != "" && !strings.HasPrefix(c.Server.BasePath, "/") {
		c.Server.BasePath = "/" + c.Server.BasePath
	}
	for i := range c.Sources {
		s := &c.Sources[i]
		if s.Type == "" {
			s.Type = "directory"
		}
		if s.Glob == "" {
			s.Glob = "*.git"
		}
		if s.Namespace == "" {
			s.Namespace = "flat"
		}
	}
	if c.Renderers.MaxRenderBytes <= 0 {
		c.Renderers.MaxRenderBytes = 1 << 20
	}
	if c.Renderers.MaxRawBytes <= 0 {
		c.Renderers.MaxRawBytes = 50 << 20
	}
	if c.Renderers.PDFEngine == "" {
		c.Renderers.PDFEngine = "native"
	}
	if c.Cache.Dir == "" {
		c.Cache.Dir = defaultCacheDir()
	}
}

// Validate checks the config for fatal problems and returns a friendly error.
func (c *Config) Validate() error {
	if !strings.Contains(c.Server.Bind, ":") {
		return fmt.Errorf("server.bind %q must be host:port", c.Server.Bind)
	}
	switch c.Server.Theme {
	case "auto", "light", "dark":
	default:
		return fmt.Errorf("server.theme %q must be auto|light|dark", c.Server.Theme)
	}
	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one [[repo_source]] is required")
	}
	for i, s := range c.Sources {
		if s.Type != "directory" {
			return fmt.Errorf("repo_source[%d].type %q unsupported (only \"directory\")", i, s.Type)
		}
		if s.Path == "" {
			return fmt.Errorf("repo_source[%d].path is required", i)
		}
		if s.Namespace != "flat" && s.Namespace != "owner" {
			return fmt.Errorf("repo_source[%d].namespace %q must be flat|owner", i, s.Namespace)
		}
		if info, err := os.Stat(s.Path); err != nil || !info.IsDir() {
			return fmt.Errorf("repo_source[%d].path %q is not a readable directory", i, s.Path)
		}
	}
	switch c.Metadata.Provider {
	case "", "json-export", "github-api":
	default:
		return fmt.Errorf("metadata.provider %q must be json-export|github-api or empty", c.Metadata.Provider)
	}
	if c.Metadata.Provider == "json-export" && c.Metadata.Path == "" {
		return fmt.Errorf("metadata.path is required for json-export provider")
	}
	switch c.Renderers.PDFEngine {
	case "pdfjs", "native":
	default:
		return fmt.Errorf("renderers.pdf_engine %q must be pdfjs|native", c.Renderers.PDFEngine)
	}
	return nil
}
