package fopsMaintenance

import (
	"encoding/json"
	"fmt"
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

	// Maintenance mode state
	enabled    bool
	enabledMux sync.RWMutex

	// Durée de rétention des requêtes en mode rétention (en secondes)
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

	// Enregistrer l'instance
	setMaintenanceHandler(h)

	// Load template file if path is provided
	if h.HTMLTemplate != "" {
		content, err := os.ReadFile(h.HTMLTemplate)
		if err != nil {
			return fmt.Errorf("failed to read template file: %v", err)
		}
		h.HTMLTemplate = string(content)
	}

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
	for _, allowedIP := range h.AllowedIPs {
		if clientIP == allowedIP {
			return next.ServeHTTP(w, r)
		}
	}

	// Set Retry-After header with default value if not specified
	retryAfter := defaultRetryAfter
	if h.RetryAfter > 0 {
		retryAfter = h.RetryAfter
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))

	// Check if client accepts JSON
	if isJSONRequest(r) {
		return serveJSON(w)
	}

	if temporaryModeEnabled {
		// Mode temporaire activé, retenir la requête
		timer := time.NewTimer(time.Duration(h.RequestRetentionModeTimeout) * time.Second)
		for {
			select {
			case <-timer.C:
				h.enabledMux.RLock()
				enabled := h.enabled
				h.enabledMux.RUnlock()
				if !enabled {
					// Mode maintenance désactivé, transférer la requête
					return next.ServeHTTP(w, r)
				}

				// Timeout atteint, afficher la page de maintenance
				return serveHTML(w, h.HTMLTemplate)
			case <-h.ctx.Done():
				// Contexte annulé, vérifier l'état "enabled"
				h.enabledMux.RLock()
				enabled := h.enabled
				h.enabledMux.RUnlock()
				if !enabled {
					// Mode maintenance désactivé, transférer la requête
					return next.ServeHTTP(w, r)
				}
			case <-time.After(1000 * time.Millisecond):
				// Vérifier périodiquement l'état "enabled" toutes les 1000ms
				h.enabledMux.RLock()
				enabled := h.enabled
				h.enabledMux.RUnlock()
				if !enabled {
					// Mode maintenance désactivé, transférer la requête
					return next.ServeHTTP(w, r)
				}
			}
		}
	} else {
		// Mode maintenance complet, afficher la page de maintenance
		return serveHTML(w, h.HTMLTemplate)
	}
}

func isJSONRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return accept == "application/json" || r.Header.Get("Content-Type") == "application/json"
}

func serveJSON(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)

	response := map[string]string{
		"status":  "error",
		"message": "Service temporarily unavailable for maintenance",
	}
	return json.NewEncoder(w).Encode(response)
}

func serveHTML(w http.ResponseWriter, template string) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)

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
