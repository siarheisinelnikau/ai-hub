package server

import (
	"net/http"

	"aihub/internal/status"
)

// handleDiskReport serves GET /api/disk — the full disk breakdown modal:
// df totals + `du -sh` rows for every big directory + docker image/volume
// detail. Go-only extension (app.py has no such endpoint, so no parity
// constraint). The scan is expensive (whole-filesystem du + docker CLI), so
// status.FullDiskReportCached caches it for 10s; ?fresh=1 forces a new scan.
func (s *Server) handleDiskReport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	fresh := r.URL.Query().Get("fresh") == "1"
	writeJSON(w, http.StatusOK, status.FullDiskReportCached(fresh))
}
