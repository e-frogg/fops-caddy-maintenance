package fopsMaintenance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"

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

	return serveHTML(w, h.HTMLTemplate)
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
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Site Maintenance</title>
    <meta name="robots" content="noindex">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body>
    <h1>Site Under Maintenance</h1>
    <p>We are currently performing scheduled maintenance. Please check back later.</p>
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
			default:
				return nil, h.Errf("unknown subdirective '%s'", h.Val())
			}
		}
	}

	return &m, nil
}
