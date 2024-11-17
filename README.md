# FroggOps Caddy Maintenance Plugin

A Caddy server plugin that provides SEO friendly maintenance mode functionality with IP-based access control and customizable templates.

[![License](https://img.shields.io/github/license/e-frogg/fops-caddy-maintenance)](https://github.com/e-frogg/fops-caddy-maintenance/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/e-frogg/fops-caddy-maintenance)](https://github.com/e-frogg/fops-caddy-maintenance/releases)
[![codecov](https://codecov.io/gh/e-frogg/fops-caddy-maintenance/graph/badge.svg?token=3RBE7W5I4B)](https://codecov.io/gh/e-frogg/fops-caddy-maintenance)
[![Go Report Card](https://goreportcard.com/badge/github.com/e-frogg/fops-caddy-maintenance)](https://goreportcard.com/report/github.com/e-frogg/fops-caddy-maintenance)

## 📋 Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Configuration](#configuration)
- [API Reference](#api-reference)
- [Performance Impact](#performance-impact)
- [Development](#development)

## ✨ Features

- Maintenance mode toggle via caddy adminAPI
- IP-based access control
- Custom HTML template support
- Configurable retry period

## 🔧 Installation

*Installation instructions to be added*

## ⚙️ Configuration

Add the maintenance directive to your Caddyfile:

  ```caddy
  localhost {
    maintenance {
      template "/path/to/template.html"
      allowed_ips 192.168.1.100 10.0.0.1
      retry_after 800
    }
  }
  ```

### Configuration Options

| Option | Description | Required |
|--------|-------------|----------|
| `template` | Path to custom HTML template | Yes |
| `allowed_ips` | List of IPs that can access during maintenance | No |
| `retry_after` | Retry-After header value in seconds | No |

## 🚀 API Reference

### Check Maintenance Status

  ```shell
  curl http://localhost:2019/maintenance/status
  ```

### Enable Maintenance Mode

  ```shell
  curl -X POST \
       -H "Content-Type: application/json" \
       -d '{"enabled": true}' \
       http://localhost:2019/maintenance/set
  ```

### Disable Maintenance Mode

  ```shell
  curl -X POST \
       -H "Content-Type: application/json" \
       -d '{"enabled": false}' \
       http://localhost:2019/maintenance/set
  ```

## 📊 Performance

The maintenance module has been thoroughly benchmarked using ApacheBench with the following test conditions:
- Intel(R) Core(TM) i3-8100T CPU @ 3.10GHz with 8gb RAM
- 1 million requests
- 100 concurrent connections
- 60 seconds duration

### Benchmark Results

| Metric | Vanilla Caddy | With Maintenance Module | Impact |
|--------|---------------|------------------------|--------|
| Requests/sec | 13,688 | 13,560 | -0.94% |
| Time per request | 7.305ms | 7.374ms | +0.94% |
| Failed requests | 0 | 0 | None |

The maintenance module shows negligible performance impact:
- Less than 1% decrease in request handling capacity
- Sub-millisecond increase in response time
- Perfect reliability maintained with zero failed requests

The benchmark can be reproduced using the `run_ab_benchmark.sh` script in the `benchmark` directory.

## 👩‍💻 Development

Run these commands in the project root:

  ```shell
  make build  # Build the plugin
  make run    # Run with example configuration
  make test   # Run test suite
  ```