package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"aihub/internal/status"
)

// Context benchmarks are measured offline; opening the graph never starts a model.
type contextBenchmarks struct {
	Version   int             `json:"version"`
	Date      string          `json:"date"`
	Status    string          `json:"status"`
	UpdatedAt string          `json:"updated_at"`
	Protocol  json.RawMessage `json:"protocol"`
	Models    []contextCurve  `json:"models"`
}

type contextCurve struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Short       string          `json:"short"`
	Quant       string          `json:"quant"`
	Backend     string          `json:"backend"`
	MaxContext  int             `json:"max_context"`
	GPUs        []int           `json:"gpus"`
	Parallel    int             `json:"parallel"`
	Status      string          `json:"status"`
	Error       string          `json:"error,omitempty"`
	QualityNote string          `json:"quality_note,omitempty"`
	Sampling    json.RawMessage `json:"sampling,omitempty"`
	TPS20k      float64         `json:"tps_20k,omitempty"`
	Points      json.RawMessage `json:"points"`
}

func (s *Server) readContextBenchmarks() (*contextBenchmarks, error) {
	f, err := os.Open(filepath.Join(s.d.AIHUBRoot, "benchmarks", "context.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var doc contextBenchmarks
	err = json.NewDecoder(io.LimitReader(f, 2<<20)).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (s *Server) handleContextBenchmarks(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	doc, err := s.readContextBenchmarks()
	if os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"version": 1, "status": "unavailable", "models": []interface{}{}})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"detail": "benchmark data could not be read"})
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) annotateContextBenchmarks(models []status.ModelStatusEntry) {
	doc, err := s.readContextBenchmarks()
	if err != nil {
		return // Keep existing registry behavior when no benchmark file exists.
	}
	curves := map[string]contextCurve{}
	for _, curve := range doc.Models {
		curves[curve.ID] = curve
	}
	for i := range models {
		m := &models[i]
		if m.Category != "LLM" {
			continue
		}
		m.TPS = "— @20k"
		if curve, ok := curves[m.ID]; ok {
			m.BenchmarkStatus = curve.Status
			var points []json.RawMessage
			if json.Unmarshal(curve.Points, &points) == nil {
				m.BenchmarkPoints = len(points)
			}
			if curve.TPS20k > 0 {
				m.BenchmarkTPS20k = curve.TPS20k
				m.TPS = fmt.Sprintf("%.1f tok/s @20k", curve.TPS20k)
			}
		}
	}
}
