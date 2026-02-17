package fopsMaintenance

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
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

	// File path containing allowed IPs with comments
	AllowedIPsFile string `json:"allowed_ips_file,omitempty"`

	// Enable support for forwarded headers (X-Forwarded-For, X-Real-IP)
	UseForwardedHeaders bool `json:"use_forwarded_headers,omitempty"`

	// List of trusted proxy IPs or CIDR ranges allowed to forward client IPs
	TrustedProxies []string `json:"trusted_proxies,omitempty"`

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

	// HTTP Basic Authentication configuration
	AuthRealm    string `json:"auth_realm,omitempty"`
	HtpasswdFile string `json:"htpasswd_file,omitempty"`

	// Paths that should bypass maintenance mode completely
	BypassPaths []string `json:"bypass_paths,omitempty"`

	// Pre-parsed IP access control for performance
	allowedIndividualIPs []net.IP
	allowedNetworks      []*net.IPNet

	// Pre-parsed trusted proxy IPs and networks for forwarded headers
	trustedProxyIPs      []net.IP
	trustedProxyNetworks []*net.IPNet

	// Pre-parsed htpasswd entries for performance
	htpasswdEntries map[string][]byte
	logger          *zap.Logger
	ctx             caddy.Context
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

	// Register the maintenance handler for admin API operations.
	registerMaintenanceHandler(h)

	// Pre-parse IP access control for performance
	if err := h.parseAllowedIPs(); err != nil {
		return fmt.Errorf("failed to parse allowed IPs: %v", err)
	}

	// Pre-parse trusted proxies for forwarded headers support
	if err := h.parseTrustedProxies(); err != nil {
		return fmt.Errorf("failed to parse trusted proxies: %v", err)
	}

	// Pre-parse htpasswd file for performance
	if err := h.parseHtpasswdFile(); err != nil {
		return fmt.Errorf("failed to parse htpasswd file: %v", err)
	}
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

// parseAllowedIPs pre-parses individual IPs and CIDR networks for performance
func (h *MaintenanceHandler) parseAllowedIPs() error {
	// Reset slices to prevent duplication on multiple calls
	h.allowedIndividualIPs = nil
	h.allowedNetworks = nil

	// Load IPs from file if specified
	if h.AllowedIPsFile != "" {
		fileIPs, err := h.loadIPsFromFile(h.AllowedIPsFile)
		if err != nil {
			return fmt.Errorf("failed to load IPs from file '%s': %v", h.AllowedIPsFile, err)
		}
		h.AllowedIPs = append(h.AllowedIPs, fileIPs...)
	}

	for _, allowedIP := range h.AllowedIPs {
		// Trim spaces to tolerate stray spaces in Caddyfiles
		allowedIP = strings.TrimSpace(allowedIP)

		// Check if it's a CIDR notation
		if strings.Contains(allowedIP, "/") {
			// Parse CIDR network
			_, ipNet, err := net.ParseCIDR(allowedIP)
			if err != nil {
				return fmt.Errorf("invalid CIDR notation '%s': %v", allowedIP, err)
			}
			h.allowedNetworks = append(h.allowedNetworks, ipNet)
		} else {
			// Parse individual IP
			ip := net.ParseIP(allowedIP)
			if ip == nil {
				return fmt.Errorf("invalid IP address '%s'", allowedIP)
			}
			h.allowedIndividualIPs = append(h.allowedIndividualIPs, ip)
		}
	}
	return nil
}

// parseTrustedProxies pre-parses trusted proxies into IPs and networks
func (h *MaintenanceHandler) parseTrustedProxies() error {
	// Reset slices to prevent duplication on multiple calls
	h.trustedProxyIPs = nil
	h.trustedProxyNetworks = nil

	if h.UseForwardedHeaders && len(h.TrustedProxies) == 0 {
		return fmt.Errorf("use_forwarded_headers requires at least one trusted proxy")
	}

	for _, proxy := range h.TrustedProxies {
		proxy = strings.TrimSpace(proxy)
		if proxy == "" {
			continue
		}

		if strings.Contains(proxy, "/") {
			_, ipNet, err := net.ParseCIDR(proxy)
			if err != nil {
				return fmt.Errorf("invalid trusted proxy CIDR '%s': %v", proxy, err)
			}
			h.trustedProxyNetworks = append(h.trustedProxyNetworks, ipNet)
			continue
		}

		ip := net.ParseIP(proxy)
		if ip == nil {
			return fmt.Errorf("invalid trusted proxy IP '%s'", proxy)
		}
		h.trustedProxyIPs = append(h.trustedProxyIPs, ip)
	}

	return nil
}

