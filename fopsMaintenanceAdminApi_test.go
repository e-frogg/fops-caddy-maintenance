package fopsMaintenance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2"
)

func TestAdminHandler_Routes(t *testing.T) {
	handler := AdminHandler{}
	routes := handler.Routes()

	if len(routes) != 2 {
		t.Errorf("Expected 2 routes, got %d", len(routes))
	}
}

func TestAdminHandler_GetStatus(t *testing.T) {
	// Setup
	handler := AdminHandler{}
	maintenanceHandler := &MaintenanceHandler{enabled: true}
	setMaintenanceHandler(maintenanceHandler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/maintenance/status", nil)
	w := httptest.NewRecorder()

	// Execute request
	err := handler.getStatus(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify response
	var response map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response["enabled"] {
		t.Error("Expected maintenance mode to be enabled")
	}
}

func TestAdminHandler_Toggle(t *testing.T) {
	// Setup
	handler := AdminHandler{}
	maintenanceHandler := &MaintenanceHandler{enabled: false}
	setMaintenanceHandler(maintenanceHandler)

	// Create test request body
	body := map[string]bool{"enabled": true}
	bodyBytes, _ := json.Marshal(body)

	// Create test request
	req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(bodyBytes))
	w := httptest.NewRecorder()

	// Execute request
	err := handler.toggle(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify handler state changed
	if !maintenanceHandler.enabled {
		t.Error("Expected maintenance mode to be enabled")
	}
}

func TestAdminHandler_Toggle_InvalidMethod(t *testing.T) {
	handler := AdminHandler{}
	req := httptest.NewRequest(http.MethodGet, "/maintenance/set", nil)
	w := httptest.NewRecorder()

	err := handler.toggle(w, req)
	if err == nil {
		t.Error("Expected error for invalid method")
	}
}

func TestAdminHandler_GetStatus_NoHandler(t *testing.T) {
	// Reset the handler
	setMaintenanceHandler(nil)

	handler := AdminHandler{}
	req := httptest.NewRequest(http.MethodGet, "/maintenance/status", nil)
	w := httptest.NewRecorder()

	err := handler.getStatus(w, req)
	if err == nil {
		t.Error("Expected error when no maintenance handler is set")
	}
}

func TestAdminHandler_Toggle_InvalidBody(t *testing.T) {
	handler := AdminHandler{}
	invalidJSON := []byte(`{"enabled": invalid}`)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(invalidJSON))
	w := httptest.NewRecorder()

	err := handler.toggle(w, req)
	if err == nil {
		t.Error("Expected error for invalid JSON body")
	}
}

func TestAdminHandler_Toggle_NoHandler(t *testing.T) {
	// Setup
	handler := AdminHandler{}
	// Reset the handler to nil
	setMaintenanceHandler(nil)

	// Create test request body
	body := map[string]bool{"enabled": true}
	bodyBytes, _ := json.Marshal(body)

	// Create test request
	req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(bodyBytes))
	w := httptest.NewRecorder()

	// Execute request
	err := handler.toggle(w, req)

	// Verify that we get an error
	if err == nil {
		t.Error("Expected error when no maintenance handler is set")
	}

	// Verify that it's the correct error type
	apiErr, ok := err.(caddy.APIError)
	if !ok {
		t.Error("Expected caddy.APIError type")
	}

	// Verify error details
	if ok && apiErr.HTTPStatus != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, apiErr.HTTPStatus)
	}
}
