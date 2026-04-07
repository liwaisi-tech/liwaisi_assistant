package httpapi

import "net/http"

// Handler returns the server's top-level HTTP handler (middleware-wrapped mux).
// This allows external adapters (e.g., A2A) to compose with the REST handler
// on the same listener without modifying the server internals.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}
