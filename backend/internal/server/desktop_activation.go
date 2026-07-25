package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func desktopActivationHandler(activation chan<- struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		select {
		case activation <- struct{}{}:
		default:
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
	}
}

func activateExistingDesktop(localURL string) error {
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(localURL, "/")+"/api/desktop/activate", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("activation returned HTTP %d", response.StatusCode)
	}
	return nil
}

func existingVisionRelayHealthy(localURL string) bool {
	return visionRelaySurfaceHealthy(localURL, "management", true)
}

func relayVisionRelayHealthy(localURL string) bool {
	return visionRelaySurfaceHealthy(localURL, "relay", false)
}

func visionRelaySurfaceHealthy(localURL, expectedSurface string, allowLegacySurface bool) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(strings.TrimRight(localURL, "/") + "/healthz")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	var health struct {
		Status      string `json:"status"`
		Application string `json:"application"`
		Surface     string `json:"surface"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return false
	}
	if health.Status != "ok" || health.Application != appSlug {
		return false
	}
	if health.Surface == expectedSurface {
		return true
	}
	return allowLegacySurface && health.Surface == ""
}
