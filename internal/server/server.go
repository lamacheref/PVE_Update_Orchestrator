// Package server expose le dashboard (lecture seule, phase 1) et l'API.
// Le frontend est embarqué dans le binaire (go:embed), zéro build JS.
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// RunSummary résume un run pour la liste.
type RunSummary struct {
	ID       string `json:"id"`
	Modified string `json:"modified"`
	DryRun   bool   `json:"dry_run"`
	OK       int    `json:"ok"`
	Fail     int    `json:"fail"`
	Hold     int    `json:"hold"`
	Targets  int    `json:"targets"`
}

type targetLite struct {
	OK    bool   `json:"ok"`
	Hold  bool   `json:"hold"`
	Error string `json:"error"`
}

type stateLite struct {
	RunID   string                `json:"run_id"`
	DryRun  bool                  `json:"dry_run"`
	Targets map[string]targetLite `json:"targets"`
}

// Listen bloque : statique + API.
func Listen(addr, stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, listRuns(stateDir))
	})
	mux.HandleFunc("/api/runs/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/runs/")
		if strings.Contains(id, "/") || strings.Contains(id, ".") {
			http.Error(w, "id invalide", http.StatusBadRequest)
			return
		}
		http.ServeFile(w, r, filepath.Join(stateDir, id+".json"))
	})
	mux.HandleFunc("/api/inventory", func(w http.ResponseWriter, r *http.Request) {
		latest, err := latestFile(stateDir, "inventory-*.json")
		if err != nil {
			http.Error(w, "aucun inventaire (lancez inventory sync --out "+filepath.Join(stateDir, "inventory-<ts>.json")+")", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, latest)
	})
	fmt.Printf("🖥️ dashboard : http://%s/ (état : %s)\n", addr, stateDir)
	return http.ListenAndServe(addr, mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func listRuns(dir string) []RunSummary {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []RunSummary
	for _, m := range matches {
		base := strings.TrimSuffix(filepath.Base(m), ".json")
		if strings.HasPrefix(base, "inventory-") {
			continue
		}
		st, err := os.Stat(m)
		if err != nil {
			continue
		}
		s := RunSummary{ID: base, Modified: st.ModTime().Format(time.RFC3339)}
		if raw, err := os.ReadFile(m); err == nil {
			var lite stateLite
			if json.Unmarshal(raw, &lite) == nil {
				s.DryRun = lite.DryRun
				for _, t := range lite.Targets {
					s.Targets++
					switch {
					case t.Hold:
						s.Hold++
					case t.OK:
						s.OK++
					default:
						s.Fail++
					}
				}
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out
}

func latestFile(dir, pattern string) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	if len(matches) == 0 {
		return "", fmt.Errorf("aucun %s", pattern)
	}
	sort.Slice(matches, func(i, j int) bool {
		si, _ := os.Stat(matches[i])
		sj, _ := os.Stat(matches[j])
		return si.ModTime().After(sj.ModTime())
	})
	return matches[0], nil
}
