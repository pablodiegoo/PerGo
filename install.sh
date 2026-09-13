#!/usr/bin/env bash
# =============================================================================
# PerGo — 1-Click Production Installer for Linux / VPS
# =============================================================================
# High-performance, self-hosted Omnichannel CPaaS Gateway (Go + NATS + Postgres).
# Automated provisioning with Traefik v3 reverse proxy & Let's Encrypt SSL/TLS.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/pablodiegoo/Ecoar/main/PerGo/install.sh | bash
# Or locally:
#   ./install.sh [options]
# =============================================================================

set -euo pipefail

# Text formatting
BOLD='\033[1m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${CYAN}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

banner() {
    echo -e "${CYAN}${BOLD}"
    cat << "EOF"
  ____             ____       
 |  _ \ ___ _ __  / ___| ___  
 | |_) / _ \ '__|| |  _ / _ \ 
 |  __/  __/ |   | |_| | (_) |
 |_|   \___|_|    \____|\___/ 
 High-Performance Omnichannel CPaaS
EOF
    echo -e "${NC}"
}

print_help() {
    echo "Usage: ./install.sh [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -d, --domain <domain>     Domain name for PerGo (e.g. api.pergo.example.com)"
    echo "  -e, --email <email>       Email for Let's Encrypt TLS certificate notices"
    echo "  -p, --password <password> Custom admin password (generated automatically if omitted)"
    echo "  --dir <path>              Installation directory (default: current directory or /opt/pergo)"
    echo "  -y, --yes, --unattended   Run non-interactively with defaults where possible"
    echo "  --no-start                Configure environment only without starting containers"
    echo "  -h, --help                Show this help message"
    echo ""
}

# Defaults
DOMAIN="${DOMAIN:-}"
ACME_EMAIL="${ACME_EMAIL:-}"
PERGO_ADMIN_PASSWORD="${PERGO_ADMIN_PASSWORD:-}"
INSTALL_DIR="${INSTALL_DIR:-}"
UNATTENDED=false
NO_START=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        -d|--domain)
            DOMAIN="$2"
            shift 2
            ;;
        -e|--email)
            ACME_EMAIL="$2"
            shift 2
            ;;
        -p|--password)
            PERGO_ADMIN_PASSWORD="$2"
            shift 2
            ;;
        --dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        -y|--yes|--unattended)
            UNATTENDED=true
            shift
            ;;
        --no-start)
            NO_START=true
            shift
            ;;
        -h|--help)
            print_help
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            print_help
            exit 1
            ;;
    esac
done

# Detect OS
OS_TYPE="$(uname -s)"
if [[ "$OS_TYPE" != "Linux" && "$OS_TYPE" != "Darwin" ]]; then
    log_error "Unsupported Operating System: $OS_TYPE. PerGo requires Linux or macOS."
    exit 1
fi

banner

# -----------------------------------------------------------------------------
# 1. Dependency Validation & Automatic Installation
# -----------------------------------------------------------------------------
log_info "Step 1/5: Checking system dependencies..."

# Check curl
if ! command -v curl >/dev/null 2>&1; then
    log_warn "curl not found. Attempting installation..."
    if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get update -y && sudo apt-get install -y curl
    elif command -v yum >/dev/null 2>&1; then
        sudo yum install -y curl
    else
        log_error "curl is required. Please install curl and re-run."
        exit 1
    fi
fi

# Check Docker
if ! command -v docker >/dev/null 2>&1; then
    log_warn "Docker is not installed."
    if [[ "$OS_TYPE" == "Linux" ]]; then
        if [[ "$UNATTENDED" == "true" ]]; then
            INSTALL_DOCKER="y"
        else
            read -rp "Would you like to install Docker automatically via get.docker.com? [y/N]: " INSTALL_DOCKER
        fi
        if [[ "$INSTALL_DOCKER" =~ ^[Yy]$ ]]; then
            log_info "Installing Docker via official script..."
            curl -fsSL https://get.docker.com | sh
            if [[ -n "${SUDO_USER:-}" ]]; then
                sudo usermod -aG docker "$SUDO_USER" || true
            else
                sudo usermod -aG docker "$USER" || true
            fi
            log_success "Docker installed successfully."
        else
            log_error "Docker is required to run PerGo in production. Aborting."
            exit 1
        fi
    else
        log_error "Please install Docker Desktop for macOS and re-run."
        exit 1
    fi
fi

# Detect Docker Compose command
DOCKER_COMPOSE_CMD=""
if docker compose version >/dev/null 2>&1; then
    DOCKER_COMPOSE_CMD="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
    DOCKER_COMPOSE_CMD="docker-compose"
else
    log_warn "Docker Compose not detected."
    if command -v apt-get >/dev/null 2>&1; then
        log_info "Attempting to install docker-compose-plugin via apt..."
        sudo apt-get update -y && sudo apt-get install -y docker-compose-plugin
        DOCKER_COMPOSE_CMD="docker compose"
    else
        log_error "Docker Compose v2 is required. Please install docker-compose and re-run."
        exit 1
    fi
