package fopsMaintenance

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(&MaintenanceHandler{})
	httpcaddyfile.RegisterHandlerDirective("maintenance", parseCaddyfile)
}

// MaintenanceHandler handles maintenance mode functionality
type MaintenanceHandler struct {
	// Custom HTML template for maintenance page
	HTMLTemplate string `json:"html_template,omitempty"`

	// List of IPs allowed to bypass maintenance mode
	AllowedIPs []string `json:"allowed_ips,omitempty"`

	// Retry-After header value in seconds
	RetryAfter int `json:"retry_after,omitempty"`

	// Default state of maintenance mode at startup
	DefaultEnabled bool `json:"default_enabled,omitempty"`

	// File path to persist maintenance status
	StatusFile string `json:"status_file,omitempty"`

	// Maintenance mode state
	enabled    bool
	enabledMux sync.RWMutex

	// Request retention mode timeout in seconds
	RequestRetentionModeTimeout int `json:"request_retention_mode_timeout,omitempty"`

	logger *zap.Logger
	ctx    caddy.Context
}

// CaddyModule returns the Caddy module information.
func (*MaintenanceHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.fops_maintenance",
		New: func() caddy.Module { return new(MaintenanceHandler) },
	}
}

// Provision implements caddy.Provisioner.
func (h *MaintenanceHandler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger()
	h.ctx = ctx

	// Register the maintenance handler
	setMaintenanceHandler(h)

	// Load template file if path is provided
	if h.HTMLTemplate != "" {
		content, err := os.ReadFile(h.HTMLTemplate)
		if err != nil {
			return fmt.Errorf("failed to read template file: %v", err)
		}
		h.HTMLTemplate = string(content)
	}

	// Try to load persisted status if StatusFile is configured
	if h.StatusFile != "" {
		if data, err := os.ReadFile(h.StatusFile); err == nil {
			var status struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.Unmarshal(data, &status); err == nil {
				h.enabledMux.Lock()
				h.enabled = status.Enabled
				h.enabledMux.Unlock()
				return nil
			}
		}
	}

	// If no persisted status, use DefaultEnabled
	h.enabledMux.Lock()
	h.enabled = h.DefaultEnabled
	h.enabledMux.Unlock()

	return nil
}

// Interface guards
var (
	_ caddy.Provisioner           = (*MaintenanceHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*MaintenanceHandler)(nil)
)

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *MaintenanceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	h.enabledMux.RLock()
	enabled := h.enabled
	temporaryModeEnabled := h.RequestRetentionModeTimeout > 0
	h.enabledMux.RUnlock()

	if !enabled {
		return next.ServeHTTP(w, r)
	}

	// Check if client IP is in allowed list
	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}
	for _, allowedIP := range h.AllowedIPs {
		if clientIP == allowedIP {
			return next.ServeHTTP(w, r)
		}
	}

	// Request retention mode disabled, serve maintenance page now
	if !temporaryModeEnabled {
		return serveMaintenancePage(r, w, h)
	}

	// Request retention mode enabled, retain request for the predefined period
	timer := time.NewTimer(time.Duration(h.RequestRetentionModeTimeout) * time.Second)
	for {
		// Wait for the timer to expire, the context to be cancelled or the maintenance mode to be disabled
		// Context can be cancelled in several real-world scenarios:
		// Client connection closed, Caddy config reload, Server graceful shutdown (SIGTERM)....
		select {
		// Timeout reached, serve maintenance page
		case <-timer.C:
			return serveMaintenancePage(r, w, h)
		// Context cancelled, serve maintenance page
		case <-h.ctx.Done():
			return serveMaintenancePage(r, w, h)
		// Check every second the "enabled" state
		case <-time.After(1000 * time.Millisecond):
			h.enabledMux.RLock()
			enabled := h.enabled
			h.enabledMux.RUnlock()
			if !enabled {
				// Mode maintenance désactivé, transférer la requête
				return next.ServeHTTP(w, r)
			}
		}
	}
}

func serveMaintenancePage(r *http.Request, w http.ResponseWriter, h *MaintenanceHandler) error {
	// Set Retry-After header with default value if not specified
	retryAfter := defaultRetryAfter
	if h.RetryAfter > 0 {
		retryAfter = h.RetryAfter
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	w.WriteHeader(http.StatusServiceUnavailable)

	// Check if client accepts JSON
	if isJSONRequest(r) {
		return serveJSON(w)
	}

	// Serve HTML maintenance page
	return serveHTML(w, h.HTMLTemplate)
}

func isJSONRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return accept == "application/json" || r.Header.Get("Content-Type") == "application/json"
}

