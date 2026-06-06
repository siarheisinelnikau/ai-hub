package status

import "testing"

func TestParseDockerDF(t *testing.T) {
	out := "Images|10|2|202.6GB|177.1GB (87%)\n" +
		"Containers|3|2|79.96MB|19.19MB (24%)\n" +
		"Local Volumes|3|1|3.353GB|2.235GB (66%)\n" +
		"Build Cache|0|0|0B|0B\n"
	got := parseDockerDF(out)
	if got.ImagesTotal != "202.6GB" || got.ImagesReclaimable != "177.1GB" {
		t.Fatalf("images = %q/%q, want 202.6GB/177.1GB", got.ImagesTotal, got.ImagesReclaimable)
	}
	if got.Containers != "79.96MB" || got.ContainersReclaim != "19.19MB" {
		t.Fatalf("containers = %q/%q", got.Containers, got.ContainersReclaim)
	}
	if got.Volumes != "3.353GB" || got.VolumesReclaimable != "2.235GB" {
		t.Fatalf("volumes = %q/%q", got.Volumes, got.VolumesReclaimable)
	}
	if got.BuildCache != "0B" {
		t.Fatalf("build cache = %q, want 0B", got.BuildCache)
	}
}

func TestParseDockerDFVerbose(t *testing.T) {
	out := `{"Images":[{"Containers":"0","ID":"sha256:9caad013f576d2cf6be7421167aee38b86c1bd39730033fa875e4ba24cda9f6b","Repository":"ai-stack-threed","SharedSize":"11.42GB","Size":"73GB","Tag":"latest","UniqueSize":"61.55GB"},{"Containers":"1","ID":"sha256:9f0a986a78ab9261afc3266c807c16933ee4c26c62cb063f0c17f8da890f6c7e","Repository":"ghcr.io/ggml-org/llama.cpp","SharedSize":"6.917kB","Size":"6.98GB","Tag":"server-cuda","UniqueSize":"6.982GB"}],"Volumes":[{"Driver":"local","Links":"1","Name":"open-webui","Size":"1.118GB"},{"Driver":"local","Links":"0","Name":"qwen-cache","Size":"1.021GB"}]}`
	imgs, vols, ok := parseDockerDFVerbose(out)
	if !ok {
		t.Fatal("parseDockerDFVerbose = !ok")
	}
	if len(imgs) != 2 || len(vols) != 2 {
		t.Fatalf("len(imgs)=%d len(vols)=%d, want 2/2", len(imgs), len(vols))
	}
	if imgs[0].Name != "ai-stack-threed:latest" || imgs[0].Size != "73GB" || imgs[0].UniqueSize != "61.55GB" {
		t.Fatalf("img[0] = %+v", imgs[0])
	}
	if imgs[0].InUse || !imgs[0].Reclaimable || imgs[0].Containers != 0 {
		t.Fatalf("img[0] flags = %+v, want unused+reclaimable", imgs[0])
	}
	if imgs[1].ID != "9f0a986a78ab" || !imgs[1].InUse || imgs[1].Reclaimable {
		t.Fatalf("img[1] = %+v, want in-use", imgs[1])
	}
	if vols[0].Name != "open-webui" || !vols[0].InUse || vols[0].Links != 1 {
		t.Fatalf("vol[0] = %+v", vols[0])
	}
	if vols[1].InUse || vols[1].Links != 0 {
		t.Fatalf("vol[1] = %+v, want unused", vols[1])
	}
}

func TestParseDockerDFVerboseBadJSON(t *testing.T) {
	if _, _, ok := parseDockerDFVerbose("not json"); ok {
		t.Fatal("bad JSON should not be ok")
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("sha256:9caad013f576d2cf"); got != "9caad013f576" {
		t.Fatalf("shortID = %q", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Fatalf("shortID = %q", got)
	}
}

func TestAtoiSafe(t *testing.T) {
	if got := atoiSafe("12"); got != 12 {
		t.Fatalf("atoiSafe = %d", got)
	}
	if got := atoiSafe(""); got != 0 {
		t.Fatalf("atoiSafe empty = %d", got)
	}
	if got := atoiSafe("x"); got != 0 {
		t.Fatalf("atoiSafe junk = %d", got)
	}
}

func TestParseDuChildren(t *testing.T) {
	out := "78.3G\t/home/user/models/Qwen3.5-122B-A10B-Uncensored-HauhauCS\n" +
		"76G\t/home/user/models/gpt-oss-120b\n" +
		"16.2G\t/home/user/models/Qwen3.8-27B-Uncensored-GGUF\n"
	items := parseDuChildren(out)
	if len(items) != 3 {
		t.Fatalf("len = %d, want 3", len(items))
	}
	if items[0].Name != "Qwen3.5-122B-A10B-Uncensored-HauhauCS" || items[0].Size != "78.3G" {
		t.Fatalf("items[0] = %+v", items[0])
	}
	if items[0].Path != "/home/user/models/Qwen3.5-122B-A10B-Uncensored-HauhauCS" {
		t.Fatalf("path = %q", items[0].Path)
	}
	if got := parseDuChildren(""); len(got) != 0 {
		t.Fatalf("empty = %v", got)
	}
}
