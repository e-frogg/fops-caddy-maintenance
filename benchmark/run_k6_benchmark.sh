#!/bin/bash

echo "🔍 Build test stack"
docker compose build
docker compose up -d

echo "📊 Testing vanilla Caddy"
docker compose run --rm k6 run \
  --out influxdb=http://influxdb:8086/k6 \
  /scripts/load_test.js

sleep 5

echo "📊 Testing Caddy with maintenance module"
docker compose run --rm k6 run \
  --out influxdb=http://influxdb:8086/k6 \
  /scripts/load_test_with_module.js