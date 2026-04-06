#!/usr/bin/env bash
# ============================================================================
# VPS Provisioning Script for BRAE (brae.liwaisi.tech)
# Run as root on a fresh Ubuntu 22.04+ / Debian 12+ VPS.
#
# Usage:
#   sudo bash deploy/setup-vps.sh
# ============================================================================
set -euo pipefail

BRAE_USER="brae"
DOMAIN="brae.liwaisi.tech"
SSH_PORT="${SSH_PORT:-22}"

echo "=== BRAE VPS Provisioning ==="
echo "Domain: ${DOMAIN}"
echo "SSH Port: ${SSH_PORT}"
echo ""

# ── 1. System updates ──────────────────────────────────────────────────────
echo "[1/8] Updating system packages..."
apt-get update -qq
apt-get upgrade -y -qq
apt-get install -y -qq \
    curl \
    gnupg \
    lsb-release \
    ca-certificates \
    unattended-upgrades \
    apt-listchanges \
    fail2ban \
    ufw \
    nginx \
    certbot \
    python3-certbot-nginx

# ── 2. Automatic security updates ─────────────────────────────────────────
echo "[2/8] Configuring automatic security updates..."
cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF

cat > /etc/apt/apt.conf.d/50unattended-upgrades <<'EOF'
Unattended-Upgrade::Allowed-Origins {
    "${distro_id}:${distro_codename}-security";
    "${distro_id}ESMApps:${distro_codename}-apps-security";
    "${distro_id}ESM:${distro_codename}-infra-security";
};
Unattended-Upgrade::AutoFixInterruptedDpkg "true";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Automatic-Reboot "false";
EOF

systemctl enable unattended-upgrades
systemctl start unattended-upgrades

# ── 3. Create brae user (non-sudoer) ──────────────────────────────────────
echo "[3/8] Creating '${BRAE_USER}' user..."
if id "${BRAE_USER}" &>/dev/null; then
    echo "  User '${BRAE_USER}' already exists, skipping."
else
    useradd -m -s /bin/bash "${BRAE_USER}"
    echo "  User '${BRAE_USER}' created with home /home/${BRAE_USER}"
fi

# ── 4. Install Docker ─────────────────────────────────────────────────────
echo "[4/8] Installing Docker..."
if command -v docker &>/dev/null; then
    echo "  Docker already installed, skipping."
else
    curl -fsSL https://get.docker.com | sh
fi

# Add brae to docker group (allows running containers without sudo)
usermod -aG docker "${BRAE_USER}"
echo "  User '${BRAE_USER}' added to docker group."

# ── 5. SSH hardening ──────────────────────────────────────────────────────
echo "[5/8] Hardening SSH..."
SSHD_CONFIG="/etc/ssh/sshd_config"

# Backup original config
cp "${SSHD_CONFIG}" "${SSHD_CONFIG}.backup.$(date +%Y%m%d)"

# Apply hardening (idempotent using sed)
sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' "${SSHD_CONFIG}"
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' "${SSHD_CONFIG}"
sed -i 's/^#\?ChallengeResponseAuthentication.*/ChallengeResponseAuthentication no/' "${SSHD_CONFIG}"
sed -i 's/^#\?UsePAM.*/UsePAM yes/' "${SSHD_CONFIG}"
sed -i 's/^#\?X11Forwarding.*/X11Forwarding no/' "${SSHD_CONFIG}"
sed -i "s/^#\?Port.*/Port ${SSH_PORT}/" "${SSHD_CONFIG}"

# Add MaxAuthTries if not present
if ! grep -q "^MaxAuthTries" "${SSHD_CONFIG}"; then
    echo "MaxAuthTries 3" >> "${SSHD_CONFIG}"
fi

# Add LoginGraceTime if not present
if ! grep -q "^LoginGraceTime" "${SSHD_CONFIG}"; then
    echo "LoginGraceTime 30" >> "${SSHD_CONFIG}"
fi

systemctl restart sshd

echo "  SSH hardened: password auth disabled, root login restricted, port ${SSH_PORT}"
echo "  IMPORTANT: Ensure your SSH key is in /root/.ssh/authorized_keys BEFORE logging out!"

# ── 6. Firewall (UFW) ────────────────────────────────────────────────────
echo "[6/8] Configuring UFW firewall..."
ufw --force reset

ufw default deny incoming
ufw default allow outgoing

# SSH
ufw allow "${SSH_PORT}/tcp" comment "SSH"

# HTTP & HTTPS (nginx)
ufw allow 80/tcp comment "HTTP"
ufw allow 443/tcp comment "HTTPS"

ufw --force enable
echo "  UFW enabled: only ports ${SSH_PORT}, 80, 443 open."

# ── 7. Fail2ban ──────────────────────────────────────────────────────────
echo "[7/8] Configuring fail2ban..."
cat > /etc/fail2ban/jail.local <<EOF
[DEFAULT]
bantime = 1h
findtime = 10m
maxretry = 5
backend = systemd

[sshd]
enabled = true
port = ${SSH_PORT}
maxretry = 3
bantime = 3600

[nginx-http-auth]
enabled = true

[nginx-limit-req]
enabled = true
logpath = /var/log/nginx/brae.liwaisi.tech.error.log
maxretry = 10
findtime = 60
bantime = 600
EOF

systemctl enable fail2ban
systemctl restart fail2ban
echo "  Fail2ban configured for SSH and nginx."

# ── 8. Nginx setup ───────────────────────────────────────────────────────
echo "[8/8] Setting up nginx..."

# Remove default site
rm -f /etc/nginx/sites-enabled/default

# Create certbot webroot
mkdir -p /var/www/certbot

# The nginx site config should be copied from deploy/nginx/brae.liwaisi.tech.conf
echo "  Nginx installed. Next steps:"
echo "    1. Copy nginx config:"
echo "       sudo cp deploy/nginx/brae.liwaisi.tech.conf /etc/nginx/sites-available/"
echo "       sudo ln -s /etc/nginx/sites-available/brae.liwaisi.tech.conf /etc/nginx/sites-enabled/"
echo "    2. Get TLS certificate (temporarily comment out ssl lines first, or use --standalone):"
echo "       sudo certbot --nginx -d ${DOMAIN}"
echo "    3. Test and reload:"
echo "       sudo nginx -t && sudo systemctl reload nginx"

# ── Summary ──────────────────────────────────────────────────────────────
echo ""
echo "============================================"
echo "  VPS Provisioning Complete"
echo "============================================"
echo ""
echo "  User:      ${BRAE_USER} (non-sudoer, docker group)"
echo "  SSH:       Port ${SSH_PORT}, key-only, root restricted"
echo "  Firewall:  Ports ${SSH_PORT}, 80, 443 only"
echo "  Fail2ban:  SSH (3 retries), nginx rate-limit"
echo "  Updates:   Automatic security patches enabled"
echo ""
echo "  Next steps:"
echo "    1. Copy your SSH public key to /home/${BRAE_USER}/.ssh/authorized_keys"
echo "    2. Clone the repo to /home/${BRAE_USER}/liwaisi_assistant"
echo "    3. Copy .env.production to .env and fill in values"
echo "    4. Run: sudo -u ${BRAE_USER} bash deploy/start.sh"
echo ""
echo "  SECURITY REMINDER:"
echo "    - Rotate the OpenRouter API key (it was in git history)"
echo "    - Set ALLOWED_EMAILS in .env to restrict access"
echo "    - Set CORS_ORIGINS=https://brae.liwaisi.tech"
echo ""
