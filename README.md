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

## 💡 Benefits
- Centralized control through Caddy
- No need to modify application code
- Clean customised user experience during maintenance
- SEO friendly interruption
- Perfect for containerized architectures
- Reduced manual intervention needs

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
| `default_enabled` | Enable maintenance mode by default at startup | No |
| `status_file` | Path to file for persisting maintenance status | No |
| `request_retention_mode_timeout` | Time in seconds to retain requests during maintenance | No |

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

### Enable Maintenance Mode with request retention for 10 seconds

  ```shell
  curl -X POST \
       -H "Content-Type: application/json" \
       -d '{"enabled": true, "request_retention_mode_timeout": 10}' \
       http://localhost:2019/maintenance/set
  ```

### Disable Maintenance Mode

  ```shell
  curl -X POST \
       -H "Content-Type: application/json" \
       -d '{"enabled": false}' \
       http://localhost:2019/maintenance/set
  ```

## Advanced Configuration Examples

### Default Maintenance Mode for Pre-production Environments

```caddy
preprod.example.com {
  maintenance {
    # Enable maintenance by default at startup
    default_enabled true
    # Allow specific IPs to access during maintenance
    allowed_ips 192.168.1.100 10.0.0.1
    # Custom template
    template "/path/to/template.html"
  }
}
```

### Persistent Maintenance Status

```caddy
example.com {
  maintenance {
    # Persist maintenance status to survive restarts
    status_file /var/lib/caddy/maintenance.json
    # Retry-After header value in seconds
    retry_after 300
  }
}
```


## Real World Use Cases

### Website Maintenance Management Made Easy

**Scenario**: 
- Web platform running as a Docker stack
- Caddy serving as the main entry point/reverse proxy
- Need for controlled maintenance periods to deploy new release or perform maintenance tasks

**Solution**:
The maintenance plugin enables seamless maintenance mode activation by:
1. Toggling maintenance mode through a simple API call to Caddy's admin interface
2. Instantly cutting off all incoming traffic
3. Displaying a maintenance page to all users
4. Safely performing required maintenance tasks, deployment, container rebuilds, etc.
5. Restoring service when ready

### Micro Maintenance with requests retention

**Scenario**: 
- Web platform running as a Docker stack
- Caddy serving as the main entry point/reverse proxy
- Need for short maintenance periods to perform quick tasks without service interruption

**Solution**:
1. Toggling maintenance on through API call to Caddy's admin interface with request retention timeout configuration 
2. Caddy instantly retain incoming requests for the predefined period until maintenance mode is disabled or display a maintenance page if timeout is reached
3. Toggling maintenance off through API, the retained requests are released and forwarded to the backend


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


## 👩‍💻 Development

Run these commands in the project root:

  ```shell
  make build  # Build the plugin
  make run    # Run with example configuration
  make test   # Run test suite
  ```