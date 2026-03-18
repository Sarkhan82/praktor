#!/bin/sh
set -e

git pull
docker compose pull
docker pull ghcr.io/mtzanidakis/praktor-agent-base:latest
docker compose build agent
docker compose up -d
docker system prune -f
