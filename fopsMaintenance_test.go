package fopsMaintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceHandler(t *testing.T) {
	// Test cases
	tests := []struct {
		name           string
		maintenanceOn  bool
		acceptHeader   string
		retryAfter     int
		expectedStatus int
		expectedType   string
	}{
		{
			name:           "Maintenance Off",
			maintenanceOn:  false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Maintenance On - HTML Response",
			maintenanceOn:  true,
			acceptHeader:   "text/html",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "text/html; charset=utf-8",
		},
		{
			name:           "Maintenance On - JSON Response",
			maintenanceOn:  true,
			acceptHeader:   "application/json",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "application/json",
		},
		{
			name:           "Maintenance On - Custom Retry After",
			maintenanceOn:  true,
			acceptHeader:   "text/html",
			retryAfter:     600,
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "text/html; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create handler
			h := &MaintenanceHandler{
				HTMLTemplate: defaultHTMLTemplate,
				RetryAfter:   tt.retryAfter,
			}

			// Set maintenance mode
			h.enabledMux.Lock()
			h.enabled = tt.maintenanceOn
			h.enabledMux.Unlock()

			// Create test request
			req := httptest.NewRequest("GET", "http://example.com", nil)
			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Create next handler that always returns 200 OK
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			})

			// Execute handler
			err := h.ServeHTTP(w, req, next)
			if err != nil {
				t.Errorf("ServeHTTP returned unexpected error: %v", err)
			}

			// Check status code
			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d; got %d", tt.expectedStatus, w.Code)
			}

			// Check content type if specified
			if tt.expectedType != "" {
				contentType := w.Header().Get("Content-Type")
				if contentType != tt.expectedType {
					t.Errorf("expected Content-Type %s; got %s", tt.expectedType, contentType)
				}
			}

			// Add check for Retry-After header
			if tt.maintenanceOn {
				expectedRetryAfter := "300" // default value
				if tt.retryAfter > 0 {
					expectedRetryAfter = fmt.Sprintf("%d", tt.retryAfter)
				}
				assert.Equal(t, expectedRetryAfter, w.Header().Get("Retry-After"))
			}
		})
	}
}

func TestParseTrustedProxiesValidation(t *testing.T) {
	tests := []struct {
		name         string
		useForwarded bool
		trusted      []string
		expectError  bool
	}{
		{
			name:         "forwarded headers disabled without proxies",
			useForwarded: false,
			expectError:  false,
		},
		{
			name:         "forwarded headers enabled without proxies",
			useForwarded: true,
			expectError:  true,
		},
		{
			name:         "invalid proxy entry",
			useForwarded: true,
			trusted:      []string{"not-an-ip"},
			expectError:  true,
		},
		{
			name:         "valid proxies parsed",
			useForwarded: true,
			trusted:      []string{"192.0.2.10", "198.51.100.0/24"},
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := MaintenanceHandler{
				UseForwardedHeaders: tt.useForwarded,
				TrustedProxies:      append([]string(nil), tt.trusted...),
			}

			err := handler.parseTrustedProxies()
			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if handler.UseForwardedHeaders {
				require.Len(t, handler.trustedProxyIPs, 1)
				require.Len(t, handler.trustedProxyNetworks, 1)
				require.True(t, handler.trustedProxyIPs[0].Equal(net.ParseIP("192.0.2.10")))
			}
		})
	}
}

func TestMaintenanceHandler_getClientIP(t *testing.T) {
	tests := []struct {
		name           string
		useForwarded   bool
		trusted        []string
		remoteAddr     string
		headers        map[string]string
		expectedClient string
	}{
		{
			name:           "forwarded headers disabled returns remote host",
			useForwarded:   false,
			remoteAddr:     "203.0.113.5:12345",
			expectedClient: "203.0.113.5",
		},
		{
			name:         "remote not trusted ignores forwarded headers",
			useForwarded: true,
			trusted:      []string{"192.0.2.1"},
			remoteAddr:   "198.51.100.2:12345",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.5",
				"X-Real-IP":       "203.0.113.6",
			},
			expectedClient: "198.51.100.2",
		},
		{
			name:         "extracts first non-trusted from X-Forwarded-For",
			useForwarded: true,
			trusted:      []string{"192.0.2.1", "198.51.100.3"},
			remoteAddr:   "192.0.2.1:443",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.5, 198.51.100.3",
				"X-Real-IP":       "198.51.100.4",
			},
			expectedClient: "203.0.113.5",
		},
		{
			name:         "falls back to X-Real-IP when XFF only has proxies",
			useForwarded: true,
			trusted:      []string{"192.0.2.1", "198.51.100.3"},
			remoteAddr:   "192.0.2.1:443",
			headers: map[string]string{
				"X-Forwarded-For": "192.0.2.1, 198.51.100.3",
				"X-Real-IP":       "203.0.113.7",
			},
			expectedClient: "203.0.113.7",
		},
		{
			name:         "falls back to remote when headers invalid",
			useForwarded: true,
			trusted:      []string{"192.0.2.1"},
			remoteAddr:   "192.0.2.1:443",
			headers: map[string]string{
				"X-Forwarded-For": "not-an-ip, 192.0.2.1",
				"X-Real-IP":       "also-invalid",
			},
			expectedClient: "192.0.2.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MaintenanceHandler{
				UseForwardedHeaders: tt.useForwarded,
				TrustedProxies:      tt.trusted,
			}

			err := h.parseTrustedProxies()
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			req.RemoteAddr = tt.remoteAddr
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			clientIP := h.getClientIP(req)
			assert.Equal(t, tt.expectedClient, clientIP)
		})
	}
}

func TestProvision(t *testing.T) {
	h := &MaintenanceHandler{
		HTMLTemplate: "benchmark/maintenance.html",
	}
	ctx := caddy.Context{}

	err := h.Provision(ctx)
	if err != nil {
		t.Errorf("Provision failed: %v", err)
	}
}
func TestMaintenanceHandlerDifferentMethods(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		maintenanceOn  bool
		acceptHeader   string
		expectedStatus int
		expectedType   string
	}{
		{
			name:           "GET Request - Maintenance On",
			method:         "GET",
			maintenanceOn:  true,
			acceptHeader:   "text/html",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "text/html; charset=utf-8",
		},
		{
			name:           "POST Request - Maintenance On",
			method:         "POST",
			maintenanceOn:  true,
			acceptHeader:   "text/html",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "text/html; charset=utf-8",
		},
		{
			name:           "POST Request - Maintenance On",
			method:         "POST",
			maintenanceOn:  true,
			acceptHeader:   "application/json",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "application/json",
		},
		{
			name:           "PUT Request - Maintenance On",
			method:         "PUT",
			maintenanceOn:  true,
			acceptHeader:   "application/json",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "application/json",
		},
		{
			name:           "DELETE Request - Maintenance On",
			method:         "DELETE",
			maintenanceOn:  true,
			acceptHeader:   "application/json",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "application/json",
		},
		{
			name:           "PATCH Request - Maintenance On",
			method:         "PATCH",
			maintenanceOn:  true,
			acceptHeader:   "application/json",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "application/json",
		},
		{
			name:           "OPTIONS Request - Maintenance On",
			method:         "OPTIONS",
			maintenanceOn:  true,
			acceptHeader:   "application/json",
			expectedStatus: http.StatusServiceUnavailable,
			expectedType:   "application/json",
		},
		{
			name:           "GET Request - Maintenance Off",
			method:         "GET",
			maintenanceOn:  false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST Request - Maintenance Off",
			method:         "POST",
			maintenanceOn:  false,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create handler
			h := &MaintenanceHandler{
				HTMLTemplate: defaultHTMLTemplate,
			}

			// Set maintenance mode
			h.enabledMux.Lock()
			h.enabled = tt.maintenanceOn
			h.enabledMux.Unlock()

			// Create test request
			req := httptest.NewRequest(tt.method, "http://example.com", nil)
			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Create next handler that always returns 200 OK
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			})

			// Execute handler
			err := h.ServeHTTP(w, req, next)
			if err != nil {
				t.Errorf("ServeHTTP returned unexpected error: %v", err)
			}

			// Check status code
			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d for method %s; got %d",
					tt.expectedStatus, tt.method, w.Code)
			}

			// Check content type if specified
			if tt.expectedType != "" {
				contentType := w.Header().Get("Content-Type")
				if contentType != tt.expectedType {
					t.Errorf("expected Content-Type %s for method %s; got %s",
						tt.expectedType, tt.method, contentType)
				}
			}

			// Additional check for maintenance mode off
			if !tt.maintenanceOn {
				// Verify that the next handler was called (status should be OK)
				if w.Code != http.StatusOK {
					t.Errorf("expected next handler to be called with status %d for method %s; got %d",
						http.StatusOK, tt.method, w.Code)
				}
			}
		})
	}
}

