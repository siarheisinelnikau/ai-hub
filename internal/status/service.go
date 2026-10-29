package status

import (
	"fmt"
	"os"
	"strings"
	"time"

	"aihub/internal/config"
	"aihub/internal/registry"
	"aihub/internal/sh"
)

// ServiceStatus mirrors app.py service_status_compat() output (only used if
// the registry has a top-level "services" list; current registry: none -> []).
type ServiceStatus struct {
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Desc        string  `json:"desc"`
	Port        int     `json:"port"`
	URL         *string `json:"url"` // null when port==0 (Python: f".." if port else None)
	Running     bool    `json:"running"`
	Healthy     bool    `json:"healthy"`
	EnabledUnit bool    `json:"enabled_unit"`
	ImageBuilt  bool    `json:"image_built"`
}

// intFromAny guards the s["port"] lookup: yaml.v3 unmarshals integers as
// int, JSON as float64. Python's s.get("port", 0) assumes an int.
func intFromAny(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// stringFromAny guards the s["category"]/s["desc"]/s["health_path"] lookups
// (Python: s.get("key", "") — missing or wrong type yields the default).
func stringFromAny(v interface{}) string {
	s, _ := v.(string)
	return s
}

// fileExists mirrors Python os.path.exists (true for file or directory).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// serviceStatusCompat mirrors app.py service_status_compat() (~lines 498-519):
// services[name] first, else a model with the same id, else an "unknown"
// stub. The model branch is dead on the current registry (no top-level
// "services") and is kept reasonable rather than parity-critical.
func serviceStatusCompat(name string, services map[string]map[string]interface{}, models map[string]registry.Model) ServiceStatus {
	s, ok := services[name]
	if !ok {
		if m, mok := models[name]; mok {
			unit := m.Sysd
			if unit == "" {
				unit = "ai-" + name
			}
			st := ServiceStatus{
				Name:        name,
				Category:    m.Category,
				Desc:        m.Desc,
				Port:        m.Port,
				Running:     false,
				Healthy:     false,
				EnabledUnit: fileExists("/etc/systemd/system/" + unit + ".service"),
				ImageBuilt:  true,
			}
			if m.Port != 0 {
				u := config.ServiceURL(m.Port)
				st.URL = &u
			}
			return st
		}
		// In neither services nor models. Python returns the partial dict
		// {"name","running","healthy","port","desc","category"} without
		// url/enabled_unit/image_built keys; the uniform struct adds them
		// (url nil, enabled_unit false, image_built true) — dead path.
		return ServiceStatus{Name: name, Running: false, Healthy: false, Port: 0, Desc: "unknown", Category: "LLM", URL: nil, ImageBuilt: true}
	}

	port := intFromAny(s["port"])
	unit := "ai-" + name + ".service"
	running := false
	healthy := false
	enabled := fileExists("/etc/systemd/system/" + unit)
	if enabled {
		_, out := sh.Run([]string{"systemctl", "is-active", unit}, 10*time.Second)
		running = strings.TrimSpace(out) == "active"
		if running {
			if hp := stringFromAny(s["health_path"]); hp != "" {
				rc, _ := sh.Run([]string{"curl", "-sf", "-o", "/dev/null", "-m", "4", fmt.Sprintf("http://127.0.0.1:%d%s", port, hp)}, 10*time.Second)
				healthy = rc == 0
			}
		}
	}
	var url *string
	if port != 0 {
		u := config.ServiceURL(port)
		url = &u
	}
	return ServiceStatus{
		Name:        name,
		Category:    stringFromAny(s["category"]),
		Desc:        stringFromAny(s["desc"]),
		Port:        port,
		URL:         url,
		Running:     running,
		Healthy:     healthy,
		EnabledUnit: enabled,
		ImageBuilt:  true,
	}
}
