package fopsMaintenance

import (
	"net/http"
	"net/http/httptest"
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
