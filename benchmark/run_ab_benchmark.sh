#!/bin/bash

echo "🔍 Build test stack"
docker compose build
docker compose up -d

echo "📊 Testing vanilla Caddy"
docker compose -f docker-compose.yml -f docker-compose-ab.yml run --rm -it ab -c 100 -t 60 http://caddy/ > ./results/ab_vanilla.txt

sleep 5

echo "📊 Testing Caddy with maintenance module"
docker compose -f docker-compose.yml -f docker-compose-ab.yml run --rm -it ab -c 100 -t 60 http://caddy-with-maintenance/ > ./results/ab_with_module.txt