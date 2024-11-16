

# Build

```shell
xcaddy build --with github.com/e-frogg/frops-caddy-maintenance=.
sudo DEBUG=1 ./caddy run --config ./tests/Caddyfile
```


# Enable maintenance mode
curl -k -X POST \
     -H "Content-Type: application/json" \
     -d '{"enabled": true}' \
     https://localhost/api/maintenance

# Disable maintenance mode
curl -k -X POST \
     -H "Content-Type: application/json" \
     -d '{"enabled": false}' \
     https://localhost/api/maintenance