func TestMaintenanceHandlerTemplate(t *testing.T) {
	// Create test files
	validHTML := `<!DOCTYPE html>
	<html>
		<body>
			<h1>Maintenance Mode</h1>
			<p>The site is currently under maintenance.</p>
		</body>
	</html>`

	// Create test directories and files
	testDir := "testdata"
	err := os.MkdirAll(testDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(testDir); err != nil {
			t.Errorf("Failed to clean up test directory: %v", err)
		}
	}()

	// Write test file
	err = os.WriteFile(filepath.Join(testDir, "valid.html"), []byte(validHTML), 0644)
	if err != nil {
		t.Fatalf("Failed to write valid HTML: %v", err)
	}

	tests := []struct {
		name          string
		templatePath  string
		expectedError bool
	}{
		{
			name:          "Valid HTML File",
			templatePath:  filepath.Join(testDir, "valid.html"),
			expectedError: false,
		},
		{
			name:          "Non-existent File",
			templatePath:  "nonexistent.html",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MaintenanceHandler{
				HTMLTemplate: tt.templatePath,
			}

			// Test Provision
			err := h.Provision(caddy.Context{})

			// Check if error matches expectation
			if tt.expectedError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// For valid file, test content
			if !tt.expectedError && err == nil {
				req := httptest.NewRequest("GET", "http://example.com", nil)
				req.Header.Set("Accept", "text/html")
				w := httptest.NewRecorder()

				h.enabledMux.Lock()
				h.enabled = true
				h.enabledMux.Unlock()

				next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
					return nil
				})

				err = h.ServeHTTP(w, req, next)
				if err != nil {
					t.Errorf("ServeHTTP returned unexpected error: %v", err)
				}

				if !strings.Contains(w.Body.String(), "Maintenance Mode") {
					t.Error("response does not contain expected content")
				}
			}
		})
	}
}

func TestMaintenanceHandler_ServeHTTP_AllowedIPs(t *testing.T) {
	tests := []struct {
		name          string
		allowedIPs    []string
		clientIP      string
		expectBlocked bool
		expectError   bool
	}{
		{
			name:          "Allowed IP should bypass maintenance",
			allowedIPs:    []string{"192.168.1.100", "10.0.0.1"},
			clientIP:      "192.168.1.100",
			expectBlocked: false,
		},
		{
			name:          "Non-allowed IP should see maintenance page",
			allowedIPs:    []string{"192.168.1.100", "10.0.0.1"},
			clientIP:      "192.168.1.101",
			expectBlocked: true,
		},
		{
			name:          "Empty allowed IPs should block all",
			allowedIPs:    []string{},
			clientIP:      "192.168.1.100",
			expectBlocked: true,
		},
		{
			name:          "Allowed IP with port should bypass maintenance",
			allowedIPs:    []string{"192.168.1.100"},
			clientIP:      "192.168.1.100:12345",
			expectBlocked: false,
		},
		{
			name:          "Non-allowed IP with port should see maintenance page",
			allowedIPs:    []string{"192.168.1.100"},
			clientIP:      "192.168.1.101:12345",
			expectBlocked: true,
		},
		{
			name:          "Local IP should bypass maintenance",
			allowedIPs:    []string{"192.168.1.50"},
			clientIP:      "192.168.1.50:54321",
			expectBlocked: false,
		},
		{
			name:          "CIDR notation - IP in range should bypass maintenance",
			allowedIPs:    []string{"192.168.5.0/22"},
			clientIP:      "192.168.5.5",
			expectBlocked: false,
		},
		{
			name:          "CIDR notation - IP outside range should see maintenance page",
			allowedIPs:    []string{"192.168.5.0/22"},
			clientIP:      "192.168.8.5",
			expectBlocked: true,
		},
		{
			name:          "CIDR notation - IP in range with port should bypass maintenance",
			allowedIPs:    []string{"192.168.5.0/22"},
			clientIP:      "192.168.5.5:12345",
			expectBlocked: false,
		},
		{
			name:          "Multiple CIDR ranges - IP in first range",
			allowedIPs:    []string{"192.168.5.0/22", "10.0.1.0/24"},
			clientIP:      "192.168.5.10",
			expectBlocked: false,
		},
		{
			name:          "Multiple CIDR ranges - IP in second range",
			allowedIPs:    []string{"192.168.5.0/22", "10.0.1.0/24"},
			clientIP:      "10.0.1.50",
			expectBlocked: false,
		},
		{
			name:          "Multiple CIDR ranges - IP outside all ranges",
			allowedIPs:    []string{"192.168.5.0/22", "10.0.1.0/24"},
			clientIP:      "192.168.1.100",
			expectBlocked: true,
		},
		{
			name:          "Mixed individual IPs and CIDR ranges",
			allowedIPs:    []string{"192.168.1.100", "192.168.5.0/22", "10.0.1.0/24"},
			clientIP:      "192.168.1.100",
			expectBlocked: false,
		},
		{
			name:          "Mixed individual IPs and CIDR ranges - IP in CIDR range",
			allowedIPs:    []string{"192.168.1.100", "192.168.5.0/22", "10.0.1.0/24"},
			clientIP:      "192.168.5.15",
			expectBlocked: false,
		},
		{
			name:          "IPv6 individual IP should bypass maintenance",
			allowedIPs:    []string{"2001:db8::1"},
			clientIP:      "2001:db8::1",
			expectBlocked: false,
		},
		{
			name:          "IPv6 CIDR notation - IP in range should bypass maintenance",
			allowedIPs:    []string{"2001:db8::/32"},
			clientIP:      "2001:db8::1234",
			expectBlocked: false,
		},
		{
			name:          "IPv6 CIDR notation - IP outside range should see maintenance page",
			allowedIPs:    []string{"2001:db8::/32"},
			clientIP:      "2001:db9::1234",
			expectBlocked: true,
		},
		{
			name:          "Mixed IPv4 and IPv6",
			allowedIPs:    []string{"192.168.1.100", "2001:db8::/32"},
			clientIP:      "2001:db8::5678",
			expectBlocked: false,
		},
		{
			name:        "Invalid CIDR notation should cause configuration error",
			allowedIPs:  []string{"192.168.5.0/22", "invalid-cidr"},
			clientIP:    "192.168.5.5",
			expectError: true,
		},
		{
			name:        "Invalid IP address should cause configuration error",
			allowedIPs:  []string{"192.168.1.100", "invalid-ip"},
			clientIP:    "192.168.1.100",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create handler with maintenance enabled
			h := &MaintenanceHandler{
				AllowedIPs: tt.allowedIPs,
			}
			// Test Provision (this will parse the IPs)
			ctx := caddy.Context{}
			err := h.Provision(ctx)

			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Set maintenance mode

			h.enabledMux.Lock()
			h.enabled = true
			h.enabledMux.Unlock()

			// Create test request with specified client IP
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.clientIP

			// Create response recorder
			w := httptest.NewRecorder()

			// Create a mock next handler that sets a specific header
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				w.Header().Set("X-Test", "passed")
				return nil
			})

			// Serve the request
			err = h.ServeHTTP(w, req, next)
			require.NoError(t, err)

			if tt.expectBlocked {
				// Verify maintenance response
				assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			} else {
				// Verify the request was passed to next handler
				assert.Equal(t, http.StatusOK, w.Code)
				assert.Equal(t, "passed", w.Header().Get("X-Test"))
			}
		})
	}
}

