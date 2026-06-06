package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListInventory(t *testing.T) {
	t.Setenv("AIHUB_DOWNLOADS_DIR", t.TempDir())
	list := List()
	if len(list) != 14 {
		t.Fatalf("list = %d entries, want 14 (installed and downloadable LLMs)", len(list))
	}
	if list[0].ID != "qwen-q4k-llama" {
		t.Fatalf("list[0] = %q, want qwen-q4k-llama (first in the panel)", list[0].ID)
	}
	found36 := false
	for _, s := range list {
		if s.ID == "" || s.Download.Status == "" {
			t.Fatalf("model %q has empty id/download status", s.ID)
		}
		if s.ID == "qwen-w4a16-single" {
			t.Fatalf("removed model %s is still in the catalog", s.ID)
		}
		if s.ID == "qwen-q6k-llama" {
			found36 = true
			if s.HFRepo == "" {
				t.Fatalf("qwen36 should have a plain-HF download source")
			}
		}
	}
	if !found36 {
		t.Fatalf("qwen-q6k-llama missing from the Model manager catalog")
	}
}

func TestHfFileURL(t *testing.T) {
	got := hfFileURL("owner/name", "dir/file.gguf")
	want := "https://huggingface.co/owner/name/resolve/main/dir/file.gguf"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
	if got := hfFileURL("o/r", "weird name.gguf"); got != "https://huggingface.co/o/r/resolve/main/weird%20name.gguf" {
		t.Fatalf("escaped url = %q", got)
	}
}

