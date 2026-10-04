package tinytests

import (
	"fmt"
	"os"
	"path/filepath"
)

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
			return "", fmt.Errorf("go.mod not found")
		}
		dir = filepath.Dir(dir)
	}
}
func SingleJoinSlash(a, b string) string {
	if len(a) == 0 || len(b) == 0 {
		return a + b
	}
	aslash := a[len(a)-1] == '/'
	bslash := b[0] == '/'
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}
