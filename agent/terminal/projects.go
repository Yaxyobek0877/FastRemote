package terminal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func resolveDirectory(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" || path == "~" {
		path = home
	}
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("papkaning to‘liq yo‘lini kiriting")
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("papka topilmadi yoki unga kirish mumkin emas")
	}
	entries, err := os.Open(path)
	if err != nil {
		return "", errors.New("papkani ochib bo‘lmadi")
	}
	defer entries.Close()
	info, err := entries.Stat()
	if err != nil || !info.IsDir() {
		return "", errors.New("tanlangan yo‘l papka emas")
	}
	return filepath.Clean(path), nil
}

// Presets are host configuration, not commands. Missing directories are hidden.
// A terminal has the same filesystem permissions as the existing OS account.
func Projects() []Project {
	home, _ := os.UserHomeDir()
	projects := []Project{{Name: "Uy papkasi", Path: home}}
	config := os.Getenv("TERMINAL_PROJECTS_FILE")
	if config == "" {
		root, err := os.UserConfigDir()
		if err == nil {
			config = filepath.Join(root, "fastremote", "projects.json")
		}
	}
	var configured []Project
	if b, err := os.ReadFile(config); err == nil && len(b) <= 64*1024 {
		_ = json.Unmarshal(b, &configured)
	}
	seen := map[string]bool{home: true}
	for _, p := range configured {
		dir, err := resolveDirectory(p.Path)
		if err != nil || seen[dir] || !titleValid(p.Name) {
			continue
		}
		projects = append(projects, Project{Name: p.Name, Path: dir})
		seen[dir] = true
	}
	return projects
}