fi
log_success "Detected Docker Compose: $DOCKER_COMPOSE_CMD"

# Verify Docker daemon is running
if ! docker info >/dev/null 2>&1; then
    log_warn "Docker daemon is not responding. Trying to start docker service..."
    if command -v systemctl >/dev/null 2>&1; then
        sudo systemctl start docker || true
        sleep 2
    fi
    if ! docker info >/dev/null 2>&1; then
        log_error "Cannot connect to Docker daemon. Check permissions or run 'sudo systemctl start docker'."
        exit 1
    fi
fi
log_success "Docker daemon is active and accessible."

# -----------------------------------------------------------------------------
# 2. Target Directory & File Preparation
# -----------------------------------------------------------------------------
log_info "Step 2/5: Setting up installation directory..."

SCRIPT_SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Determine target directory
if [[ -z "$INSTALL_DIR" ]]; then
    if [[ -f "docker-compose.prod.yml" ]]; then
        INSTALL_DIR="$(pwd)"
    elif [[ -f "PerGo/docker-compose.prod.yml" ]]; then
        INSTALL_DIR="$(pwd)/PerGo"
    else
        if [[ "$EUID" -eq 0 ]]; then
            INSTALL_DIR="/opt/pergo"
        else
            INSTALL_DIR="$HOME/pergo"
        fi
    fi
fi

mkdir -p "$INSTALL_DIR"

# If docker-compose.prod.yml is missing, copy locally or download
if [[ ! -f "$INSTALL_DIR/docker-compose.prod.yml" ]]; then
    if [[ -f "$SCRIPT_SOURCE_DIR/docker-compose.prod.yml" ]]; then
        log_info "Copying docker-compose.prod.yml from local repository..."
        cp "$SCRIPT_SOURCE_DIR/docker-compose.prod.yml" "$INSTALL_DIR/docker-compose.prod.yml"
    else
        log_info "Downloading docker-compose.prod.yml from official repository..."
        curl -fsSL https://raw.githubusercontent.com/pablodiegoo/Ecoar/main/PerGo/docker-compose.prod.yml -o "$INSTALL_DIR/docker-compose.prod.yml" || {
            log_error "Failed to download docker-compose.prod.yml. Ensure network connectivity."
            exit 1
        }
    fi
fi

cd "$INSTALL_DIR"
log_info "Working directory: $INSTALL_DIR"

# -----------------------------------------------------------------------------
# 3. Interactive Configuration & Secrets Generation
# -----------------------------------------------------------------------------
log_info "Step 3/5: Configuring environment and generating cryptographic secrets..."

# Prompt for Domain if empty
if [[ -z "$DOMAIN" ]]; then
    if [[ "$UNATTENDED" == "true" ]]; then
        DOMAIN="localhost"
    else
        echo ""
        echo -e "${BOLD}Domain Configuration:${NC}"
        echo "Enter the fully-qualified domain name (FQDN) where PerGo will be accessed."
        echo "Example: api.pergo.yourdomain.com"
        read -rp "Domain: " DOMAIN
        # Strip scheme and trailing slashes
        DOMAIN="$(echo "$DOMAIN" | sed -e 's|^[^/]*//||' -e 's|/.*$||')"
    fi
fi

if [[ -z "$DOMAIN" ]]; then
    log_error "Domain cannot be empty. Please provide a valid domain name."
    exit 1
fi

# Prompt for ACME Email if empty
if [[ -z "$ACME_EMAIL" ]]; then
    if [[ "$UNATTENDED" == "true" ]]; then
        ACME_EMAIL="admin@${DOMAIN}"
    else
        echo ""
        echo -e "${BOLD}Let's Encrypt SSL/TLS Configuration:${NC}"
        echo "Enter your contact email for automated Let's Encrypt certificate renewal notices."
        read -rp "Email [admin@${DOMAIN}]: " INPUT_EMAIL
        ACME_EMAIL="${INPUT_EMAIL:-admin@${DOMAIN}}"
    fi
fi

# Cryptographic Key Generation helpers
generate_random_string() {
    local length="${1:-24}"
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex "$((length / 2))"
    else
        tr -dc 'a-zA-Z0-9' < /dev/urandom | head -c "$length" || true
    fi
}

generate_kek_base64() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -base64 32
    elif command -v python3 >/dev/null 2>&1; then
        python3 -c "import secrets, base64; print(base64.b64encode(secrets.token_bytes(32)).decode())"
    else
        head -c 32 /dev/urandom | base64 | tr -d '\n'
    fi
}

# Generate Admin Password if empty
if [[ -z "$PERGO_ADMIN_PASSWORD" ]]; then
    PERGO_ADMIN_PASSWORD="$(generate_random_string 24)"
fi

POSTGRES_PASSWORD="$(generate_random_string 24)"
PERGO_SESSION_SECRET="$(generate_random_string 32)"
PERGO_KEK_BASE64="$(generate_kek_base64)"

