package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitRegistryFailsClosed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AIHUB_REGISTRY", "custom.yaml")
	if _, err := Load(root, root); err == nil {
		t.Fatal("missing explicit registry silently accepted")
	}
	for _, body := range []string{
		"models: [", "modles: []",
		"models: [{id: '../token', sysd: ai-model, port: 18021}]",
		"models: [{id: model, sysd: --bad, port: 18021}]",
		"models: [{id: model, sysd: ai-model, port: 99999}]",
	} {
		if err := os.WriteFile(filepath.Join(root, "custom.yaml"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root, root); err == nil {
			t.Fatalf("accepted invalid registry %q", body)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "custom.yaml"), []byte("models: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if f, err := Load(root, root); err != nil || len(f.Models) != 0 {
		t.Fatalf("valid empty registry: %v", err)
	}
}
