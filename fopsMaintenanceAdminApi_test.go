package fopsMaintenance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	body := map[string]interface{}{
		"enabled":                        true,
		"request_retention_mode_timeout": 60,
	}
	bodyBytes, _ := json.Marshal(body)

	// Create test request
	req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(bodyBytes))
	w := httptest.NewRecorder()

	// Execute request
	err := handler.toggle(w, req)
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

	if maintenanceHandler.RequestRetentionModeTimeout != 60 {
		t.Errorf("Expected request retention mode timeout to be 60, got %d", maintenanceHandler.RequestRetentionModeTimeout)
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
	invalidJSON := []byte(`{"enabled": invalid, "request_retention_mode_timeout": "invalid"}`)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(invalidJSON))
	w := httptest.NewRecorder()

	err := handler.toggle(w, req)
	if err == nil {
		t.Error("Expected error for invalid JSON body")
	}

	apiErr, ok := err.(caddy.APIError)
	if !ok {
		t.Error("Expected caddy.APIError type")
	}

	if ok && apiErr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, apiErr.HTTPStatus)
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

// TestAdminHandler_MarshalError tests the error handling when json.Marshal fails
func TestAdminHandler_MarshalError(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	statusFile := filepath.Join(tmpDir, "maintenance_status.json")

	// Setup maintenance handler
	maintenanceHandler := &MaintenanceHandler{
		StatusFile: statusFile,
	}
	setMaintenanceHandler(maintenanceHandler)

	// Create admin handler
	adminHandler := AdminHandler{}

	// Create request body
	body := map[string]interface{}{
		"enabled": true,
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/maintenance/set", bytes.NewBuffer(bodyBytes))
	w := httptest.NewRecorder()

	// Set a custom JSON marshal function that always fails
	originalMarshalFunc := jsonMarshalFunc
	defer func() {
		// Restore the original marshal function
		jsonMarshalFunc = originalMarshalFunc
	}()

	// Replace with a function that always returns an error
	jsonMarshalFunc = func(v interface{}) ([]byte, error) {
		return nil, fmt.Errorf("simulated marshal error")
	}

	// Execute request
	err = adminHandler.toggle(w, req)

	// Verify that we got an error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal status")

	// Verify that the error is of the correct type
	apiErr, ok := err.(caddy.APIError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, apiErr.HTTPStatus)
}

// TestJSONMarshalFunctions tests the JSON marshal function helpers
func TestJSONMarshalFunctions(t *testing.T) {
	// Save original marshal function
	originalMarshalFunc := jsonMarshalFunc
	defer func() {
		// Restore the original marshal function
		jsonMarshalFunc = originalMarshalFunc
	}()

	// Test SetJSONMarshalFunc
	customMarshalCalled := false
	customMarshal := func(v interface{}) ([]byte, error) {
		customMarshalCalled = true
		return []byte("custom"), nil
	}

	// Set custom marshal function
	SetJSONMarshalFunc(customMarshal)

	// Test that the custom function is used
	data, err := jsonMarshalFunc(struct{}{})
	assert.NoError(t, err)
	assert.Equal(t, []byte("custom"), data)
	assert.True(t, customMarshalCalled)

	// Test ResetJSONMarshal
	ResetJSONMarshal()

	// Test that the original function is restored
	testData := map[string]string{"test": "value"}
	data, err = jsonMarshalFunc(testData)
	assert.NoError(t, err)

	// Parse the result to verify it's valid JSON
	var result map[string]string
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)
	assert.Equal(t, "value", result["test"])
}
