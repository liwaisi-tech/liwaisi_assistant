# ── Build stage ──
FROM node:22-alpine AS builder

WORKDIR /build

# Cache dependencies
COPY package.json package-lock.json ./
RUN npm ci --network-timeout=120000

# Copy source and build
COPY . .
RUN npm run build

# ── Runtime stage ──
FROM nginx:1.27-alpine

# Remove default config
RUN rm /etc/nginx/conf.d/default.conf

# Copy nginx config
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy built assets
COPY --from=builder /build/dist /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:80/ || exit 1
