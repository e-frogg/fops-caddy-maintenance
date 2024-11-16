package fopsMaintenance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(&MaintenanceHandler{})
	httpcaddyfile.RegisterHandlerDirective("maintenance", parseCaddyfile)
	httpcaddyfile.RegisterHandlerDirective("maintenance_api", parseMaintenanceAPI)
}

// MaintenanceHandler handles maintenance mode functionality
type MaintenanceHandler struct {
	// Custom HTML template for maintenance page
	HTMLTemplate string `json:"html_template,omitempty"`

	// Maintenance mode state
	enabled    bool
	enabledMux sync.RWMutex

	logger *zap.Logger
	ctx    caddy.Context
}

// CaddyModule returns the Caddy module information.
func (*MaintenanceHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.fops-maintenance",
		New: func() caddy.Module { return new(MaintenanceHandler) },
	}
}

// Provision implements caddy.Provisioner.
func (h *MaintenanceHandler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger()
	h.ctx = ctx

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

// Validate implements caddy.Validator.
func (h *MaintenanceHandler) Validate() error {
	return nil
}

// Interface guards
var (
	_ caddy.Provisioner           = (*MaintenanceHandler)(nil)
	_ caddy.Validator             = (*MaintenanceHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*MaintenanceHandler)(nil)
)

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *MaintenanceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {

	// Check if this is an API request
	if r.URL.Path == "/api/maintenance" {
		h.handleAPI(w, r)
		return nil
	}

	// Regular maintenance mode handling
	h.enabledMux.RLock()
	enabled := h.enabled
	h.enabledMux.RUnlock()

	if !enabled {
		return next.ServeHTTP(w, r)
	}

	w.Header().Set("Retry-After", "300")
	w.Header().Set("X-Robots-Tag", "noindex")

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

// API endpoints
func (h *MaintenanceHandler) handleAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getMaintenanceStatus(w, r)
	case http.MethodPost:
		h.toggleMaintenance(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *MaintenanceHandler) getMaintenanceStatus(w http.ResponseWriter, r *http.Request) {
	h.enabledMux.RLock()
	status := h.enabled
	h.enabledMux.RUnlock()

	h.logger.Debug("maintenance: status check",
		zap.Bool("enabled", status))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"maintenance_enabled": status})
}

func (h *MaintenanceHandler) toggleMaintenance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("maintenance: failed to decode request body",
			zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.enabledMux.Lock()
	h.enabled = req.Enabled
	h.enabledMux.Unlock()

	h.logger.Info("maintenance: mode changed",
		zap.Bool("enabled", req.Enabled))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"maintenance_enabled": req.Enabled})
}

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
			default:
				return nil, h.Errf("unknown subdirective '%s'", h.Val())
			}
		}
	}

	return &m, nil
}

// parseMaintenanceAPI parses the maintenance_api directive in the Caddyfile
func parseMaintenanceAPI(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m MaintenanceHandler

	// Consume the directive token
	if h.Next() {
		// No arguments are expected
		if h.NextArg() {
			return nil, h.ArgErr()
		}
	}

	return &m, nil
}