// loadIPsFromFile reads IPs from a file with comment support
func (h *MaintenanceHandler) loadIPsFromFile(filePath string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	var ips []string
	lines := strings.Split(string(content), "\n")

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Extract IP from line (remove inline comments)
		if commentIndex := strings.Index(line, "#"); commentIndex != -1 {
			line = strings.TrimSpace(line[:commentIndex])
		}

		// Skip empty lines after comment removal
		if line == "" {
			continue
		}

		// Validate IP format
		if strings.Contains(line, "/") {
			// CIDR notation
			_, _, err := net.ParseCIDR(line)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR notation '%s' at line %d: %v", line, lineNum+1, err)
			}
		} else {
			// Individual IP
			ip := net.ParseIP(line)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address '%s' at line %d", line, lineNum+1)
			}
		}

		ips = append(ips, line)
	}

	return ips, nil
}

// parseHtpasswdFile parses the htpasswd file and stores credentials in memory
func (h *MaintenanceHandler) parseHtpasswdFile() error {
	// Reset map to prevent duplication on multiple calls
	h.htpasswdEntries = make(map[string][]byte)

	if h.HtpasswdFile == "" {
		if h.logger != nil {
			h.logger.Debug("No htpasswd file configured")
		}
		return nil // No htpasswd file configured
	}

	if h.logger != nil {
		h.logger.Debug("Loading htpasswd file", zap.String("file", h.HtpasswdFile))
	}

	content, err := os.ReadFile(h.HtpasswdFile)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("Failed to read htpasswd file", zap.String("file", h.HtpasswdFile), zap.Error(err))
		}
		return fmt.Errorf("failed to read htpasswd file '%s': %v", h.HtpasswdFile, err)
	}

	lines := strings.Split(string(content), "\n")
	loadedUsers := 0

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Extract line content (remove inline comments)
		if commentIndex := strings.Index(line, "#"); commentIndex != -1 {
			line = strings.TrimSpace(line[:commentIndex])
		}

		// Skip empty lines after comment removal
		if line == "" {
			continue
		}

		// Parse htpasswd line (username:password_hash)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			if h.logger != nil {
				h.logger.Error("Invalid htpasswd format", zap.Int("line", lineNum+1), zap.String("line", line))
			}
			return fmt.Errorf("invalid htpasswd format at line %d: expected 'username:password_hash'", lineNum+1)
		}

		username := strings.TrimSpace(parts[0])
		passwordHash := strings.TrimSpace(parts[1])

		if username == "" {
			if h.logger != nil {
				h.logger.Error("Empty username in htpasswd", zap.Int("line", lineNum+1))
			}
			return fmt.Errorf("empty username at line %d", lineNum+1)
		}

		if passwordHash == "" {
			if h.logger != nil {
				h.logger.Error("Empty password hash in htpasswd", zap.Int("line", lineNum+1), zap.String("username", username))
			}
			return fmt.Errorf("empty password hash at line %d", lineNum+1)
		}

		// Store the password hash
		h.htpasswdEntries[username] = []byte(passwordHash)
		loadedUsers++

		if h.logger != nil {
			h.logger.Debug("Loaded user from htpasswd", zap.String("username", username))
		}
	}

	if h.logger != nil {
		h.logger.Info("Htpasswd file loaded successfully",
			zap.String("file", h.HtpasswdFile),
			zap.Int("users_loaded", loadedUsers),
		)
	}

	return nil
}