# Write .env file
ENV_FILE="$INSTALL_DIR/.env"
if [[ -f "$ENV_FILE" ]]; then
    log_warn "Existing .env file detected at $ENV_FILE. Creating backup .env.backup.$(date +%s)..."
    cp "$ENV_FILE" "${ENV_FILE}.backup.$(date +%s)"
fi

log_info "Writing production environment settings to $ENV_FILE..."
cat > "$ENV_FILE" << EOF
# PerGo Production Environment Configuration
# Generated automatically by install.sh on $(date -u +"%Y-%m-%dT%H:%M:%SZ")

DOMAIN=${DOMAIN}
ACME_EMAIL=${ACME_EMAIL}

POSTGRES_USER=pergo
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=pergo

PERGO_ENV=production
PERGO_SERVER_PORT=8080
PERGO_ADMIN_PASSWORD=${PERGO_ADMIN_PASSWORD}
PERGO_SESSION_SECRET=${PERGO_SESSION_SECRET}
PERGO_KEK_BASE64=${PERGO_KEK_BASE64}

# Media & Data Retention Settings (LGPD)
MEDIA_RETENTION_DAYS=30
AUDIT_RETENTION_DAYS=90
EOF

chmod 600 "$ENV_FILE"
log_success "Environment configured successfully with secure permissions (chmod 600)."

# -----------------------------------------------------------------------------
# 4. Service Deployment
# -----------------------------------------------------------------------------
if [[ "$NO_START" == "true" ]]; then
    log_info "Step 4/5 skipped (--no-start specified)."
else
    log_info "Step 4/5: Deploying containers with Docker Compose..."

    # Check if local Dockerfile exists to build, otherwise compose uses image
    BUILD_FLAG=""
    if [[ -f "Dockerfile" ]]; then
        BUILD_FLAG="--build"
    fi

    log_info "Starting Traefik, PostgreSQL, NATS JetStream, and PerGo..."
    $DOCKER_COMPOSE_CMD -f docker-compose.prod.yml up $BUILD_FLAG -d

    log_info "Waiting for services to become healthy..."
    ATTEMPTS=0
    MAX_ATTEMPTS=30
    HEALTHY=false

    while [[ $ATTEMPTS -lt $MAX_ATTEMPTS ]]; do
        ATTEMPTS=$((ATTEMPTS + 1))
        # Check if pergo container is running
        if docker ps --filter "name=pergo-app" --format "{{.Status}}" | grep -q "Up"; then
            HEALTHY=true
            break
        fi
        sleep 2
        echo -n "."
    done
    echo ""

    if [[ "$HEALTHY" == "true" ]]; then
        log_success "PerGo cluster is up and running!"
    else
        log_warn "Containers are starting. Use '${DOCKER_COMPOSE_CMD} -f docker-compose.prod.yml logs -f' to inspect."
    fi
fi

# -----------------------------------------------------------------------------
# 5. Final Summary & Operations Guide
# -----------------------------------------------------------------------------
log_info "Step 5/5: Deployment completed."

echo ""
echo -e "${GREEN}${BOLD}===============================================================================${NC}"
echo -e "${GREEN}${BOLD}                  PerGo Open-Source CPaaS Deployed Successfully!               ${NC}"
echo -e "${GREEN}${BOLD}===============================================================================${NC}"
echo ""
echo -e "  ${BOLD}Public URL:${NC}         https://${DOMAIN}"
echo -e "  ${BOLD}Operator Console:${NC}   https://${DOMAIN}/admin"
echo -e "  ${BOLD}Interactive Docs:${NC}   https://${DOMAIN}/docs  (OpenAPI 3.1 Scalar Portal)"
echo -e "  ${BOLD}Health Liveness:${NC}    https://${DOMAIN}/healthz"
echo -e "  ${BOLD}Health Readiness:${NC}   https://${DOMAIN}/readyz"
echo ""
echo -e "  ${BOLD}Admin Password:${NC}     ${YELLOW}${BOLD}${PERGO_ADMIN_PASSWORD}${NC}"
echo -e "  ${BOLD}Config Location:${NC}    ${INSTALL_DIR}/.env"
echo ""
echo -e "${YELLOW}IMPORTANT: Save your Admin Password and Key Encryption Key (KEK) safely!${NC}"
echo -e "The KEK in ${INSTALL_DIR}/.env encrypts all channel credentials at rest."
echo ""
echo -e "${BOLD}Management Commands:${NC}"
echo "  cd ${INSTALL_DIR}"
echo "  View logs:          ${DOCKER_COMPOSE_CMD} -f docker-compose.prod.yml logs -f"
echo "  Check status:       ${DOCKER_COMPOSE_CMD} -f docker-compose.prod.yml ps"
echo "  Restart services:   ${DOCKER_COMPOSE_CMD} -f docker-compose.prod.yml restart"
echo "  Stop services:      ${DOCKER_COMPOSE_CMD} -f docker-compose.prod.yml down"
echo ""
echo -e "${GREEN}Quick API Test (Verify after DNS propagation):${NC}"
echo "  curl -k https://${DOMAIN}/healthz"
echo ""
