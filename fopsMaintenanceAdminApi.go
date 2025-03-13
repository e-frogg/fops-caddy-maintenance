package fopsMaintenance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/caddyserver/caddy/v2"
)

var (
	maintenanceHandlerInstance *MaintenanceHandler
	instanceMux                sync.RWMutex
)

func init() {
	caddy.RegisterModule(AdminHandler{})
}

// AdminHandler handles maintenance mode administration
type AdminHandler struct{}

// CaddyModule returns the Caddy module information.
func (AdminHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "admin.api.maintenance",
		New: func() caddy.Module { return new(AdminHandler) },
	}
}

// Routes returns the admin router for the maintenance endpoints
func (h AdminHandler) Routes() []caddy.AdminRoute {
	return []caddy.AdminRoute{
		{
			Pattern: "/maintenance/status",
			Handler: caddy.AdminHandlerFunc(h.getStatus),
		},
		{
			Pattern: "/maintenance/set",
			Handler: caddy.AdminHandlerFunc(h.toggle),
		},
	}
}

func (h AdminHandler) getStatus(w http.ResponseWriter, r *http.Request) error {
	maintenanceHandler := getMaintenanceHandler()
	if maintenanceHandler == nil {
		return caddy.APIError{
			HTTPStatus: http.StatusNotFound,
			Err:        fmt.Errorf("maintenance handler not found"),
		}
	}

	maintenanceHandler.enabledMux.RLock()
	status := maintenanceHandler.enabled
	maintenanceHandler.enabledMux.RUnlock()

	return json.NewEncoder(w).Encode(map[string]bool{
		"enabled": status,
	})
}

func (h AdminHandler) toggle(w http.ResponseWriter, r *http.Request) error {
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
		data, err := json.Marshal(status)
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

func getMaintenanceHandler() *MaintenanceHandler {
	instanceMux.RLock()
	defer instanceMux.RUnlock()
	return maintenanceHandlerInstance
}

func setMaintenanceHandler(h *MaintenanceHandler) {
	instanceMux.Lock()
	maintenanceHandlerInstance = h
	instanceMux.Unlock()
}
