package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// handleV1Status returns basic gateway status info for the mobile app.
func (s *Server) handleV1Status(w http.ResponseWriter, r *http.Request) {
	uptime := int64(time.Since(s.startedAt).Seconds())
	resp := map[string]any{
		"version":        s.version,
		"uptime_seconds": uptime,
		"started_at":     s.startedAt.UTC().Format(time.RFC3339),
	}
	jsonResponse(w, resp)
}

// handleV1FCMToken persists the FCM token sent by the mobile app.
func (s *Server) handleV1FCMToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Token == "" {
		jsonError(w, "token is required", http.StatusBadRequest)
		return
	}

	dataDir := filepath.Join(s.dataDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		jsonError(w, "failed to create data dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tokenPath := filepath.Join(dataDir, "fcm_token.txt")
	if err := os.WriteFile(tokenPath, []byte(body.Token), 0o600); err != nil {
		jsonError(w, "failed to persist token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// handleV1QRPayload serves the pairing payload written by the cloudflared tunnel
// bootstrapper. This endpoint is PUBLIC (no auth) — the payload itself contains
// the bearer token required for further authenticated calls.
func (s *Server) handleV1QRPayload(w http.ResponseWriter, r *http.Request) {
	payloadPath := filepath.Join(s.dataDir, "data", "qr_payload.json")
	data, err := os.ReadFile(payloadPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "tunnel not ready"})
			return
		}
		jsonError(w, "failed to read payload: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
