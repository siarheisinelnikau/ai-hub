// Package config loads ai-hub runtime configuration from the environment.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds runtime configuration for the ai-hub panel.
type Config struct {
	Host                    string // listen interface (AIHUB_HOST)
	Port                    int    // HTTP listen port (AIHUB_PORT, default 8080)
	Root                    string // install root dir (AIHUB, default: dir of the executable)
	TokenPath               string // Root + "token"
	Token                   string // AIHUB_TOKEN, never included in public configuration
	AllowUnauthenticatedAPI bool   // explicit compatibility mode for isolated networks
	SecureCookies           bool   // enable when TLS terminates at a reverse proxy
}

// Load reads configuration from the environment:
//
//   - AIHUB_PORT: listen port, default 8080 (parse error is returned)
//   - AIHUB:      root dir, default = directory containing the executable
func Load() (*Config, error) {
	if err := LoadEnvironment(); err != nil {
		return nil, err
	}
	cfg := &Config{
		Host: EnvOr("AIHUB_HOST", "127.0.0.1"),
		Port: 8080,
		Root: defaultRoot(),
	}

	if v := os.Getenv("AIHUB_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("AIHUB_PORT must be an integer between 1 and 65535")
		}
		cfg.Port = port
	}

	if v := os.Getenv("AIHUB"); v != "" {
		cfg.Root = v
	}
	var err error
	cfg.Root, err = filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve AIHUB: %w", err)
	}

	cfg.TokenPath = EnvOr("AIHUB_TOKEN_FILE", filepath.Join(cfg.Root, "token"))
	if !filepath.IsAbs(cfg.TokenPath) {
		cfg.TokenPath = filepath.Join(cfg.Root, cfg.TokenPath)
	}
	cfg.Token = os.Getenv("AIHUB_TOKEN")
	for key, dst := range map[string]*bool{
		"AIHUB_ALLOW_UNAUTHENTICATED_API": &cfg.AllowUnauthenticatedAPI,
		"AIHUB_SECURE_COOKIES":            &cfg.SecureCookies,
	} {
		if value := os.Getenv(key); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("%s must be true or false", key)
			}
			*dst = parsed
		}
	}
	return cfg, nil
}

func (c *Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

// defaultRoot returns the directory containing the executable, falling back
// to the current working directory if any step fails.
func defaultRoot() string {
	// go run builds into a temporary directory; prefer the checkout when run there.
	if wd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(wd, "registry.yaml")); err == nil {
			return wd
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return fallbackDir()
	}
	root := filepath.Dir(exe)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fallbackDir()
	}
	return root
}

func fallbackDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
