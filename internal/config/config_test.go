package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotEnvPrecedenceAndLiteralValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv("AIHUB_ENV_FILE", path)
	t.Setenv("AIHUB_PORT", "9090")
	t.Setenv("AIHUB_HOST", "127.0.0.1")
	keys := []string{"AIHUB_TOKEN", "AIHUB_SERVER_IP", "AIHUB_PUBLIC_URL", "AIHUB_WEBUI_URL", "AIHUB_MCP_URL", "AIHUB", "AIHUB_TOKEN_FILE"}
	for _, k := range keys {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	content := "\ufeff# config\nAIHUB_PORT=8081\nexport AIHUB_TOKEN='secret # literal $VALUE'\nAIHUB_SERVER_IP=10.0.0.7 # LAN\nAIHUB_WEBUI_URL=\"https://chat.example.test/\"\nAIHUB_TOKEN_FILE=private-token\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9090 || cfg.Address() != "127.0.0.1:9090" {
		t.Fatal("environment override ignored")
	}
	if cfg.Token != "secret # literal $VALUE" || filepath.Base(cfg.TokenPath) != "private-token" {
		t.Fatal("literal token/path parsing failed")
	}
	pub, err := cfg.Public()
	if err != nil || pub.ServerIP != "10.0.0.7" || pub.WebUIURL != "https://chat.example.test/" {
		t.Fatalf("public: %+v, %v", pub, err)
	}
}

func TestInvalidEnvironment(t *testing.T) {
	t.Setenv("AIHUB_ENV_FILE", "-")
	for _, port := range []string{"bad", "0", "65536", "-1"} {
		t.Setenv("AIHUB_PORT", port)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted port %s", port)
		}
	}
	for _, line := range []string{"bad assignment", "AIHUB_TOKEN=\"secret-unclosed", "AIHUB_TOKEN='secret-unclosed"} {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loadEnvFile(path); err == nil || strings.Contains(err.Error(), "secret-unclosed") {
			t.Fatal("invalid value accepted or disclosed")
		}
	}
	t.Setenv("AIHUB_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	if err := LoadEnvironment(); err == nil {
		t.Fatal("missing explicit env file silently ignored")
	}
}

func TestPublicURLs(t *testing.T) {
	for _, key := range []string{"AIHUB_PUBLIC_URL", "AIHUB_WEBUI_URL", "AIHUB_MCP_URL", "AIHUB_SERVER_IP"} {
		t.Setenv(key, "")
	}
	for _, u := range []string{"javascript:alert(1)", "http://user:secret@example.test", "https:///missing-host"} {
		t.Setenv("AIHUB_WEBUI_URL", u)
		if _, err := (&Config{}).Public(); err == nil {
			t.Fatal("invalid public URL accepted")
		}
	}
	t.Setenv("AIHUB_SERVER_IP", "::1")
	if ServiceURL(8188) != "http://[::1]:8188" {
		t.Fatal("IPv6 URL is malformed")
	}
}