// TestParseAllowedIPs tests the IP parsing functionality directly
func TestParseAllowedIPs(t *testing.T) {
	tests := []struct {
		name        string
		allowedIPs  []string
		expectError bool
		errorMsg    string
	}{
		{
			name:       "Valid individual IPv4 IPs",
			allowedIPs: []string{"192.168.1.100", "10.0.0.1"},
		},
		{
			name:       "Valid CIDR IPv4 networks",
			allowedIPs: []string{"192.168.5.0/22", "10.0.1.0/24"},
		},
		{
			name:       "Valid individual IPv6 IPs",
			allowedIPs: []string{"2001:db8::1", "::1"},
		},
		{
			name:       "Valid CIDR IPv6 networks",
			allowedIPs: []string{"2001:db8::/32", "::/128"},
		},
		{
			name:       "Mixed IPv4 and IPv6",
			allowedIPs: []string{"192.168.1.100", "2001:db8::/32", "10.0.1.0/24"},
		},
		{
			name:        "Invalid CIDR notation",
			allowedIPs:  []string{"192.168.5.0/22", "invalid-cidr"},
			expectError: true,
			errorMsg:    "invalid IP address",
		},
		{
			name:        "Invalid IP address",
			allowedIPs:  []string{"192.168.1.100", "invalid-ip"},
			expectError: true,
			errorMsg:    "invalid IP address",
		},
		{
			name:        "Invalid CIDR format",
			allowedIPs:  []string{"192.168.1.100/33"}, // Invalid for IPv4
			expectError: true,
			errorMsg:    "invalid CIDR notation",
		},
		{
			name:       "IPs with leading/trailing spaces",
			allowedIPs: []string{" 192.168.1.100 ", " 10.0.0.1", "2001:db8::1 "},
		},
		{
			name:       "CIDR with leading/trailing spaces",
			allowedIPs: []string{" 192.168.5.0/22 ", " 10.0.1.0/24", "2001:db8::/32 "},
		},
		{
			name:       "Multiple calls should reset slices",
			allowedIPs: []string{"192.168.1.100", "192.168.5.0/22"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MaintenanceHandler{
				AllowedIPs: tt.allowedIPs,
			}

			err := h.parseAllowedIPs()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestParseAllowedIPsMultipleCalls tests that slices are properly reset on multiple calls
func TestParseAllowedIPsMultipleCalls(t *testing.T) {
	h := &MaintenanceHandler{
		AllowedIPs: []string{"192.168.1.100", "192.168.5.0/22"},
	}

	// First call
	err := h.parseAllowedIPs()
	require.NoError(t, err)

	// Verify first call populated slices correctly
	assert.Equal(t, 1, len(h.allowedIndividualIPs), "Should have 1 individual IP")
	assert.Equal(t, 1, len(h.allowedNetworks), "Should have 1 network")

	// Second call with different IPs
	h.AllowedIPs = []string{"10.0.0.1", "10.0.1.0/24"}
	err = h.parseAllowedIPs()
	require.NoError(t, err)

	// Verify that slices were reset and contain new values
	assert.Equal(t, 1, len(h.allowedIndividualIPs), "Should have 1 individual IP after reset")
	assert.Equal(t, 1, len(h.allowedNetworks), "Should have 1 network after reset")

	// Verify the content is from the second call, not accumulated
	assert.Equal(t, "10.0.0.1", h.allowedIndividualIPs[0].String(), "Should contain IP from second call")
}

// TestIsIPAllowedDirect tests the IP checking functionality directly
func TestIsIPAllowedDirect(t *testing.T) {
	h := &MaintenanceHandler{
		AllowedIPs: []string{"192.168.1.100", "192.168.5.0/22", "2001:db8::/32"},
	}

	err := h.parseAllowedIPs()
	require.NoError(t, err)

	tests := []struct {
		name     string
		clientIP string
		expected bool
	}{
		{
			name:     "Exact IPv4 match",
			clientIP: "192.168.1.100",
			expected: true,
		},
		{
			name:     "IPv4 in CIDR range",
			clientIP: "192.168.5.5",
			expected: true,
		},
		{
			name:     "IPv6 in CIDR range",
			clientIP: "2001:db8::1234",
			expected: true,
		},
		{
			name:     "IPv4 outside range",
			clientIP: "192.168.8.5",
			expected: false,
		},
		{
			name:     "IPv6 outside range",
			clientIP: "2001:db9::1234",
			expected: false,
		},
		{
			name:     "Invalid IP",
			clientIP: "invalid-ip",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := h.isIPAllowed(tt.clientIP)
			assert.Equal(t, tt.expected, result,
				"Expected %v for clientIP %s", tt.expected, tt.clientIP)
		})
	}
}

func TestParseCaddyfile(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedM     *MaintenanceHandler
		expectErr     bool
		expectedErrIs string
	}{
		{
			name:      "Basic directive without args",
			input:     "maintenance",
			expectedM: &MaintenanceHandler{},
		},
		{
			name:  "Template path as argument",
			input: "maintenance /path/to/template.html",
			expectedM: &MaintenanceHandler{
				HTMLTemplate: "/path/to/template.html",
			},
		},
		{
			name: "Template in block",
			input: `maintenance {
				template /path/to/template.html
			}`,
			expectedM: &MaintenanceHandler{
				HTMLTemplate: "/path/to/template.html",
			},
		},
		{
			name: "Allowed IPs in block",
			input: `maintenance {
				allowed_ips 192.168.1.100 10.0.0.1
			}`,
			expectedM: &MaintenanceHandler{
				AllowedIPs: []string{"192.168.1.100", "10.0.0.1"},
			},
		},
		{
			name: "Complete configuration",
			input: `maintenance {
				template /path/to/template.html
				allowed_ips 192.168.1.100 10.0.0.1
			}`,
			expectedM: &MaintenanceHandler{
				HTMLTemplate: "/path/to/template.html",
				AllowedIPs:   []string{"192.168.1.100", "10.0.0.1"},
			},
		},
		{
			name: "Invalid subdirective",
			input: `maintenance {
				invalid_directive value
			}`,
			expectErr:     true,
			expectedErrIs: "unknown subdirective 'invalid_directive'",
		},
		{
			name: "Template without value",
			input: `maintenance {
				template
			}`,
			expectErr: true,
		},
		{
			name: "With retry_after configuration",
			input: `maintenance {
				retry_after 600
			}`,
			expectedM: &MaintenanceHandler{
				RetryAfter: 600,
			},
		},
		{
			name: "Complete configuration with retry_after",
			input: `maintenance {
				template /path/to/template.html
				allowed_ips 192.168.1.100 10.0.0.1
				retry_after 600
			}`,
			expectedM: &MaintenanceHandler{
				HTMLTemplate: "/path/to/template.html",
				AllowedIPs:   []string{"192.168.1.100", "10.0.0.1"},
				RetryAfter:   600,
			},
		},
		{
			name: "Invalid retry_after value",
			input: `maintenance {
				retry_after invalid
			}`,
			expectErr:     true,
			expectedErrIs: "invalid retry_after value",
		},
		{
			name: "Negative retry_after value",
			input: `maintenance {
				retry_after -1
			}`,
			expectErr:     true,
			expectedErrIs: "retry_after value must be positive",
		},
		{
			name: "retry_after without value",
			input: `maintenance {
				retry_after
			}`,
			expectErr:     true,
			expectedErrIs: "wrong argument count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new dispenser with the test input
			d := caddyfile.NewTestDispenser(tt.input)

			// Parse the Caddyfile
			h := httpcaddyfile.Helper{Dispenser: d}
			actual, err := parseCaddyfile(h)

			// Check error expectations
			if tt.expectErr {
				require.Error(t, err)
				if tt.expectedErrIs != "" {
					assert.Contains(t, err.Error(), tt.expectedErrIs)
				}
				return
			}
			require.NoError(t, err)

			// Type assert and compare the results
			actualHandler, ok := actual.(*MaintenanceHandler)
			require.True(t, ok)

			// Compare fields
			assert.Equal(t, tt.expectedM.HTMLTemplate, actualHandler.HTMLTemplate)
			assert.Equal(t, tt.expectedM.AllowedIPs, actualHandler.AllowedIPs)
			assert.Equal(t, tt.expectedM.RetryAfter, actualHandler.RetryAfter)
		})
	}
}

func TestMaintenanceHandlerRequestRetentionMode(t *testing.T) {
	// Test cases
	tests := []struct {
		name                        string
		maintenanceOn               bool
		requestRetentionModeTimeout int
		expectedStatus              int
		expectedType                string
	}{
		{
			name:                        "Request Retention Mode - Maintenance On",
			maintenanceOn:               true,
			requestRetentionModeTimeout: 2,
			expectedStatus:              http.StatusServiceUnavailable,
			expectedType:                "text/html; charset=utf-8",
		},
		{
			name:                        "Request Retention Mode - Maintenance Off",
			maintenanceOn:               false,
			requestRetentionModeTimeout: 2,
			expectedStatus:              http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := caddy.NewContext(caddy.Context{Context: context.Background()})
			defer cancel()

			h := &MaintenanceHandler{
				HTMLTemplate:                defaultHTMLTemplate,
				RequestRetentionModeTimeout: tt.requestRetentionModeTimeout,
				ctx:                         ctx,
			}

			// Set maintenance mode
			h.enabledMux.Lock()
			h.enabled = tt.maintenanceOn
			h.enabledMux.Unlock()

			// Create test request
			req := httptest.NewRequest("GET", "http://example.com", nil)

			// Create response recorder
			w := httptest.NewRecorder()

			// Create next handler that always returns 200 OK
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			})

			// Execute handler
			err := h.ServeHTTP(w, req, next)
			if err != nil {
				t.Errorf("ServeHTTP returned unexpected error: %v", err)
			}

			// Check status code
			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d; got %d", tt.expectedStatus, w.Code)
			}

			// Check content type if specified
			if tt.expectedType != "" {
				contentType := w.Header().Get("Content-Type")
				if contentType != tt.expectedType {
					t.Errorf("expected Content-Type %s; got %s", tt.expectedType, contentType)
				}
			}
		})
	}
}

