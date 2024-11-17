package fopsMaintenance

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func TestMaintenanceHandler(t *testing.T) {
	// Test cases
	tests := []struct {
		name           string
		maintenanceOn  bool
		acceptHeader   string
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
		})
	}
}

func TestProvision(t *testing.T) {
	h := &MaintenanceHandler{
		HTMLTemplate: "build/maintenance.html",
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
