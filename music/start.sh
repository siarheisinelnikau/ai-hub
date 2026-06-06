#!/bin/bash
# =============================================================================
# ACE-Step 1.5 — Quick Start Script
# =============================================================================
# Builds and launches ACE-Step 1.5 music generation service.
#
# Usage:
#   ./start.sh              # Build and start (first time)
#   ./start.sh --no-build   # Start without rebuilding
#   ./start.sh --api        # Start REST API server instead of Gradio
#   ./start.sh --stop       # Stop the service
#
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Defaults
BUILD=true
MODE="gradio"
ACTION="up"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-build) BUILD=false; shift ;;
        --api) MODE="api"; shift ;;
        --stop) ACTION="down"; shift ;;
        --logs) ACTION="logs"; shift ;;
        --status) ACTION="status"; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Create required directories
mkdir -p /srv/ai/models/music/checkpoints
mkdir -p /srv/ai/models/music/outputs

# Copy .env if not present
if [ ! -f .env ]; then
    echo "Creating .env from .env.example..."
    cp .env.example .env
fi

# Override mode if --api was passed
if [ "$MODE" = "api" ]; then
    export ACESTEP_MODE=api
fi

case "$ACTION" in
    down)
        echo "Stopping ACE-Step..."
        docker compose down
        echo "Stopped."
        ;;
    logs)
        docker compose logs -f --tail=100
        ;;
    status)
        docker compose ps
        echo ""
        echo "Health check:"
        curl -sf http://localhost:8016/ > /dev/null 2>&1 && echo "  Gradio UI: OK" || echo "  Gradio UI: DOWN"
        ;;
    up)
        if [ "$BUILD" = true ]; then
            echo "Building ACE-Step 1.5 image..."
            docker compose build
        fi

        echo "Starting ACE-Step 1.5 (mode: $MODE)..."
        docker compose up -d

        echo ""
        echo "==========================================="
        echo " ACE-Step 1.5 is starting up"
        echo "==========================================="
        echo ""
        echo "  Gradio UI: http://localhost:8016"
        echo "  REST API:  http://localhost:8017"
        echo ""
        echo "  First startup downloads ~20GB of models."
        echo "  Monitor progress: docker compose logs -f"
        echo ""
        echo "  Check status:  ./start.sh --status"
        echo "  View logs:     ./start.sh --logs"
        echo "  Stop service:  ./start.sh --stop"
        echo "==========================================="
        ;;
esac