func TestParseCaddyfileRequestRetentionModeTimeout(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedError bool
		expected      *MaintenanceHandler
	}{
		{
			name: "Valid request_retention_mode_timeout",
			input: `maintenance {
					request_retention_mode_timeout 30
				}`,
			expectedError: false,
			expected: &MaintenanceHandler{
				RequestRetentionModeTimeout: 30,
			},
		},
		{
			name: "Missing request_retention_mode_timeout value",
			input: `maintenance {
					request_retention_mode_timeout
				}`,
			expectedError: true,
			expected:      nil,
		},
		{
			name: "Invalid request_retention_mode_timeout value",
			input: `maintenance {
					request_retention_mode_timeout invalid
				}`,
			expectedError: true,
			expected:      nil,
		},
		{
			name: "Negative request_retention_mode_timeout value",
			input: `maintenance {
					request_retention_mode_timeout -10
				}`,
			expectedError: true,
			expected:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := caddyfile.NewTestDispenser(tt.input)
			h := httpcaddyfile.Helper{Dispenser: d}

			actual, err := parseCaddyfile(h)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, actual)
			} else {
				assert.NoError(t, err)
				actualHandler, ok := actual.(*MaintenanceHandler)
				assert.True(t, ok)
				assert.Equal(t, tt.expected.RequestRetentionModeTimeout, actualHandler.RequestRetentionModeTimeout)
			}
		})
	}
}

func TestMaintenanceHandlerRequestRetentionModeWithDisable(t *testing.T) {
	// Create context with timeout
	ctx, cancel := caddy.NewContext(caddy.Context{Context: context.Background()})
	defer cancel()

	// Create handler with retention mode of 30 seconds
	h := &MaintenanceHandler{
		HTMLTemplate:                defaultHTMLTemplate,
		RequestRetentionModeTimeout: 30,
		ctx:                         ctx,
	}

	// Enable maintenance mode initially
	h.enabledMux.Lock()
	h.enabled = true
	h.enabledMux.Unlock()

	// Create test request
	req := httptest.NewRequest("GET", "http://example.com", nil)

	// Create response recorder
	w := httptest.NewRecorder()

	// Create next handler that sets a specific header to verify it was called
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("X-Test", "request-processed")
		w.WriteHeader(http.StatusOK)
		return nil
	})

	// Launch request processing in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- h.ServeHTTP(w, req, next)
	}()

	// Wait a short time to ensure request is being held
	time.Sleep(2 * time.Second)

	// Disable maintenance mode
	h.enabledMux.Lock()
	h.enabled = false
	h.enabledMux.Unlock()

	// Wait for the request to complete
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Request did not complete in time")
	}

	// Verify that the request was processed by the next handler
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "request-processed", w.Header().Get("X-Test"))
}

func TestMaintenanceHandlerRequestRetentionModeWithPeriodicCheck(t *testing.T) {
	// Create context with timeout
	ctx, cancel := caddy.NewContext(caddy.Context{Context: context.Background()})
	defer cancel()

	// Create handler with retention mode of 30 seconds
	h := &MaintenanceHandler{
		HTMLTemplate:                defaultHTMLTemplate,
		RequestRetentionModeTimeout: 30,
		ctx:                         ctx,
	}

	// Enable maintenance mode initially
	h.enabledMux.Lock()
	h.enabled = true
	h.enabledMux.Unlock()

	// Create test request
	req := httptest.NewRequest("GET", "http://example.com", nil)

	// Create response recorder
	w := httptest.NewRecorder()

	// Create next handler that sets a specific header to verify it was called
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("X-Test", "request-processed")
		w.WriteHeader(http.StatusOK)
		return nil
	})

	// Launch request processing in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- h.ServeHTTP(w, req, next)
	}()

	// Wait slightly longer than 1 second to ensure we hit the periodic check
	time.Sleep(1100 * time.Millisecond)

	// Disable maintenance mode
	h.enabledMux.Lock()
	h.enabled = false
	h.enabledMux.Unlock()

	// Wait for the request to complete
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Request did not complete in time")
	}

	// Verify that the request was processed by the next handler
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "request-processed", w.Header().Get("X-Test"))
}

func TestMaintenanceHandler_DefaultEnabled(t *testing.T) {
	tests := []struct {
		name           string
		defaultEnabled bool
		expectedState  bool
	}{
		{
			name:           "Default Enabled True",
			defaultEnabled: true,
			expectedState:  true,
		},
		{
			name:           "Default Enabled False",
			defaultEnabled: false,
			expectedState:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MaintenanceHandler{
				DefaultEnabled: tt.defaultEnabled,
			}

			ctx := caddy.Context{}
			err := h.Provision(ctx)
			require.NoError(t, err)

			h.enabledMux.RLock()
			state := h.enabled
			h.enabledMux.RUnlock()

			assert.Equal(t, tt.expectedState, state, "Maintenance state should match DefaultEnabled")
		})
	}
}

func TestMaintenanceHandler_StatusFile(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name           string
		setupFile      bool
		fileContent    string
		defaultEnabled bool
		expectedState  bool
	}{
		{
			name:           "Load Enabled State from File",
			setupFile:      true,
			fileContent:    `{"enabled": true}`,
			defaultEnabled: false,
			expectedState:  true,
		},
		{
			name:           "Load Disabled State from File",
			setupFile:      true,
			fileContent:    `{"enabled": false}`,
			defaultEnabled: true,
			expectedState:  false,
		},
		{
			name:           "Invalid JSON in File",
			setupFile:      true,
			fileContent:    `invalid json`,
			defaultEnabled: true,
			expectedState:  true,
		},
		{
			name:           "No File Present",
			setupFile:      false,
			defaultEnabled: true,
			expectedState:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a unique file for each test case
			statusFile := filepath.Join(tmpDir, fmt.Sprintf("status_%s.json", strings.ReplaceAll(tt.name, " ", "_")))

			// Setup status file if needed
			if tt.setupFile {
				err := os.WriteFile(statusFile, []byte(tt.fileContent), 0644)
				require.NoError(t, err, "Failed to write status file")
			}

			// Create and provision handler
			h := &MaintenanceHandler{
				DefaultEnabled: tt.defaultEnabled,
				StatusFile:     statusFile,
			}

			ctx := caddy.Context{}
			err := h.Provision(ctx)
			require.NoError(t, err, "Failed to provision handler")

			// Check state
			h.enabledMux.RLock()
			state := h.enabled
			h.enabledMux.RUnlock()

			assert.Equal(t, tt.expectedState, state, "Maintenance state does not match expected state")
		})
	}
}

// Create a custom marshaller for testing
type jsonMarshaller interface {
	Marshal(v interface{}) ([]byte, error)
}

type defaultJSONMarshaller struct{}

func (m *defaultJSONMarshaller) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

type errorJSONMarshaller struct{}

func (m *errorJSONMarshaller) Marshal(v interface{}) ([]byte, error) {
	return nil, fmt.Errorf("simulated marshal error")
}

// Mock version of AdminHandler for testing
type mockAdminHandler struct {
	AdminHandler
	marshaller jsonMarshaller
}