// isAuthenticated checks if the request has valid HTTP Basic Authentication
func (h *MaintenanceHandler) isAuthenticated(r *http.Request) bool {
	if h.HtpasswdFile == "" || len(h.htpasswdEntries) == 0 {
		if h.logger != nil {
			h.logger.Debug("No authentication configured")
		}
		return false // No authentication configured
	}

	// Get Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		if h.logger != nil {
			h.logger.Debug("No Authorization header present")
		}
		return false
	}

	// Check if it's Basic authentication
	if !strings.HasPrefix(authHeader, "Basic ") {
		if h.logger != nil {
			h.logger.Debug("Authorization header is not Basic", zap.String("auth_header", authHeader))
		}
		return false
	}

	// Extract and decode credentials
	encodedCredentials := strings.TrimPrefix(authHeader, "Basic ")
	decodedCredentials, err := base64.StdEncoding.DecodeString(encodedCredentials)
	if err != nil {
		if h.logger != nil {
			h.logger.Debug("Failed to decode base64 credentials", zap.Error(err))
		}
		return false
	}

	// Split username and password
	credentials := strings.SplitN(string(decodedCredentials), ":", 2)
	if len(credentials) != 2 {
		if h.logger != nil {
			h.logger.Debug("Invalid credentials format", zap.String("decoded", string(decodedCredentials)))
		}
		return false
	}

	username := credentials[0]
	password := credentials[1]

	if h.logger != nil {
		h.logger.Debug("Checking authentication",
			zap.String("username", username),
			zap.Bool("password_provided", password != ""),
		)
	}

	// Get stored password hash
	storedHash, exists := h.htpasswdEntries[username]
	if !exists {
		if h.logger != nil {
			h.logger.Debug("User not found in htpasswd", zap.String("username", username))
		}
		return false
	}

	// Verify password
	result := h.verifyPassword(password, storedHash)
	if h.logger != nil {
		h.logger.Debug("Password verification result",
			zap.String("username", username),
			zap.Bool("valid", result),
		)
	}
	return result
}

// verifyPassword verifies a password against a stored hash
func (h *MaintenanceHandler) verifyPassword(password string, storedHash []byte) bool {
	// Check if it's a bcrypt hash (starts with $2a$, $2b$, or $2y$)
	if len(storedHash) >= 4 && (storedHash[0] == '$' && storedHash[1] == '2') {
		// bcrypt hash
		err := bcrypt.CompareHashAndPassword(storedHash, []byte(password))
		return err == nil
	}

	// For other hash types (MD5, SHA1, etc.), we would need additional libraries
	// For now, we only support bcrypt which is the most secure option
	return false
}

