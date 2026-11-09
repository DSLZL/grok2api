#!/bin/sh
set -eu

umask 077

if [ ! -f "${GROK2API_CONFIG_SOURCE}" ]; then
  echo "missing config: ${GROK2API_CONFIG_SOURCE}" >&2
  echo "mount config.yaml to /run/grok2api/config.yaml" >&2
  exit 1
fi

cp "${GROK2API_CONFIG_SOURCE}" /app/config.yaml
chown grok2api:grok2api /app/config.yaml
chmod 0600 /app/config.yaml

if [ "$#" -gt 0 ] && [ "$1" = "/app/grok2api" ] && [ "${2:-}" != "migrate" ]; then
  su-exec grok2api:grok2api /app/grok2api migrate --config /app/config.yaml
fi

exec su-exec grok2api:grok2api "$@"