func (h *mockAdminHandler) toggle(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return caddy.APIError{
			HTTPStatus: http.StatusMethodNotAllowed,
			Err:        fmt.Errorf("method not allowed"),
		}
	}

	var req struct {
		Enabled                     bool `json:"enabled"`
		RequestRetentionModeTimeout int  `json:"request_retention_mode_timeout,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        err,
		}
	}

	maintenanceHandler := getMaintenanceHandler()
	if maintenanceHandler == nil {
		return caddy.APIError{
			HTTPStatus: http.StatusNotFound,
			Err:        fmt.Errorf("maintenance handler not found"),
		}
	}

	maintenanceHandler.enabledMux.Lock()
	maintenanceHandler.enabled = req.Enabled
	maintenanceHandler.RequestRetentionModeTimeout = req.RequestRetentionModeTimeout
	maintenanceHandler.enabledMux.Unlock()

	// Persist status if StatusFile is configured
	if maintenanceHandler.StatusFile != "" {
		status := struct {
			Enabled bool `json:"enabled"`
		}{
			Enabled: req.Enabled,
		}

		// Use the custom marshaller
		data, err := h.marshaller.Marshal(status)
		if err != nil {
			return caddy.APIError{
				HTTPStatus: http.StatusInternalServerError,
				Err:        fmt.Errorf("failed to marshal status: %v", err),
			}
		}

		if err := os.WriteFile(maintenanceHandler.StatusFile, data, 0644); err != nil {
			return caddy.APIError{
				HTTPStatus: http.StatusInternalServerError,
				Err:        fmt.Errorf("failed to persist status: %v", err),
			}
		}
	}

	return json.NewEncoder(w).Encode(map[string]bool{
		"enabled": req.Enabled,
	})
}

func TestMaintenanceHandler_StatusPersistenceErrors(t *testing.T) {
	tests := []struct {
		name            string
		setupHandler    func() *MaintenanceHandler
		expectError     bool
		errorContains   string
		useErrorMarshal bool
	}{
		{
			name: "Marshal Error",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					StatusFile: "/tmp/test_marshal_error.json",
				}
				setMaintenanceHandler(h)
				return h
			},
			expectError:     true,
			errorContains:   "failed to marshal status",
			useErrorMarshal: true,
		},
		{
			name: "Write Error - Directory Not Exists",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					StatusFile: "/nonexistent/directory/maintenance.json",
				}
				setMaintenanceHandler(h)
				return h
			},
			expectError:   true,
			errorContains: "failed to persist status",
		},
		{
			name: "Write Error - Permission Denied",
			setupHandler: func() *MaintenanceHandler {
				// Create a file with no write permissions
				tmpDir := t.TempDir()
				statusFile := filepath.Join(tmpDir, "readonly.json")

				// Create the file
				err := os.WriteFile(statusFile, []byte(`{"enabled":false}`), 0400)
				require.NoError(t, err)

				// Remove write permissions
				err = os.Chmod(statusFile, 0400)
				require.NoError(t, err)

				h := &MaintenanceHandler{
					StatusFile: statusFile,
				}
				setMaintenanceHandler(h)
				return h
			},
			expectError:   true,
			errorContains: "failed to persist status",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Write Error - Permission Denied" && os.Geteuid() == 0 {
				t.Skip("skipping permission denied scenario when running as root")
			}

			// Setup handler
			tt.setupHandler()

			// Create admin handler with appropriate marshaller
			var adminHandler mockAdminHandler
			if tt.useErrorMarshal {
				adminHandler.marshaller = &errorJSONMarshaller{}
			} else {
				adminHandler.marshaller = &defaultJSONMarshaller{}
			}

			// Create request body
			body := map[string]interface{}{
				"enabled": true,
			}
			bodyBytes, err := json.Marshal(body)
			require.NoError(t, err)

			// Create request
			req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(bodyBytes))
			w := httptest.NewRecorder()

			// Execute request
			err = adminHandler.toggle(w, req)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMaintenanceHandler_RestartPersistence(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	statusFile := filepath.Join(tmpDir, "maintenance_status.json")

	// First handler instance
	h1 := &MaintenanceHandler{
		StatusFile: statusFile,
	}
	ctx := caddy.Context{}
	err := h1.Provision(ctx)
	require.NoError(t, err)

	// Set initial state
	h1.enabledMux.Lock()
	h1.enabled = true
	h1.enabledMux.Unlock()

	// Persist state
	status := struct {
		Enabled bool `json:"enabled"`
	}{
		Enabled: true,
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	err = os.WriteFile(statusFile, data, 0644)
	require.NoError(t, err)

	// Create new handler instance (simulating restart)
	h2 := &MaintenanceHandler{
		StatusFile:     statusFile,
		DefaultEnabled: false, // Different from persisted state
	}
	err = h2.Provision(ctx)
	require.NoError(t, err)

	// Check if state was restored
	h2.enabledMux.RLock()
	state := h2.enabled
	h2.enabledMux.RUnlock()

	assert.True(t, state, "Maintenance state should be restored from file")
}

func TestParseCaddyfile_NewOptions(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedM     *MaintenanceHandler
		expectErr     bool
		expectedErrIs string
	}{
		{
			name: "Default enabled true",
			input: `maintenance {
				default_enabled true
			}`,
			expectedM: &MaintenanceHandler{
				DefaultEnabled: true,
			},
		},
		{
			name: "Default enabled false",
			input: `maintenance {
				default_enabled false
			}`,
			expectedM: &MaintenanceHandler{
				DefaultEnabled: false,
			},
		},
		{
			name: "Status file path",
			input: `maintenance {
				status_file /var/lib/caddy/maintenance.json
			}`,
			expectedM: &MaintenanceHandler{
				StatusFile: "/var/lib/caddy/maintenance.json",
			},
		},
		{
			name: "Complete configuration with new options",
			input: `maintenance {
				template /path/to/template.html
				allowed_ips 192.168.1.100 10.0.0.1
				retry_after 600
				default_enabled true
				status_file /var/lib/caddy/maintenance.json
				request_retention_mode_timeout 30
			}`,
			expectedM: &MaintenanceHandler{
				HTMLTemplate:                "/path/to/template.html",
				AllowedIPs:                  []string{"192.168.1.100", "10.0.0.1"},
				RetryAfter:                  600,
				DefaultEnabled:              true,
				StatusFile:                  "/var/lib/caddy/maintenance.json",
				RequestRetentionModeTimeout: 30,
			},
		},
		{
			name: "Invalid default_enabled value",
			input: `maintenance {
				default_enabled invalid
			}`,
			expectErr:     true,
			expectedErrIs: "invalid default_enabled value",
		},
		{
			name: "Missing default_enabled value",
			input: `maintenance {
				default_enabled
			}`,
			expectErr: true,
		},
		{
			name: "Missing status_file value",
			input: `maintenance {
				status_file
			}`,
			expectErr: true,
		},
		{
			name: "Status file with invalid permissions",
			input: `maintenance {
				status_file /root/maintenance.json
			}`,
			expectedM: &MaintenanceHandler{
				StatusFile: "/root/maintenance.json",
			},
		},
		{
			name: "Allowed IPs file",
			input: `maintenance {
				allowed_ips_file /etc/caddy/allowed_ips.txt
			}`,
			expectedM: &MaintenanceHandler{
				AllowedIPsFile: "/etc/caddy/allowed_ips.txt",
			},
		},
		{
			name: "Combined allowed IPs and file",
			input: `maintenance {
				allowed_ips 192.168.1.100 10.0.0.1
				allowed_ips_file /etc/caddy/allowed_ips.txt
			}`,
			expectedM: &MaintenanceHandler{
				AllowedIPs:     []string{"192.168.1.100", "10.0.0.1"},
				AllowedIPsFile: "/etc/caddy/allowed_ips.txt",
			},
		},
		{
			name: "HTTP Basic Authentication",
			input: `maintenance {
				htpasswd_file /etc/caddy/.htpasswd
				auth_realm "Maintenance Access"
			}`,
			expectedM: &MaintenanceHandler{
				HtpasswdFile: "/etc/caddy/.htpasswd",
				AuthRealm:    "Maintenance Access",
			},
		},
		{
			name: "Complete configuration with authentication",
			input: `maintenance {
				template /path/to/template.html
				allowed_ips 192.168.1.100 10.0.0.1
				retry_after 600
				default_enabled true
				status_file /var/lib/caddy/maintenance.json
				request_retention_mode_timeout 30
				htpasswd_file /etc/caddy/.htpasswd
				auth_realm "Maintenance Access"
			}`,
			expectedM: &MaintenanceHandler{
				HTMLTemplate:                "/path/to/template.html",
				AllowedIPs:                  []string{"192.168.1.100", "10.0.0.1"},
				RetryAfter:                  600,
				DefaultEnabled:              true,
				StatusFile:                  "/var/lib/caddy/maintenance.json",
				RequestRetentionModeTimeout: 30,
				HtpasswdFile:                "/etc/caddy/.htpasswd",
				AuthRealm:                   "Maintenance Access",
			},
		},
		{
			name: "Missing auth_realm value",
			input: `maintenance {
				auth_realm
			}`,
			expectErr: true,
		},
		{
			name: "Missing htpasswd_file value",
			input: `maintenance {
				htpasswd_file
			}`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := caddyfile.NewTestDispenser(tt.input)
			h := httpcaddyfile.Helper{Dispenser: d}

			actual, err := parseCaddyfile(h)

			if tt.expectErr {
				require.Error(t, err)
				if tt.expectedErrIs != "" {
					assert.Contains(t, err.Error(), tt.expectedErrIs)
				}
				return
			}

			require.NoError(t, err)
			actualHandler, ok := actual.(*MaintenanceHandler)
			require.True(t, ok)

			// Compare fields
			if tt.expectedM.HTMLTemplate != "" {
				assert.Equal(t, tt.expectedM.HTMLTemplate, actualHandler.HTMLTemplate)
			}
			if tt.expectedM.StatusFile != "" {
				assert.Equal(t, tt.expectedM.StatusFile, actualHandler.StatusFile)
			}
			assert.Equal(t, tt.expectedM.DefaultEnabled, actualHandler.DefaultEnabled)
			assert.Equal(t, tt.expectedM.AllowedIPs, actualHandler.AllowedIPs)
			assert.Equal(t, tt.expectedM.RetryAfter, actualHandler.RetryAfter)
			assert.Equal(t, tt.expectedM.RequestRetentionModeTimeout, actualHandler.RequestRetentionModeTimeout)
		})
	}
}

func TestMaintenanceHandler_FilePermissions(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	statusFile := filepath.Join(tmpDir, "maintenance_status.json")

	// Create admin handler
	adminHandler := AdminHandler{}

	// Setup maintenance handler
	maintenanceHandler := &MaintenanceHandler{
		StatusFile: statusFile,
	}
	setMaintenanceHandler(maintenanceHandler)

	// Create request to enable maintenance
	body := map[string]interface{}{
		"enabled": true,
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(bodyBytes))
	w := httptest.NewRecorder()

	// Execute request
	err = adminHandler.toggle(w, req)
	require.NoError(t, err)

	// Check file permissions
	info, err := os.Stat(statusFile)
	require.NoError(t, err)

	// Check if permissions are 0644
	expectedPerm := os.FileMode(0644)
	assert.Equal(t, expectedPerm, info.Mode().Perm(),
		"Status file should have 0644 permissions")
}

func TestMaintenanceHandler_ServeHTTP_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		setupHandler   func() *MaintenanceHandler
		setupRequest   func() *http.Request
		expectedStatus int
	}{
		{
			name: "Context Cancelled During Retention",
			setupHandler: func() *MaintenanceHandler {
				ctx, cancel := context.WithCancel(context.Background())
				h := &MaintenanceHandler{
					HTMLTemplate:                defaultHTMLTemplate,
					RequestRetentionModeTimeout: 30,
					ctx:                         caddy.Context{Context: ctx},
				}
				h.enabledMux.Lock()
				h.enabled = true
				h.enabledMux.Unlock()

				// Cancel context after a short delay
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()

				return h
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "http://example.com", nil)
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name: "Timer Expiration During Retention",
			setupHandler: func() *MaintenanceHandler {
				ctx := context.Background()
				h := &MaintenanceHandler{
					HTMLTemplate:                defaultHTMLTemplate,
					RequestRetentionModeTimeout: 1, // Very short timeout
					ctx:                         caddy.Context{Context: ctx},
				}
				h.enabledMux.Lock()
				h.enabled = true
				h.enabledMux.Unlock()
				return h
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "http://example.com", nil)
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.setupHandler()
			req := tt.setupRequest()
			w := httptest.NewRecorder()

			// Create next handler
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			})

			// Execute handler in a goroutine
			errChan := make(chan error, 1)
			go func() {
				errChan <- h.ServeHTTP(w, req, next)
			}()

			// Wait for completion with timeout
			select {
			case err := <-errChan:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("Request did not complete in time")
			}

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestMaintenanceHandler_StatusPersistence(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	statusFile := filepath.Join(tmpDir, "maintenance_status.json")

	// Create admin handler
	adminHandler := AdminHandler{}

	tests := []struct {
		name          string
		enabled       bool
		expectError   bool
		checkContent  bool
		expectEnabled bool
	}{
		{
			name:          "Enable Maintenance",
			enabled:       true,
			expectError:   false,
			checkContent:  true,
			expectEnabled: true,
		},
		{
			name:          "Disable Maintenance",
			enabled:       false,
			expectError:   false,
			checkContent:  true,
			expectEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup maintenance handler
			maintenanceHandler := &MaintenanceHandler{
				StatusFile: statusFile,
			}
			setMaintenanceHandler(maintenanceHandler)

			// Create request body
			body := map[string]interface{}{
				"enabled": tt.enabled,
			}
			bodyBytes, err := json.Marshal(body)
			require.NoError(t, err)

			// Create request
			req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(bodyBytes))
			w := httptest.NewRecorder()

			// Execute request
			err = adminHandler.toggle(w, req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			if tt.checkContent {
				// Read and verify file content
				content, err := os.ReadFile(statusFile)
				require.NoError(t, err)

				var status struct {
					Enabled bool `json:"enabled"`
				}
				err = json.Unmarshal(content, &status)
				require.NoError(t, err)

				assert.Equal(t, tt.expectEnabled, status.Enabled)
			}
		})
	}
}

func TestMaintenanceHandler_AllowedIPsFile(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		fileContent   string
		expectedIPs   []string
		expectError   bool
		errorContains string
	}{
		{
			name: "Valid IPs with comments",
			fileContent: `# Office network
192.168.1.100  # Admin workstation
10.0.0.1       # Server room

# Development team
192.168.5.0/22 # Dev network range
10.0.1.0/24    # QA network range

# External access
172.16.0.0/16  # Corporate VPN
2001:db8::/32  # IPv6 test range

# Individual IPv6
2001:db8::1    # Test server
::1             # Localhost IPv6`,
			expectedIPs: []string{
				"192.168.1.100",
				"10.0.0.1",
				"192.168.5.0/22",
				"10.0.1.0/24",
				"172.16.0.0/16",
				"2001:db8::/32",
				"2001:db8::1",
				"::1",
			},
		},
		{
			name: "Empty file",
			fileContent: `# This file is empty
`,
			expectedIPs: []string{},
		},
		{
			name: "Only comments",
			fileContent: `# This is a comment
# Another comment
# No IPs here`,
			expectedIPs: []string{},
		},
		{
			name: "Invalid IP in file",
			fileContent: `192.168.1.100
invalid-ip
10.0.0.1`,
			expectError:   true,
			errorContains: "invalid IP address",
		},
		{
			name: "Invalid CIDR in file",
			fileContent: `192.168.1.100
192.168.1.0/33  # Invalid CIDR
10.0.0.1`,
			expectError:   true,
			errorContains: "invalid CIDR notation",
		},
		{
			name: "Mixed valid and invalid",
			fileContent: `192.168.1.100  # Valid
192.168.1.0/24   # Valid
invalid-ip       # Invalid
10.0.0.1         # Valid`,
			expectError:   true,
			errorContains: "invalid IP address",
		},
		{
			name: "Whitespace handling",
			fileContent: `  192.168.1.100  
  10.0.0.1  
  192.168.5.0/22  
`,
			expectedIPs: []string{
				"192.168.1.100",
				"10.0.0.1",
				"192.168.5.0/22",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a unique file for each test case
			ipsFile := filepath.Join(tmpDir, fmt.Sprintf("ips_%s.txt", strings.ReplaceAll(tt.name, " ", "_")))

			// Write test file
			err := os.WriteFile(ipsFile, []byte(tt.fileContent), 0644)
			require.NoError(t, err, "Failed to write IPs file")

			// Create handler
			h := &MaintenanceHandler{
				AllowedIPsFile: ipsFile,
			}

			// Test Provision
			err = h.Provision(caddy.Context{})

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)

			// Verify that IPs were loaded correctly
			assert.ElementsMatch(t, tt.expectedIPs, h.AllowedIPs, "Loaded IPs should match expected IPs")
		})
	}
}

