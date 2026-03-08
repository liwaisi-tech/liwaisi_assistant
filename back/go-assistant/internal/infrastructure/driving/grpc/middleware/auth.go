package middleware

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenValidator defines the interface for validating authorization tokens.
type TokenValidator interface {
	// ValidateToken checks if the token is valid and returns the associated claims or an error.
	ValidateToken(ctx context.Context, token string) error
}

// AuthInterceptor validates JWT tokens from the authorization metadata header.
// It uses the provided TokenValidator to perform the validation.
func AuthInterceptor(validator TokenValidator) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if validator == nil {
			return status.Errorf(codes.Unimplemented, "authentication is properly not configured for this server")
		}

		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Errorf(codes.Unauthenticated, "missing metadata")
		}

		authHeader, ok := md["authorization"]
		if !ok || len(authHeader) == 0 {
			return status.Errorf(codes.Unauthenticated, "authorization token is not supplied")
		}

		token := authHeader[0]
		if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			// Expected format is "Bearer <jwt>"
			return status.Errorf(codes.Unauthenticated, "invalid authorization format")
		}

		if err := validator.ValidateToken(ss.Context(), token[7:]); err != nil {
			return status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}

		return handler(srv, ss)
	}
}
