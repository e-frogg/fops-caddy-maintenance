<h1 align="center">FroggOps Caddy Maintenance Plugin</h1>

<p align="center">
  A Caddy plugin that provides humans and SEO bots friendly maintenance page. 
  Including IP-based access control and customizable templates.
</p>

<div align="center">

[![License](https://img.shields.io/github/license/e-frogg/fops-caddy-maintenance)](https://github.com/e-frogg/fops-caddy-maintenance/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/e-frogg/fops-caddy-maintenance)](https://github.com/e-frogg/fops-caddy-maintenance/releases)
[![codecov](https://codecov.io/gh/e-frogg/fops-caddy-maintenance/graph/badge.svg?token=3RBE7W5I4B)](https://codecov.io/gh/e-frogg/fops-caddy-maintenance)
[![Go Report Card](https://goreportcard.com/badge/github.com/e-frogg/fops-caddy-maintenance)](https://goreportcard.com/report/github.com/e-frogg/fops-caddy-maintenance)

</div>

---

## ⚠️ Development Status

> **Note**
> This plugin is under active development. While being built for production environments with thorough testing and best practices in mind, please be aware of its current status, deployment in production environments is currently at your own risk


---

## 📋 Table of Contents

- [Features](#-features)
- [Installation](#-installation)
- [Configuration](#%EF%B8%8F-configuration)
- [API Reference](#-api-reference)
- [Performance Impact](#-performance-impact)
- [Real World Use Cases](#real-world-use-cases)
- [Development](#-development)


## ✨ Features

- Maintenance mode toggle via caddy adminAPI
- IP-based access control
- Custom HTML template support
- Configurable retry period

## 🔧 Build caddy with plugin

  ```shell
  xcaddy build --with github.com/e-frogg/fops-caddy-maintenance
  ```

## ⚙️ Configuration

Add the maintenance directive to your Caddyfile:

  ```caddy
  localhost {
    maintenance {
      # Path to custom HTML template
      template "/path/to/template.html"
      # List of IPs that can access during maintenance
      allowed_ips 192.168.1.100 10.0.0.1
      # Retry-After header value in seconds (default: 300)
      retry_after 3600
    }
  }
  ```

### Configuration Options

| Option | Description | Required |
|--------|-------------|----------|
| `template` | Path to custom HTML template | No |
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

## 📊 Performance Impact

The maintenance module has been thoroughly benchmarked using ApacheBench with the following test conditions:
- 1 million requests
- 100 concurrent connections
- Document size: 12 bytes
- Test duration: ~73 seconds

### Benchmark Results

The maintenance module shows negligible performance impact:
- Less than 1% decrease in request handling capacity
- Sub-millisecond increase in response time
- Perfect reliability maintained with zero failed requests

| Metric | Vanilla Caddy | With Maintenance Module | Impact |
|--------|---------------|------------------------|--------|
| Requests/sec | 13,638 | 13,528 | -0.81% |
| Time per request | 7.332ms | 7.392ms | +0.82% |
| Transfer rate | 1,917.91 KB/sec | 1,902.38 KB/sec | -0.81% |
| Failed requests | 0 | 0 | None |

## Real World Use Cases

### Website Maintenance Management Made Easy

Managing maintenance windows for any web platform can be challenging, especially in modern architectures. Here's how this plugin simplifies the process:

**Scenario**: 
- Web platform running as a Docker stack
- Caddy serving as the main entry point/reverse proxy
- Need for controlled maintenance periods to deploy new versions of the application

**Solution**:
The maintenance plugin enables seamless maintenance mode activation by:
1. Toggling maintenance mode through a simple API call to Caddy's admin interface
2. Instantly cutting off all incoming traffic
3. Displaying a maintenance page to all users
4. Safely performing required maintenance tasks, deployment, container rebuilds, etc.
5. Restoring service when ready

**Benefits**:
- Centralized control through Caddy
- No need to modify application code
- Clean customised user experience during maintenance
- SEO friendly interruption
- Perfect for containerized architectures

### Automated Maintenance Based on Critical Services Health

Automatically managing platform availability based on components health status.

**Scenario**: 
- Microservices architecture with critical dependencies
- Essential services like Database or Queue
- Need for automatic response to infrastructure issues
- Prevention of cascading failures

**Solution**:
The maintenance plugin can be integrated with Docker health checks:
1. Docker health checks monitor critical services
2. Custom script watches for health status changes
3. Maintenance mode automatically triggered when critical service fails
4. System remains protected until services are healthy again

**Benefits**:
- Automatic protection of system integrity
- Immediate response to infrastructure issues
- Clear communication to end users
- Prevention of data corruption
- Reduced manual intervention needs


## 👩‍💻 Development

Run these commands in the project root:

  ```shell
  make build  # Build the plugin
  make run    # Run with example configuration
  make test   # Run test suite
  ```