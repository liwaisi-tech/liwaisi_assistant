package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/config"
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
func NewServer(cfg ServerConfig, appService *app.SessionService, logger *slog.Logger, billingFetcher BillingFetcher, opts ...ServerOption) *Server {
	broker := NewSSEBroker(logger)

	handlers := &Handlers{
		App:            appService,
		Broker:         broker,
		Logger:         logger,
		BillingFetcher: billingFetcher,
	}
	for _, opt := range opts {
		opt(handlers)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, handlers)

	// Build middleware chain.
	// Auth middleware is applied after CORS and before handlers.
	// When handlers.Verifier is nil (dev-mode), auth middleware passes through.
	middlewares := []func(http.Handler) http.Handler{
		CORSMiddleware(cfg.AllowedOrigins),
		AuthMiddleware(handlers.Verifier, logger, handlers.ConfigProvider),
		LoggingMiddleware(logger),
		RecoveryMiddleware(logger),
		RequestIDMiddleware,
	}
	// Rate limiting is applied after auth so user identity is available.
	if handlers.RateLimitCfg != nil {
		middlewares = append(middlewares, rateLimitMiddleware(*handlers.RateLimitCfg))
	}
	handler := Chain(mux, middlewares...)

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

// ServerOption configures optional server dependencies.
type ServerOption func(*Handlers)

// WithRepos injects persistence repositories for flow/execution endpoints.
func WithRepos(flowRepo persist.FlowRepository, intelRepo persist.IntelligenceRepository, eventRepo persist.EventRepository) ServerOption {
	return func(h *Handlers) {
		h.FlowRepo = flowRepo
		h.IntelRepo = intelRepo
		h.EventRepo = eventRepo
	}
}

// WithAuth injects a token verifier for authentication.
// When set, auth middleware is applied to protected routes.
// When nil, all requests pass through with a synthetic dev-user (dev-mode).
func WithAuth(verifier auth.TokenVerifier) ServerOption {
	return func(h *Handlers) {
		h.Verifier = verifier
	}
}

// WithUserRepo injects the user repository for user upsert on authentication.
func WithUserRepo(repo persist.UserRepository) ServerOption {
	return func(h *Handlers) {
		h.UserRepo = repo
	}
}

// WithPersonalityRepo injects the personality repository for personality endpoints.
func WithPersonalityRepo(repo persist.PersonalityRepository) ServerOption {
	return func(h *Handlers) {
		h.PersonalityRepo = repo
	}
}

// WithToolRegistry injects the tool registry for tools endpoints.
func WithToolRegistry(registry *tools.Registry) ServerOption {
	return func(h *Handlers) {
		h.ToolRegistry = registry
	}
}

// WithWaitlistRepo injects the waitlist repository for the public waitlist endpoint.
func WithWaitlistRepo(repo persist.WaitlistRepository) ServerOption {
	return func(h *Handlers) {
		h.WaitlistRepo = repo
	}
}

// WithRateLimiting enables rate limiting middleware with the given configuration.
func WithRateLimiting(cfg RateLimitConfig) ServerOption {
	return func(h *Handlers) {
		h.RateLimitCfg = &cfg
	}
}

// WithConfigProvider injects the config provider for admin config endpoints.
func WithConfigProvider(provider *config.Provider) ServerOption {
	return func(h *Handlers) {
		h.ConfigProvider = provider
	}
}

// WithAdminEmail sets the admin email for authorization.
func WithAdminEmail(email string) ServerOption {
	return func(h *Handlers) {
		h.AdminEmail = email
	}
}
