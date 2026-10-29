package models

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredCatalogReplacesLegacyAndRejectsTraversal(t *testing.T) {
	old, order := Catalog, configuredOrder
	t.Cleanup(func() { Catalog, configuredOrder = old, order })
	root := t.TempDir()
	t.Setenv("AIHUB_CATALOG", "catalog.yaml")
	path := filepath.Join(root, "catalog.yaml")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("models: []\n")
	if err := ConfigureCatalog(root); err != nil || len(IDs()) != 0 || len(Catalog) != 0 {
		t.Fatalf("empty catalog: %v", err)
	}
	for _, file := range []string{"../token", "/token", "a/../../token", "a\\token", "*.gguf", ""} {
		write(fmt.Sprintf("models:\n- id: local-llm\n  port: 18021\n  hf_repo: owner/model\n  local_path: %q\n  files: [%q]\n", filepath.Join(root, "weights"), file))
		if err := ConfigureCatalog(root); err == nil {
			t.Fatalf("accepted unsafe path %q", file)
		}
	}
	write(fmt.Sprintf("models:\n- id: local-llm\n  port: 18021\n  hf_repo: owner/model\n  local_path: %q\n  files: [weights/model.gguf]\n", filepath.Join(root, "weights")))
	if err := ConfigureCatalog(root); err != nil || len(IDs()) != 1 || IDs()[0] != "local-llm" {
		t.Fatalf("custom catalog: %v", err)
	}
}
