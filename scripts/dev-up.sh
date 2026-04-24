#!/usr/bin/env bash
set -euo pipefail

# Starts the local dev stack (Postgres + MinIO) and waits for health.
# Idempotent: safe to run repeatedly.

cd "$(dirname "$0")/.."

docker compose up -d --build postgres minio minio-setup

echo
echo "Waiting for Postgres to be healthy..."
until [ "$(docker inspect -f '{{.State.Health.Status}}' jacaranda-postgres 2>/dev/null || echo starting)" = "healthy" ]; do
  sleep 1
done
echo "Postgres: healthy (localhost:55432, user=jacaranda db=jacaranda)"

echo "Waiting for MinIO to be healthy..."
until [ "$(docker inspect -f '{{.State.Health.Status}}' jacaranda-minio 2>/dev/null || echo starting)" = "healthy" ]; do
  sleep 1
done
echo "MinIO:    healthy (S3 at localhost:9000, console at http://localhost:9001)"
echo
echo "Dev stack ready."
