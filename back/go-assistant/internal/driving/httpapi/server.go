package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	// Addr is the TCP address to listen on (e.g., ":8080").
	Addr string

	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout time.Duration

	// IdleTimeout is the maximum duration an idle keep-alive connection stays open.
	IdleTimeout time.Duration

	// ShutdownTimeout is the maximum duration to wait for active connections during shutdown.
	ShutdownTimeout time.Duration

	// AllowedOrigins is the list of allowed CORS origins. Use ["*"] to allow all.
	AllowedOrigins []string
}

// DefaultServerConfig returns a ServerConfig with sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Addr:            ":8080",
		ReadTimeout:     10 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		AllowedOrigins:  []string{"*"},
	}
}

// Server is the HTTP server that serves the API.
type Server struct {
	httpServer *http.Server
	broker     *SSEBroker
	logger     *slog.Logger
	config     ServerConfig
}

// NewServer creates an HTTP server with all routes and middleware wired.
func NewServer(cfg ServerConfig, appService *app.SessionService, logger *slog.Logger, billingFetcher BillingFetcher) *Server {
	broker := NewSSEBroker(logger)

	handlers := &Handlers{
		App:            appService,
		Broker:         broker,
		Logger:         logger,
		BillingFetcher: billingFetcher,
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, handlers)

	// Apply middleware chain.
	handler := Chain(mux,
		CORSMiddleware(cfg.AllowedOrigins),
		LoggingMiddleware(logger),
		RecoveryMiddleware(logger),
		RequestIDMiddleware,
	)

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.Addr,
			Handler:      handler,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: 0, // Disabled for SSE; managed per-request via ResponseController
			IdleTimeout:  cfg.IdleTimeout,
		},
		broker: broker,
		logger: logger,
		config: cfg,
	}
}

// Broker returns the SSE broker for event callback wiring.
func (s *Server) Broker() *SSEBroker {
	return s.broker
}

// Start starts the HTTP server. Blocks until the server stops.
func (s *Server) Start() error {
	s.logger.Info("server starting", slog.String("addr", s.config.Addr))
	return s.httpServer.ListenAndServe()
}

// Serve starts the server on the given listener.
// Used by integration tests to start on a pre-bound :0 port.
func (s *Server) Serve(ln net.Listener) error {
	s.logger.Info("server serving", slog.String("addr", ln.Addr().String()))
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully stops the server, waiting for active connections to drain.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("server shutting down")
	return s.httpServer.Shutdown(ctx)
}