func TestMaintenanceHandler_AllowedIPsFileCombined(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	ipsFile := filepath.Join(tmpDir, "allowed_ips.txt")

	// Create IPs file
	fileContent := `# Office network
192.168.1.100  # Admin workstation
10.0.0.1       # Server room
192.168.5.0/22 # Dev network range`

	err := os.WriteFile(ipsFile, []byte(fileContent), 0644)
	require.NoError(t, err)

	// Create handler with both inline IPs and file
	h := &MaintenanceHandler{
		AllowedIPs:     []string{"172.16.0.0/16", "2001:db8::1"},
		AllowedIPsFile: ipsFile,
	}

	// Test Provision
	err = h.Provision(caddy.Context{})
	require.NoError(t, err)

	// Verify that both inline and file IPs are combined
	expectedIPs := []string{
		"172.16.0.0/16",  // From inline
		"2001:db8::1",    // From inline
		"192.168.1.100",  // From file
		"10.0.0.1",       // From file
		"192.168.5.0/22", // From file
	}

	assert.ElementsMatch(t, expectedIPs, h.AllowedIPs, "Combined IPs should match expected IPs")
}

func TestLoadIPsFromFile(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		fileContent   string
		expectedIPs   []string
		expectError   bool
		errorContains string
	}{
		{
			name: "Simple IPs",
			fileContent: `192.168.1.100
10.0.0.1
192.168.5.0/22`,
			expectedIPs: []string{
				"192.168.1.100",
				"10.0.0.1",
				"192.168.5.0/22",
			},
		},
		{
			name: "IPs with inline comments",
			fileContent: `192.168.1.100 # Admin
10.0.0.1 # Server
192.168.5.0/22 # Network`,
			expectedIPs: []string{
				"192.168.1.100",
				"10.0.0.1",
				"192.168.5.0/22",
			},
		},
		{
			name: "Mixed comments and IPs",
			fileContent: `# Header comment
192.168.1.100
# Middle comment
10.0.0.1 # Inline comment
# Footer comment`,
			expectedIPs: []string{
				"192.168.1.100",
				"10.0.0.1",
			},
		},
		{
			name: "Empty lines and whitespace",
			fileContent: `

192.168.1.100

  10.0.0.1  

192.168.5.0/22

`,
			expectedIPs: []string{
				"192.168.1.100",
				"10.0.0.1",
				"192.168.5.0/22",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a unique file for each test case
			ipsFile := filepath.Join(tmpDir, fmt.Sprintf("test_%s.txt", strings.ReplaceAll(tt.name, " ", "_")))

			// Write test file
			err := os.WriteFile(ipsFile, []byte(tt.fileContent), 0644)
			require.NoError(t, err, "Failed to write IPs file")

			// Create handler
			h := &MaintenanceHandler{}

			// Test loadIPsFromFile directly
			loadedIPs, err := h.loadIPsFromFile(ipsFile)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expectedIPs, loadedIPs, "Loaded IPs should match expected IPs")
		})
	}
}

