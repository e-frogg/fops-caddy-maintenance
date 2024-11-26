package fopsMaintenance

import (
	"context"
	"fmt"
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
	defer os.RemoveAll(testDir)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create handler with maintenance enabled
			h := &MaintenanceHandler{
				AllowedIPs: tt.allowedIPs,
			}
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
			err := h.ServeHTTP(w, req, next)
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
