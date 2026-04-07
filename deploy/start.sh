#!/usr/bin/env bash
# ============================================================================
# BRAE Deployment Script
# Run as the 'brae' user from the project root.
#
# Usage:
#   bash deploy/start.sh          # Build and start
#   bash deploy/start.sh stop     # Stop all services
#   bash deploy/start.sh restart  # Rebuild and restart
#   bash deploy/start.sh logs     # Tail logs
#   bash deploy/start.sh status   # Show service status
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "${SCRIPT_DIR}")"

cd "${PROJECT_DIR}"

# Verify .env exists
if [ ! -f .env ]; then
    echo "ERROR: .env file not found in ${PROJECT_DIR}"
    echo "Copy deploy/.env.production to .env and fill in values."
    exit 1
fi

# Verify required variables
check_var() {
    local var_name="$1"
    local value
    value=$(grep "^${var_name}=" .env | cut -d= -f2-)
    if [ -z "${value}" ]; then
        echo "ERROR: ${var_name} is not set in .env"
        return 1
    fi
}

ACTION="${1:-start}"

case "${ACTION}" in
    start)
        echo "=== Starting BRAE ==="
        echo "Checking required environment variables..."
        MISSING=0
        check_var "OPENROUTER_API_KEY" || MISSING=1
        check_var "GOOGLE_CLIENT_ID" || MISSING=1
        check_var "VITE_GOOGLE_CLIENT_ID" || MISSING=1
        check_var "POSTGRES_PASSWORD" || MISSING=1
        check_var "ADMIN_EMAIL" || MISSING=1
        if [ "${MISSING}" -eq 1 ]; then
            echo ""
            echo "Fix the above errors in .env before starting."
            exit 1
        fi
        echo "All required variables set."
        echo ""
        echo "Building and starting services..."
        docker compose -f docker-compose.yml -f docker-compose.prod.yml build
        docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
        echo ""
        echo "Waiting for health checks..."
        sleep 5
        docker compose -f docker-compose.yml -f docker-compose.prod.yml ps
        echo ""
        echo "=== BRAE is running ==="
        echo "Frontend: http://127.0.0.1:3000 (via nginx: https://brae.liwaisi.tech)"
        echo "Backend:  http://127.0.0.1:8080 (internal only)"
        ;;

    stop)
        echo "=== Stopping BRAE ==="
        docker compose -f docker-compose.yml -f docker-compose.prod.yml down
        echo "Done."
        ;;

    restart)
        echo "=== Restarting BRAE ==="
        docker compose -f docker-compose.yml -f docker-compose.prod.yml down
        docker compose -f docker-compose.yml -f docker-compose.prod.yml build
        docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
        sleep 5
        docker compose -f docker-compose.yml -f docker-compose.prod.yml ps
        echo "Done."
        ;;

    logs)
        docker compose -f docker-compose.yml -f docker-compose.prod.yml logs -f --tail=100
        ;;

    status)
        docker compose -f docker-compose.yml -f docker-compose.prod.yml ps
        echo ""
        echo "Health checks:"
        curl -sf http://127.0.0.1:8080/api/v1/health 2>/dev/null && echo " Backend: OK" || echo " Backend: UNREACHABLE"
        curl -sf http://127.0.0.1:3000/ >/dev/null 2>&1 && echo " Frontend: OK" || echo " Frontend: UNREACHABLE"
        ;;

    *)
        echo "Usage: $0 {start|stop|restart|logs|status}"
        exit 1
        ;;
esac
