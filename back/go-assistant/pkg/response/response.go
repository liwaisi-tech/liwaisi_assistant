// Package response contains shared API response structs for the go-assistant service.
package response

// HealthResponse represents the JSON response body for the health check endpoint.
type HealthResponse struct {
	// Status is the aggregate health status of the service.
	Status string `json:"status"`
	// Version is the current version of the service.
	Version string `json:"version,omitempty"`
	// Timestamp is the ISO 8601 time when the health check was performed.
	Timestamp string `json:"timestamp,omitempty"`
	// Checks contains the health status of individual components.
	Checks map[string]string `json:"checks,omitempty"`
}
