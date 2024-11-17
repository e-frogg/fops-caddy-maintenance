


# Response during maintenance mode

## Standard

## Accept: application/json

└── curl -H 'Accept: application/json' https://localhost
{"message":"Service temporarily unavailable for maintenance","status":"error"}


# Usage in a caddyfile





# Maintenance API

## Status
```shell
curl http://localhost:2019/maintenance/status
```

## Enable maintenance mode
```shell
curl -X POST \
     -H "Content-Type: application/json" \
     -d '{"enabled": true}' \
     http://localhost:2019/maintenance/set
```

## Disable maintenance mode
```shell
curl -X POST \
     -H "Content-Type: application/json" \
     -d '{"enabled": false}' \
     http://localhost:2019/maintenance/set
```


# Development

```shell
make build
make run
```