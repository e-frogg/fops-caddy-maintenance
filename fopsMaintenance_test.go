package fopsMaintenance

import (
	"bytes"
	"context"
	"encoding/json"
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
			name:          "Real world IP should bypass maintenance",
			allowedIPs:    []string{"90.24.160.89"},
			clientIP:      "90.24.160.89:54321",
			expectBlocked: false,
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
		t.Run(tt.name, func(t *testing.T) {
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
