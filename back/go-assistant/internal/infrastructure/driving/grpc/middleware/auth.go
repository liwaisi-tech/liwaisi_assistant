package middleware

import (
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthInterceptor validates JWT tokens from the authorization metadata header.
func AuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
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

		// Mock JWT validation:
		// In a real application, parse and validate the JWT signature here.
		// claims, err := validateJWT(token[7:])
		// if err != nil { return status.Errorf(codes.Unauthenticated, ...) }

		return handler(srv, ss)
	}
}