// isIPAllowed checks if an IP address is allowed using pre-parsed IPs and networks
func (h *MaintenanceHandler) isIPAllowed(clientIP string) bool {
	// Parse client IP
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	// Check individual IPs first (faster for exact matches)
	for _, allowedIP := range h.allowedIndividualIPs {
		if ip.Equal(allowedIP) {
			return true
		}
	}

	// Check CIDR networks
	for _, network := range h.allowedNetworks {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// isTrustedProxy checks whether an IP belongs to the trusted proxy list
func (h *MaintenanceHandler) isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}

	for _, proxyIP := range h.trustedProxyIPs {
		if ip.Equal(proxyIP) {
			return true
		}
	}

	for _, network := range h.trustedProxyNetworks {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// isPathBypassed checks if a request path should bypass maintenance mode completely
func (h *MaintenanceHandler) isPathBypassed(path string) bool {
	if len(h.BypassPaths) == 0 {
		return false
	}

	// Normalize path for comparison
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}

	for _, bypassPath := range h.BypassPaths {
		bypassPath = strings.TrimSuffix(bypassPath, "/")
		if bypassPath == "" {
			bypassPath = "/"
		}

		// Exact match
		if path == bypassPath {
			return true
		}

		// Prefix match (for directories)
		if strings.HasSuffix(bypassPath, "/*") {
			prefix := strings.TrimSuffix(bypassPath, "/*")
			if prefix == "" {
				prefix = "/"
			}
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}

	return false
}

// Interface guards
var (
	_ caddy.Provisioner           = (*MaintenanceHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*MaintenanceHandler)(nil)
)

// getClientIP returns the effective client IP, optionally using forwarded headers
func (h *MaintenanceHandler) getClientIP(r *http.Request) string {
	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	if !h.UseForwardedHeaders {
		return clientIP
	}

	remoteIP := net.ParseIP(clientIP)
	if remoteIP == nil || !h.isTrustedProxy(remoteIP) {
		return clientIP
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			if candidate == "" {
				continue
			}

			ip := net.ParseIP(candidate)
			if ip == nil {
				continue
			}

			if h.isTrustedProxy(ip) {
				continue
			}

			return candidate
		}
	}

	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		if ip := net.ParseIP(xrip); ip != nil && !h.isTrustedProxy(ip) {
			return xrip
		}
	}

	return clientIP
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *MaintenanceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	h.enabledMux.RLock()
	enabled := h.enabled
	temporaryModeEnabled := h.RequestRetentionModeTimeout > 0
	h.enabledMux.RUnlock()

	if !enabled {
		return next.ServeHTTP(w, r)
	}

	// Check if path should bypass maintenance mode completely
	if h.isPathBypassed(r.URL.Path) {
		if h.logger != nil {
			h.logger.Debug("Path bypassed, forwarding request",
				zap.String("path", r.URL.Path),
				zap.Strings("bypass_paths", h.BypassPaths),
			)
		}
		return next.ServeHTTP(w, r)
	}

	// Check if client IP is in allowed list
	clientIP := h.getClientIP(r)

	// Debug logging
	if h.logger != nil {
		h.logger.Debug("Maintenance mode active",
			zap.String("client_ip", clientIP),
			zap.String("user_agent", r.UserAgent()),
			zap.String("path", r.URL.Path),
			zap.Bool("htpasswd_configured", h.HtpasswdFile != ""),
			zap.Int("htpasswd_entries_count", len(h.htpasswdEntries)),
		)
	}

	if h.isIPAllowed(clientIP) {
		if h.logger != nil {
			h.logger.Debug("IP allowed, bypassing maintenance", zap.String("client_ip", clientIP))
		}
		return next.ServeHTTP(w, r)
	}

	// Check if client is authenticated via HTTP Basic Auth
	authResult := h.isAuthenticated(r)
	if h.logger != nil {
		h.logger.Debug("Authentication check result",
			zap.Bool("authenticated", authResult),
			zap.String("auth_header", r.Header.Get("Authorization")),
		)
	}

	if authResult {
		return next.ServeHTTP(w, r)
	}

	// Request retention mode disabled, serve maintenance page now
	if !temporaryModeEnabled {
		if h.logger != nil {
			h.logger.Debug("Serving maintenance page", zap.String("client_ip", clientIP))
		}
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
				// Maintenance mode disabled, forward the request
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

	// Check if HTTP Basic Auth is configured
	if h.HtpasswdFile != "" && len(h.htpasswdEntries) > 0 {
		realm := "Maintenance Mode"
		if h.AuthRealm != "" {
			realm = h.AuthRealm
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
		// Return 401 to prompt for authentication
		w.WriteHeader(http.StatusUnauthorized)
		if h.logger != nil {
			h.logger.Debug("Returning 401 Unauthorized to prompt for authentication",
				zap.String("realm", realm),
				zap.String("htpasswd_file", h.HtpasswdFile),
				zap.Int("users_configured", len(h.htpasswdEntries)),
			)
		}
	} else {
		// No authentication configured, return 503 for maintenance
		w.WriteHeader(http.StatusServiceUnavailable)
		if h.logger != nil {
			h.logger.Debug("Returning 503 Service Unavailable (no authentication configured)")
		}
	}

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
			case "use_forwarded_headers":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				val, err := strconv.ParseBool(h.Val())
				if err != nil {
					return nil, h.Errf("invalid use_forwarded_headers value: %v", err)
				}

				m.UseForwardedHeaders = val
			case "trusted_proxies":
				for h.NextArg() {
					m.TrustedProxies = append(m.TrustedProxies, h.Val())
				}
			case "allowed_ips_file":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				m.AllowedIPsFile = h.Val()
			case "auth_realm":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				m.AuthRealm = h.Val()
			case "htpasswd_file":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				m.HtpasswdFile = h.Val()
			case "bypass_paths":
				// Parse multiple paths until the end of the line
				for h.NextArg() {
					m.BypassPaths = append(m.BypassPaths, h.Val())
				}
			default:
				return nil, h.Errf("unknown subdirective '%s'", h.Val())
			}
		}
	}

	return &m, nil
}