func serveJSON(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")

	response := map[string]string{
		"status":  "error",
		"message": "Service temporarily unavailable for maintenance",
	}
	return json.NewEncoder(w).Encode(response)
}

func serveHTML(w http.ResponseWriter, template string) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if template == "" {
		template = defaultHTMLTemplate
	}
	_, err := w.Write([]byte(template))
	return err
}

const defaultHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Maintenance in Progress</title>
    <meta name="robots" content="noindex">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        :root {
            --primary-color: #2563eb;
            --secondary-color: #4b5563;
            --text-color: #1f2937;
            --bg-color: #f3f4f6;
            --container-bg-color: #ffffff;
        }
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            line-height: 1.6;
            color: var(--text-color);
            background: var(--bg-color);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 1rem;
        }
        
        .maintenance-container {
            max-width: 500px;
            text-align: center;
            background: var(--container-bg-color);
            padding: 2rem;
            border-radius: 1rem;
            box-shadow: 0 4px 8px rgba(0, 0, 0, 0.1);
            transition: box-shadow 0.3s ease-in-out;
        }

        .maintenance-container:hover {
            box-shadow: 0 8px 16px rgba(0, 0, 0, 0.2);
        }
        
        .icon {
            font-size: 4rem;
            margin-bottom: 1rem;
            color: var(--primary-color);
        }
        
        h1 {
            font-size: 2rem;
            font-weight: 700;
            margin-bottom: 1rem;
            color: var(--text-color);
        }
        
        p {
            font-size: 1.125rem;
            color: var(--secondary-color);
            margin-bottom: 1.5rem;
        }

        .refresh-button {
            background-color: var(--primary-color);
            color: white;
            border: none;
            padding: 0.75rem 1.5rem;
            border-radius: 0.5rem;
            font-size: 1rem;
            cursor: pointer;
            transition: background-color 0.3s;
        }

        .refresh-button:hover {
            background-color: #1d4ed8;
        }
        
        @media (max-width: 640px) {
            .maintenance-container {
                padding: 1.5rem;
                margin: 1rem;
            }
            
            h1 {
                font-size: 1.5rem;
            }
            
            p {
                font-size: 1rem;
            }
            
            .icon {
                font-size: 3rem;
            }

            .refresh-button {
                padding: 0.5rem 1rem;
                font-size: 0.875rem;
            }
        }
    </style>
</head>
<body>
    <div class="maintenance-container">
        <div class="icon">🔧</div>
        <h1>We'll Be Back Soon!</h1>
        <p>We're currently upgrading our system to serve you better. <br>We appreciate your patience during this brief maintenance.</p>
        <p>Feel free to refresh the page in a few minutes.</p>
        <button class="refresh-button" onclick="location.reload()">Refresh Page</button>
    </div>
</body>
</html>`

const defaultRetryAfter = 300

// parseCaddyfile parses the maintenance directive in the Caddyfile
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m MaintenanceHandler

	for h.Next() {
		// Parse any arguments on the same line as the directive
		if h.NextArg() {
			m.HTMLTemplate = h.Val() // This will now be treated as a file path
		}

		// Parse any block
		for h.NextBlock(0) {
			switch h.Val() {
			case "template":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				m.HTMLTemplate = h.Val() // This will now be treated as a file path
			case "allowed_ips":
				// Parse multiple IPs until the end of the line
				for h.NextArg() {
					m.AllowedIPs = append(m.AllowedIPs, h.Val())
				}
			case "retry_after":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				val, err := strconv.Atoi(h.Val())
				if err != nil {
					return nil, h.Errf("invalid retry_after value: %v", err)
				}
				if val <= 0 {
					return nil, h.Errf("retry_after value must be positive")
				}
				m.RetryAfter = val
			case "default_enabled":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				val, err := strconv.ParseBool(h.Val())
				if err != nil {
					return nil, h.Errf("invalid default_enabled value: %v", err)
				}
				m.DefaultEnabled = val
			case "status_file":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				m.StatusFile = h.Val()
			case "request_retention_mode_timeout":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				val, err := strconv.Atoi(h.Val())
				if err != nil {
					return nil, h.Errf("invalid request_retention_mode_timeout value: %v", err)
				}
				if val <= 0 {
					return nil, h.Errf("request_retention_mode_timeout value must be positive")
				}
				m.RequestRetentionModeTimeout = val
			default:
				return nil, h.Errf("unknown subdirective '%s'", h.Val())
			}
		}
	}

	return &m, nil
}
