package server

import "net/http"

func (a *app) handleVisionDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	a.mu.RLock()
	info := a.lastVision
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, info)
}