func TestMaintenanceHandler_HTTPBasicAuth(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	// Create htpasswd file with bcrypt hash
	htpasswdContent := `# Test users
admin:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi  # password: password
user:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi   # password: password
# Comment line
invalid_user:invalid_hash
`

	htpasswdFile := filepath.Join(tmpDir, "test.htpasswd")
	err := os.WriteFile(htpasswdFile, []byte(htpasswdContent), 0644)
	require.NoError(t, err, "Failed to write htpasswd file")

	tests := []struct {
		name           string
		setupHandler   func() *MaintenanceHandler
		setupRequest   func() *http.Request
		expectedStatus int
		expectAuth     bool
	}{
		{
			name: "No Authentication - Should See Maintenance Page",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					DefaultEnabled: true,
				}
				return h
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "http://example.com", nil)
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectAuth:     false,
		},
		{
			name: "Valid Authentication - Should Bypass Maintenance",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					HtpasswdFile:   htpasswdFile,
					AuthRealm:      "Test Realm",
					DefaultEnabled: true,
				}
				return h
			},
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "http://example.com", nil)
				// admin:password encoded in base64
				req.Header.Set("Authorization", "Basic YWRtaW46cGFzc3dvcmQ=")
				return req
			},
			expectedStatus: http.StatusOK,
			expectAuth:     true,
		},
		{
			name: "Invalid Authentication - Should See Maintenance Page",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					HtpasswdFile:   htpasswdFile,
					AuthRealm:      "Test Realm",
					DefaultEnabled: true,
				}
				return h
			},
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "http://example.com", nil)
				// admin:wrongpassword encoded in base64
				req.Header.Set("Authorization", "Basic YWRtaW46d3JvbmdwYXNzd29yZA==")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
		{
			name: "No Authorization Header - Should See Maintenance Page",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					HtpasswdFile:   htpasswdFile,
					AuthRealm:      "Test Realm",
					DefaultEnabled: true,
				}
				return h
			},
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "http://example.com", nil)
			},
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
		{
			name: "Invalid Authorization Format - Should See Maintenance Page",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					HtpasswdFile:   htpasswdFile,
					AuthRealm:      "Test Realm",
					DefaultEnabled: true,
				}
				return h
			},
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "http://example.com", nil)
				req.Header.Set("Authorization", "InvalidFormat")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
		{
			name: "Non-existent User - Should See Maintenance Page",
			setupHandler: func() *MaintenanceHandler {
				h := &MaintenanceHandler{
					HtpasswdFile:   htpasswdFile,
					AuthRealm:      "Test Realm",
					DefaultEnabled: true,
				}
				return h
			},
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "http://example.com", nil)
				// nonexistent:password encoded in base64
				req.Header.Set("Authorization", "Basic bm9uZXhpc3RlbnQ6cGFzc3dvcmQ=")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.setupHandler()

			// Provision the handler to load htpasswd file
			ctx := caddy.Context{}
			err := h.Provision(ctx)
			require.NoError(t, err)

			req := tt.setupRequest()
			w := httptest.NewRecorder()

			// Create next handler that sets a specific header
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				w.Header().Set("X-Test", "request-processed")
				w.WriteHeader(http.StatusOK)
				return nil
			})

			// Execute handler
			err = h.ServeHTTP(w, req, next)
			require.NoError(t, err)

			// Check status code
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectAuth {
				// Verify the request was passed to next handler
				assert.Equal(t, "request-processed", w.Header().Get("X-Test"))
			} else {
				// Check for WWW-Authenticate header if authentication is configured
				if h.HtpasswdFile != "" {
					wwwAuth := w.Header().Get("WWW-Authenticate")
					assert.Contains(t, wwwAuth, "Basic realm=")
					if h.AuthRealm != "" {
						assert.Contains(t, wwwAuth, h.AuthRealm)
					}
				}
			}
		})
	}
}

func TestMaintenanceHandler_ParseHtpasswdFile(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		fileContent   string
		expectError   bool
		errorContains string
		expectedUsers []string
	}{
		{
			name: "Valid htpasswd file",
			fileContent: `# Test users
admin:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi
user:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi
`,
			expectedUsers: []string{"admin", "user"},
		},
		{
			name: "Empty file",
			fileContent: `# Empty file
`,
			expectedUsers: []string{},
		},
		{
			name: "Only comments",
			fileContent: `# This is a comment
# Another comment`,
			expectedUsers: []string{},
		},
		{
			name:          "Invalid format - missing colon",
			fileContent:   `admin$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi`,
			expectError:   true,
			errorContains: "invalid htpasswd format",
		},
		{
			name:          "Empty username",
			fileContent:   `:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi`,
			expectError:   true,
			errorContains: "empty username",
		},
		{
			name:          "Empty password hash",
			fileContent:   `admin:`,
			expectError:   true,
			errorContains: "empty password hash",
		},
		{
			name: "Mixed valid and invalid",
			fileContent: `admin:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi
invalid_format
user:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi`,
			expectError:   true,
			errorContains: "invalid htpasswd format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a unique file for each test case
			htpasswdFile := filepath.Join(tmpDir, fmt.Sprintf("test_%s.htpasswd", strings.ReplaceAll(tt.name, " ", "_")))

			// Write test file
			err := os.WriteFile(htpasswdFile, []byte(tt.fileContent), 0644)
			require.NoError(t, err, "Failed to write htpasswd file")

			// Create handler
			h := &MaintenanceHandler{
				HtpasswdFile: htpasswdFile,
			}

			// Test parseHtpasswdFile
			err = h.parseHtpasswdFile()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)

			// Verify that users were loaded correctly
			var actualUsers []string
			for username := range h.htpasswdEntries {
				actualUsers = append(actualUsers, username)
			}
			assert.ElementsMatch(t, tt.expectedUsers, actualUsers, "Loaded users should match expected users")
		})
	}
}

