package httpapi

import (
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

// handleHealth returns a 200 OK health check response.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// handleVersion returns build version information.
func handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, VersionResponse{
		Version: version.Version,
		Commit:  version.GitCommit,
		Built:   version.BuildTime,
	})
}
