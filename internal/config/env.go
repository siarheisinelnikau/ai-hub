package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoadEnvironment reads one .env without executing shell commands or expanding
// variables. Process environment always wins, including explicitly empty values.
// AIHUB_ENV_FILE=- disables loading (useful for isolated tests and CI).
func LoadEnvironment() error {
	if p := os.Getenv("AIHUB_ENV_FILE"); p != "" {
		if p == "-" {
			return nil
		}
		return loadEnvFile(p)
	}
	var candidates []string
	if root := os.Getenv("AIHUB"); root != "" {
		candidates = append(candidates, filepath.Join(root, ".env"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, ".env"))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return loadEnvFile(p)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		s := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		key, value, ok := strings.Cut(s, "=")
		key = strings.TrimSpace(key)
		if !ok || !envKey.MatchString(key) {
			return fmt.Errorf("env file line %d: expected KEY=value", line)
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return fmt.Errorf("env file line %d: invalid quoted value", line)
			}
			value = decoded
		} else if strings.HasPrefix(value, "'") {
			if len(value) < 2 || !strings.HasSuffix(value, "'") {
				return fmt.Errorf("env file line %d: unclosed quote", line)
			}
			value = value[1 : len(value)-1]
		} else if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("env file line %d: cannot set %s", line, key)
			}
		}
	}
	return scanner.Err()
}

func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ServiceURL builds LAN-facing links. Internal health checks still use localhost.
func ServiceURL(port int) string {
	return "http://" + net.JoinHostPort(EnvOr("AIHUB_SERVER_IP", "127.0.0.1"), strconv.Itoa(port))
}

// Public contains only browser-visible addresses. Never add credentials here.
type Public struct {
	ServerIP string `json:"serverIP"`
	HubURL   string `json:"hubURL"`
	WebUIURL string `json:"webuiURL"`
	MCPURL   string `json:"mcpURL"`
	CaseURL  string `json:"caseURL,omitempty"`
}

func (c *Config) Public() (Public, error) {
	p := Public{
		ServerIP: os.Getenv("AIHUB_SERVER_IP"),
		HubURL:   os.Getenv("AIHUB_PUBLIC_URL"),
		WebUIURL: EnvOr("AIHUB_WEBUI_URL", ServiceURL(3000)),
		MCPURL:   EnvOr("AIHUB_MCP_URL", ServiceURL(8090)+"/mcp"),
		CaseURL:  os.Getenv("AIHUB_CASE_URL"),
	}
	if p.ServerIP == "" {
		p.WebUIURL = os.Getenv("AIHUB_WEBUI_URL")
		p.MCPURL = os.Getenv("AIHUB_MCP_URL")
	}
	for _, raw := range []string{p.HubURL, p.WebUIURL, p.MCPURL, p.CaseURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return Public{}, fmt.Errorf("public service URLs must be http(s) URLs without credentials")
		}
	}
	if strings.ContainsAny(p.ServerIP, "/?#@\"'<> ") {
		return Public{}, fmt.Errorf("AIHUB_SERVER_IP must be an IP address or hostname")
	}
	return p, nil
}