func TestMaintenanceHandler_VerifyPassword(t *testing.T) {
	h := &MaintenanceHandler{}

	tests := []struct {
		name        string
		password    string
		storedHash  []byte
		expectValid bool
	}{
		{
			name:        "Valid bcrypt hash",
			password:    "password",
			storedHash:  []byte("$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"),
			expectValid: true,
		},
		{
			name:        "Invalid password with valid bcrypt hash",
			password:    "wrongpassword",
			storedHash:  []byte("$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"),
			expectValid: false,
		},
		{
			name:        "Non-bcrypt hash (unsupported)",
			password:    "password",
			storedHash:  []byte("$1$salt$hash"),
			expectValid: false,
		},
		{
			name:        "Plain text (unsupported)",
			password:    "password",
			storedHash:  []byte("password"),
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := h.verifyPassword(tt.password, tt.storedHash)
			assert.Equal(t, tt.expectValid, result)
		})
	}
}

func TestMaintenanceHandler_CombinedAccessControl(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create htpasswd file
	htpasswdContent := `admin:$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi`
	htpasswdFile := filepath.Join(tmpDir, "test.htpasswd")
	err := os.WriteFile(htpasswdFile, []byte(htpasswdContent), 0644)
	require.NoError(t, err)

	tests := []struct {
		name           string
		allowedIPs     []string
		clientIP       string
		authHeader     string
		expectedStatus int
		expectBypass   bool
	}{
		{
			name:           "IP Allowed - Should Bypass",
			allowedIPs:     []string{"192.168.1.100"},
			clientIP:       "192.168.1.100",
			expectedStatus: http.StatusOK,
			expectBypass:   true,
		},
		{
			name:           "Auth Valid - Should Bypass",
			allowedIPs:     []string{"192.168.1.100"},
			clientIP:       "192.168.1.101",
			authHeader:     "Basic YWRtaW46cGFzc3dvcmQ=", // admin:password
			expectedStatus: http.StatusOK,
			expectBypass:   true,
		},
		{
			name:           "Neither IP nor Auth - Should Block",
			allowedIPs:     []string{"192.168.1.100"},
			clientIP:       "192.168.1.101",
			expectedStatus: http.StatusUnauthorized,
			expectBypass:   false,
		},
		{
			name:           "Both IP and Auth - Should Bypass (IP takes precedence)",
			allowedIPs:     []string{"192.168.1.100"},
			clientIP:       "192.168.1.100",
			authHeader:     "Basic YWRtaW46cGFzc3dvcmQ=",
			expectedStatus: http.StatusOK,
			expectBypass:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MaintenanceHandler{
				AllowedIPs:   tt.allowedIPs,
				HtpasswdFile: htpasswdFile,
				AuthRealm:    "Test Realm",
			}

			// Provision the handler
			ctx := caddy.Context{}
			err := h.Provision(ctx)
			require.NoError(t, err)

			// Enable maintenance mode
			h.enabledMux.Lock()
			h.enabled = true
			h.enabledMux.Unlock()

			// Create request
			req := httptest.NewRequest("GET", "http://example.com", nil)
			req.RemoteAddr = tt.clientIP
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()

			// Create next handler
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				w.Header().Set("X-Test", "request-processed")
				w.WriteHeader(http.StatusOK)
				return nil
			})

			// Execute handler
			err = h.ServeHTTP(w, req, next)
			require.NoError(t, err)

			// Check status code
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectBypass {
				assert.Equal(t, "request-processed", w.Header().Get("X-Test"))
			} else {
				// Check for WWW-Authenticate header
				wwwAuth := w.Header().Get("WWW-Authenticate")
				assert.Contains(t, wwwAuth, "Basic realm=")
			}
		})
	}
}

func TestMaintenanceHandler_BypassPaths(t *testing.T) {
	tests := []struct {
		name           string
		bypassPaths    []string
		requestPath    string
		expectedBypass bool
	}{
		{
			name:           "No bypass paths configured",
			bypassPaths:    []string{},
			requestPath:    "/.well-known/mercure",
			expectedBypass: false,
		},
		{
			name:           "Exact path match",
			bypassPaths:    []string{"/.well-known/mercure"},
			requestPath:    "/.well-known/mercure",
			expectedBypass: true,
		},
		{
			name:           "Directory wildcard match",
			bypassPaths:    []string{"/.well-known/*"},
			requestPath:    "/.well-known/mercure",
			expectedBypass: true,
		},
		{
			name:           "Directory wildcard match nested",
			bypassPaths:    []string{"/.well-known/*"},
			requestPath:    "/.well-known/mercure/hub",
			expectedBypass: true,
		},
		{
			name:           "Root path bypass",
			bypassPaths:    []string{"/"},
			requestPath:    "/",
			expectedBypass: true,
		},
		{
			name:           "Root path bypass with trailing slash",
			bypassPaths:    []string{"/"},
			requestPath:    "/",
			expectedBypass: true,
		},
		{
			name:           "Multiple bypass paths",
			bypassPaths:    []string{"/.well-known/*", "/health", "/status"},
			requestPath:    "/health",
			expectedBypass: true,
		},
		{
			name:           "Path not in bypass list",
			bypassPaths:    []string{"/.well-known/*", "/health"},
			requestPath:    "/api/users",
			expectedBypass: false,
		},
		{
			name:           "Case sensitive match",
			bypassPaths:    []string{"/.well-known/mercure"},
			requestPath:    "/.WELL-KNOWN/MERCURE",
			expectedBypass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MaintenanceHandler{
				BypassPaths: tt.bypassPaths,
			}

			result := h.isPathBypassed(tt.requestPath)
			if result != tt.expectedBypass {
				t.Errorf("isPathBypassed() = %v, want %v for path %s with bypass paths %v",
					result, tt.expectedBypass, tt.requestPath, tt.bypassPaths)
			}
		})
	}
}

func TestMaintenanceHandler_ServeHTTP_BypassPaths(t *testing.T) {
	// Create a test handler that records if it was called
	var handlerCalled bool
	var capturedRequest *http.Request

	testHandler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		handlerCalled = true
		capturedRequest = r
		w.WriteHeader(http.StatusOK)
		return nil
	})

	// Test with bypass path configured
	h := &MaintenanceHandler{
		enabled:     true,
		BypassPaths: []string{"/.well-known/*", "/health"},
	}

	// Test request to bypassed path
	req := httptest.NewRequest("GET", "/.well-known/mercure", nil)
	w := httptest.NewRecorder()

	err := h.ServeHTTP(w, req, testHandler)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if !handlerCalled {
		t.Error("Handler was not called for bypassed path")
	}

	if capturedRequest.URL.Path != "/.well-known/mercure" {
		t.Errorf("Captured request path = %v, want %v", capturedRequest.URL.Path, "/.well-known/mercure")
	}

	if w.Code != http.StatusOK {
		t.Errorf("Response code = %v, want %v", w.Code, http.StatusOK)
	}

	// Reset for next test
	handlerCalled = false
	capturedRequest = nil

	// Test request to non-bypassed path (should show maintenance page)
	req = httptest.NewRequest("GET", "/api/users", nil)
	w = httptest.NewRecorder()

	err = h.ServeHTTP(w, req, testHandler)
	if err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if handlerCalled {
		t.Error("Handler was called for non-bypassed path")
	}

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Response code = %v, want %v", w.Code, http.StatusServiceUnavailable)
	}
}

func TestParseCaddyfile_BypassPaths(t *testing.T) {
	tests := []struct {
		name        string
		caddyfile   string
		expectError bool
		expected    []string
	}{
		{
			name: "Single bypass path",
			caddyfile: `maintenance {
				bypass_paths /.well-known/*
			}`,
			expectError: false,
			expected:    []string{"/.well-known/*"},
		},
		{
			name: "Multiple bypass paths",
			caddyfile: `maintenance {
				bypass_paths /.well-known/* /health /status
			}`,
			expectError: false,
			expected:    []string{"/.well-known/*", "/health", "/status"},
		},
		{
			name: "Bypass paths with other options",
			caddyfile: `maintenance {
				bypass_paths /.well-known/*
				default_enabled true
				retry_after 300
			}`,
			expectError: false,
			expected:    []string{"/.well-known/*"},
		},
		{
			name: "Empty bypass paths",
			caddyfile: `maintenance {
				bypass_paths
			}`,
			expectError: false,
			expected:    nil, // Le parser retourne nil quand aucun argument n'est fourni
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new dispenser with the test input
			d := caddyfile.NewTestDispenser(tt.caddyfile)

			// Parse the Caddyfile
			h := httpcaddyfile.Helper{Dispenser: d}
			actual, err := parseCaddyfile(h)

			// Check error expectations
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Type assert and compare the results
			actualHandler, ok := actual.(*MaintenanceHandler)
			require.True(t, ok)

			// Compare BypassPaths
			assert.Equal(t, tt.expected, actualHandler.BypassPaths)
		})
	}
}
