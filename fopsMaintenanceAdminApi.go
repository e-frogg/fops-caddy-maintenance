package fopsMaintenance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/caddyserver/caddy/v2"
)

var (
	maintenanceHandlers []*MaintenanceHandler
	instanceMux         sync.RWMutex
	// For testing purposes only
	jsonMarshalFunc = json.Marshal
)

// ResetJSONMarshal resets the JSON marshal function to the default
// This is for testing purposes only
func ResetJSONMarshal() {
	jsonMarshalFunc = json.Marshal
}

// SetJSONMarshalFunc sets a custom JSON marshal function
// This is for testing purposes only
func SetJSONMarshalFunc(fn func(interface{}) ([]byte, error)) {
	jsonMarshalFunc = fn
}

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
	handlers := getMaintenanceHandlers()
	if len(handlers) == 0 {
		return caddy.APIError{
			HTTPStatus: http.StatusNotFound,
			Err:        fmt.Errorf("maintenance handler not found"),
		}
	}

	status := false
	for _, maintenanceHandler := range handlers {
		maintenanceHandler.enabledMux.RLock()
		enabled := maintenanceHandler.enabled
		maintenanceHandler.enabledMux.RUnlock()
		if enabled {
			status = true
			break
		}
	}

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

	handlers := getMaintenanceHandlers()
	if len(handlers) == 0 {
		return caddy.APIError{
			HTTPStatus: http.StatusNotFound,
			Err:        fmt.Errorf("maintenance handler not found"),
		}
	}

	status := struct {
		Enabled bool `json:"enabled"`
	}{
		Enabled: req.Enabled,
	}
	statusFiles := getUniqueStatusFiles(handlers)
	if len(statusFiles) > 0 {
		statusData, err := jsonMarshalFunc(status)
		if err != nil {
			return caddy.APIError{
				HTTPStatus: http.StatusInternalServerError,
				Err:        fmt.Errorf("failed to marshal status: %v", err),
			}
		}

		if err := persistStatusFiles(statusFiles, statusData); err != nil {
			return caddy.APIError{
				HTTPStatus: http.StatusInternalServerError,
				Err:        fmt.Errorf("failed to persist status: %v", err),
			}
		}
	}

	for _, maintenanceHandler := range handlers {
		maintenanceHandler.enabledMux.Lock()
		maintenanceHandler.enabled = req.Enabled
		maintenanceHandler.RequestRetentionModeTimeout = req.RequestRetentionModeTimeout
		maintenanceHandler.enabledMux.Unlock()
	}

	return json.NewEncoder(w).Encode(map[string]bool{
		"enabled": req.Enabled,
	})
}

func getMaintenanceHandler() *MaintenanceHandler {
	handlers := getMaintenanceHandlers()
	if len(handlers) == 0 {
		return nil
	}

	return handlers[0]
}

func getMaintenanceHandlers() []*MaintenanceHandler {
	instanceMux.Lock()
	defer instanceMux.Unlock()
	pruneInactiveHandlersLocked()

	handlers := make([]*MaintenanceHandler, len(maintenanceHandlers))
	copy(handlers, maintenanceHandlers)

	return handlers
}

func registerMaintenanceHandler(h *MaintenanceHandler) {
	if h == nil {
		return
	}

	instanceMux.Lock()
	defer instanceMux.Unlock()
	pruneInactiveHandlersLocked()

	for _, current := range maintenanceHandlers {
		if current == h {
			return
		}
	}

	maintenanceHandlers = append(maintenanceHandlers, h)
}

func setMaintenanceHandler(h *MaintenanceHandler) {
	instanceMux.Lock()
	if h == nil {
		maintenanceHandlers = nil
	} else {
		maintenanceHandlers = []*MaintenanceHandler{h}
	}
	instanceMux.Unlock()
}

func isMaintenanceHandlerActive(handler *MaintenanceHandler) bool {
	if handler == nil {
		return false
	}

	// Handlers manually created in tests may not have a Caddy context.
	if handler.ctx.Context == nil {
		return true
	}

	select {
	case <-handler.ctx.Done():
		return false
	default:
		return true
	}
}

func pruneInactiveHandlersLocked() {
	if len(maintenanceHandlers) == 0 {
		return
	}

	kept := maintenanceHandlers[:0]
	for _, handler := range maintenanceHandlers {
		if isMaintenanceHandlerActive(handler) {
			kept = append(kept, handler)
		}
	}

	if len(kept) == 0 {
		maintenanceHandlers = nil
		return
	}

	maintenanceHandlers = kept
}

func getUniqueStatusFiles(handlers []*MaintenanceHandler) []string {
	seen := make(map[string]struct{}, len(handlers))
	files := make([]string, 0, len(handlers))

	for _, handler := range handlers {
		if handler.StatusFile == "" {
			continue
		}

		if _, exists := seen[handler.StatusFile]; exists {
			continue
		}

		seen[handler.StatusFile] = struct{}{}
		files = append(files, handler.StatusFile)
	}

	return files
}

type statusFileBackup struct {
	Path    string
	Exists  bool
	Data    []byte
	Mode    os.FileMode
	Written bool
}

func persistStatusFiles(paths []string, data []byte) error {
	backups := make([]statusFileBackup, 0, len(paths))

	for _, path := range paths {
		backup := statusFileBackup{
			Path: path,
			Mode: 0644,
		}

		if info, err := os.Stat(path); err == nil {
			backup.Exists = true
			backup.Mode = info.Mode().Perm()
			previousData, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("failed to read current status file '%s': %v", path, readErr)
			}
			backup.Data = previousData
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to stat status file '%s': %v", path, err)
		}

		if err := atomicWriteFile(path, data, 0644); err != nil {
			rollbackPersistedStatusFiles(backups)
			return fmt.Errorf("failed writing status file '%s': %v", path, err)
		}

		backup.Written = true
		backups = append(backups, backup)
	}

	return nil
}

func rollbackPersistedStatusFiles(backups []statusFileBackup) {
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		if !backup.Written {
			continue
		}

		if !backup.Exists {
			_ = os.Remove(backup.Path)
			continue
		}

		_ = atomicWriteFile(backup.Path, backup.Data, backup.Mode)
	}
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".maintenance-status-*")
	if err != nil {
		return err
	}

	tmpPath := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}

	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	cleanup = false

	return nil
}
