# Liwaisi Assistant -- root Makefile
# ──────────────────────────────────────────────────────────────

# ── SOPS / secrets management ────────────────────────────────

.PHONY: sops-setup sops-encrypt sops-decrypt sops-edit deploy

## Initialize age keypair for SOPS secret management
sops-setup:
	@bash scripts/sops-setup.sh

## Encrypt .env.sops.yaml in place
sops-encrypt:
	sops encrypt -i .env.sops.yaml

## Decrypt .env.sops.yaml to .env for local use
sops-decrypt:
	sops decrypt .env.sops.yaml > .env

## Open .env.sops.yaml in your editor for editing
sops-edit:
	sops .env.sops.yaml

## Decrypt secrets and start all services
deploy: sops-decrypt
	docker compose up -d