func TestRemovedWeightsKeepDownloadableCatalog(t *testing.T) {
	id := "qwen38-35b-q8-llama"
	original := Catalog[id]
	info := original
	info.LocalPath = t.TempDir()
	Catalog[id] = info
	defer func() { Catalog[id] = original }()
	if err := os.MkdirAll(filepath.Join(info.LocalPath, ".cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if Ready(id) {
		t.Fatal("cache is not usable weights")
	}
	for _, file := range info.Files {
		if err := os.WriteFile(filepath.Join(info.LocalPath, file), []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if !Ready(id) {
		t.Fatal("complete weights must become ready")
	}
	if err := os.Remove(filepath.Join(info.LocalPath, info.Files[0])); err != nil {
		t.Fatal(err)
	}
	if Ready(id) {
		t.Fatal("deleted weights must not leave a launchable card")
	}
	if Catalog[id].HFRepo == "" || len(Catalog[id].Files) == 0 {
		t.Fatal("download source lost after deletion")
	}
	ports := map[int]bool{}
	for _, m := range Catalog {
		if ports[m.Port] {
			t.Fatalf("duplicate catalog port %d", m.Port)
		}
		ports[m.Port] = true
	}
	q122 := Catalog["qwen35-122b-q4k-llama"]
	if strings.Contains(strings.Join(q122.Files, " "), "-MTP.gguf") {
		t.Fatal("merged output is not a HuggingFace source file")
	}
}

func TestDownloadProgress(t *testing.T) {
	t.Setenv("AIHUB_DOWNLOADS_DIR", t.TempDir())
	dir := t.TempDir()
	info := Catalog["qwen-q6k-llama"]
	original := info
	defer func() { Catalog["qwen-q6k-llama"] = original }()
	info.LocalPath = dir
	Catalog["qwen-q6k-llama"] = info
	if err := os.WriteFile(filepath.Join(dir, "part.gguf"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	stateSet(State{ID: "qwen-q6k-llama", Status: "downloading", Updated: 1, TotalBytes: 2 << 20})
	if !IsDownloading("qwen-q6k-llama") {
		t.Fatal("IsDownloading = false, want true")
	}
	for _, s := range List() {
		if s.ID != "qwen-q6k-llama" {
			continue
		}
		if s.Download.Pct != 50 {
			t.Fatalf("pct = %v, want 50 (1MiB of 2MiB)", s.Download.Pct)
		}
	}
	stateSet(State{ID: "qwen-q6k-llama", Status: "complete", Updated: 2})
	if IsDownloading("qwen-q6k-llama") {
		t.Fatal("IsDownloading = true after complete")
	}
}

func TestDownloadComplete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIHUB_DOWNLOADS_DIR", dir)
	info := Catalog["qwen-q6k-llama"]
	original := info
	defer func() { Catalog["qwen-q6k-llama"] = original }()
	info.LocalPath = dir // point probe at the temp dir
	Catalog["qwen-q6k-llama"] = info

	orig := runDownload
	runDownload = func(repo, localDir, logFile string, files []string) error {
		if localDir == "" {
			localDir = dir
		}
		if err := os.MkdirAll(localDir, 0o755); err != nil {
			return err
		}
		for _, file := range files {
			if err := os.WriteFile(filepath.Join(localDir, file), []byte("x"), 0644); err != nil {
				return err
			}
		}
		return nil
	}
	defer func() { runDownload = orig }()

	st, err := Download("qwen-q6k-llama")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if st.Status != "downloading" {
		t.Fatalf("initial status = %q, want downloading", st.Status)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stateGet("qwen-q6k-llama").Status == "complete" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s := stateGet("qwen-q6k-llama"); s.Status != "complete" {
		t.Fatalf("final status = %q (err=%q), want complete", s.Status, s.Error)
	}
	// on-disk detection should now see the file
	list := List()
	for _, s := range list {
		if s.ID == "qwen-q6k-llama" && !s.OnDisk {
			t.Fatalf("qwen-q6k-llama should be on-disk after download")
		}
	}
}

func TestDownloadNoSource(t *testing.T) {
	t.Setenv("AIHUB_DOWNLOADS_DIR", t.TempDir())
	if _, err := Download("qwen-w4a16-single"); err == nil {
		t.Fatalf("expected an error for a removed (no longer in catalog) model")
	}
	if _, err := Download("does-not-exist"); err == nil {
		t.Fatalf("expected an error for an unknown model id")
	}
}

func TestResumeRelaunchesPending(t *testing.T) {
	t.Setenv("AIHUB_DOWNLOADS_DIR", t.TempDir())
	seed := map[string]State{
		"qwen-q6k-llama": {ID: "qwen-q6k-llama", Status: "downloading", Started: 1, Updated: 1},
		"qwen-q4k-llama": {ID: "qwen-q4k-llama", Status: "complete", Updated: 1},
	}
	mu.Lock()
	saveStatesLocked(seed)
	mu.Unlock()

	orig := runDownload
	completed := make(chan struct{}, 1)
	runDownload = func(repo, localDir, logFile string, files []string) error { completed <- struct{}{}; return nil }
	defer func() { runDownload = orig }()

	if n := Resume(); n != 1 {
		t.Fatalf("Resume = %d, want 1 (only the pending one)", n)
	}
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("pending download did not resume")
	}
}

func TestCompanionProjectorUsesItsOwnRepository(t *testing.T) {
	original := runDownload
	defer func() { runDownload = original }()
	var repos []string
	var names []string
	runDownload = func(repo, dir, log string, files []string) error {
		repos = append(repos, repo)
		names = append(names, files...)
		return nil
	}
	if err := runModelDownload(Catalog["qwen-q4k-llama"]); err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0] == repos[1] || names[len(names)-1] != "mmproj-Qwen3.8-27B-Q8_0.gguf" {
		t.Fatalf("vision source missing: %v %v", repos, names)
	}
}

func TestStrataDownloadPreparesCorrectFamilyWithoutStarting(t *testing.T) {
	for _, test := range []struct{ path, family, quant, port string }{
		{"/home/user/models/Strata-iq3-data", "qwen", "IQ3_XXS", "18033"},
		{"/home/user/models/Strata-data", "unsloth", "UD-IQ4_XS", "18029"},
	} {
		args, quant := strataSetupArgs(test.path)
		joined := strings.Join(args, " ")
		for _, required := range []string{"--family " + test.family, "--model " + test.quant, "--port " + test.port, "--data-dir " + test.path, "--context 262144", "--no-start"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("download recipe missing %q: %v", required, args)
			}
		}
		if quant != test.quant {
			t.Fatal("setup settings are not isolated by quantization")
		}
	}
}
