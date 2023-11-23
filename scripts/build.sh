#!/usr/bin/env bash

# Optional: If not using GHCR, let it build locally
#docker build -t instafix ../src/

# Refresh existing containers
docker-compose down
docker-compose up -d