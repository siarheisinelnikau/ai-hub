package models

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var configuredOrder []string

// ConfigureCatalog loads an administrator-owned download catalog at startup,
// before pollers and downloads start. An unset path retains legacy profiles.
// A configured catalog replaces the built-in list; an empty list disables it.
func ConfigureCatalog(root string) error {
	path := os.Getenv("AIHUB_CATALOG")
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read configured catalog: %w", err)
	}
	var file struct {
		Models []struct {
			ID        string   `yaml:"id"`
			Short     string   `yaml:"short"`
			Backend   string   `yaml:"backend"`
			Quant     string   `yaml:"quant"`
			Port      int      `yaml:"port"`
			HFRepo    string   `yaml:"hf_repo"`
			LocalPath string   `yaml:"local_path"`
			Files     []string `yaml:"files"`
		} `yaml:"models"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return fmt.Errorf("parse configured catalog: %w", err)
	}
	catalog := map[string]Info{}
	order := []string{}
	idPattern := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,95}$`)
	repoPattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	for _, item := range file.Models {
		if _, exists := catalog[item.ID]; exists || !idPattern.MatchString(item.ID) {
			return fmt.Errorf("invalid or duplicate catalog id: %q", item.ID)
		}
		local := filepath.Clean(item.LocalPath)
		if !filepath.IsAbs(local) || filepath.Dir(local) == local || len(item.Files) == 0 || !repoPattern.MatchString(item.HFRepo) || item.Port < 1 || item.Port > 65535 {
			return fmt.Errorf("catalog %s requires a model directory, repository, files and valid port", item.ID)
		}
		for _, name := range item.Files {
			if name == "" || strings.ContainsAny(name, "\\:*?[]") || strings.HasPrefix(name, "/") {
				return fmt.Errorf("invalid file path in catalog %s", item.ID)
			}
			for _, part := range strings.Split(name, "/") {
				if part == ".." || part == "." || part == "" {
					return fmt.Errorf("unsafe file path in catalog %s", item.ID)
				}
			}
		}
		catalog[item.ID] = Info{ID: item.ID, Short: item.Short, Backend: item.Backend, Quant: item.Quant, Port: item.Port, HFRepo: item.HFRepo, LocalPath: local, Files: item.Files}
		order = append(order, item.ID)
	}
	Catalog, configuredOrder = catalog, order
	return nil
}
