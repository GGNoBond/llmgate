package utils

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// FindProjectRoot /*--- FindProjectRoot ---*/
// FindProjectRoot find the root directory of the project.
func FindProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		if dir == "/" {
			return "", os.ErrNotExist
		}
		dir = filepath.Dir(dir)
	}
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func WriteJSONErr(w http.ResponseWriter, status int, errMsg string) {
	WriteJSON(w, status, map[string]string{"error": errMsg})
}

func Defer[T any](p *T, defaultVal T) T {
	if p == nil {
		return defaultVal
	}
	return *p
}
