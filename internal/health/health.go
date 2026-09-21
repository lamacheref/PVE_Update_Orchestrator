// Package health exécute les pré/post-checks sur nodes et guests.
// Chaque check rend un résultat structuré ; le runner décide (hold/skip).
package health

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
)

// Runner exécute une commande sur un hôte.
type Runner func(ctx context.Context, host, cmd string) (string, error)

// Check est le résultat d'un contrôle.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// FailedUnits compte les unités en échec dans `systemctl --failed`.
func FailedUnits(out string) (int, []string) {
	var bad []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "UNIT") || strings.HasPrefix(line, "0 loaded units") {
			continue
		}
		if strings.Contains(line, ".service") || strings.Contains(line, ".scope") ||
			strings.Contains(line, ".mount") || strings.Contains(line, "failed") {
			bad = append(bad, line)
		}
	}
	return len(bad), bad
}

// DiskUsePct lit `df -P /` → pourcentage utilisé (-1 si illisible).
func DiskUsePct(out string) int {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] == "Filesystem" {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSuffix(f[4], "%")); err == nil {
			return v
		}
	}
	return -1
}

// ErrorLines rend les lignes non vides, plafonnées à max (avec compteur total).
func ErrorLines(out string, max int) (total int, sample []string) {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		total++
		if len(sample) < max {
			sample = append(sample, strings.TrimSpace(line))
		}
	}
	return total, sample
}

var reQuorum = regexp.MustCompile(`(?m)^\s*(Quorate|Quorum):\s*(Yes|No)`)

// Quorate lit pvecm status.
func Quorate(out string) bool {
	m := reQuorum.FindStringSubmatch(out)
	return len(m) == 3 && m[2] == "Yes"
}

// CheckNode exécute les contrôles d'un node Proxmox (lecture seule).
func CheckNode(ctx context.Context, run Runner, host string) []Check {
	var checks []Check
	add := func(name, cmd string, interpret func(string) (bool, string)) {
		out, err := run(ctx, host, cmd)
		if err != nil {
			checks = append(checks, Check{Name: name, Detail: "exec: " + err.Error()})
			return
		}
		ok, detail := interpret(out)
		checks = append(checks, Check{Name: name, OK: ok, Detail: detail})
	}

	add("quorum", "pvecm status", func(out string) (bool, string) {
		if Quorate(out) {
			return true, "quorate"
		}
		return false, "NON quorate — hold impératif"
	})
	add("failed-units", "systemctl --failed --no-pager", func(out string) (bool, string) {
		n, bad := FailedUnits(out)
		if n == 0 {
			return true, "0 unité en échec"
		}
		return false, fmt.Sprintf("%d unité(s) en échec : %s", n, strings.Join(bad, " | "))
	})
	add("journal-errors", `journalctl -p err --since "7 days ago" --no-pager -q | head -30`, func(out string) (bool, string) {
		total, sample := ErrorLines(out, 5)
		if total == 0 {
			return true, "0 erreur 7j"
		}
		return true, fmt.Sprintf("%d erreur(s) 7j (info) : %s", total, strings.Join(sample, " | "))
	})
	add("dmesg-errors", "dmesg -l err --nopager 2>/dev/null | tail -10", func(out string) (bool, string) {
		total, sample := ErrorLines(out, 3)
		if total == 0 {
			return true, "dmesg propre"
		}
		return true, fmt.Sprintf("%d erreur(s) kernel (info) : %s", total, strings.Join(sample, " | "))
	})
	add("disk-root", "df -P / | tail -1", func(out string) (bool, string) {
		pct := DiskUsePct(out)
		if pct < 0 {
			return false, "df illisible"
		}
		if pct >= 80 {
			return false, fmt.Sprintf("/ à %d%% — hold (seuil 80%%)", pct)
		}
		return true, fmt.Sprintf("/ à %d%%", pct)
	})
	add("pveversion", "pveversion | head -1", func(out string) (bool, string) {
		v := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		if v == "" {
			return false, "pveversion vide"
		}
		return true, v
	})
	return checks
}

// Critical rend vrai si un check bloquant est en échec (quorum, disk, failed-units).
func Critical(checks []Check) bool {
	for _, c := range checks {
		if !c.OK && (c.Name == "quorum" || c.Name == "disk-root" || c.Name == "failed-units") {
			return true
		}
	}
	return false
}

// execGuest construit la commande hôte pour exécuter dans un guest.
func execGuest(g inventory.Guest, inner string) string {
	if g.Kind == "lxc" {
		return fmt.Sprintf("pct exec %d -- bash -c %s", g.VMID, shellQuote(inner))
	}
	return fmt.Sprintf("qm guest exec %d --timeout 60 -- bash -c %s", g.VMID, shellQuote(inner))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CheckGuest contrôle un LXC/VM via le node hôte (agent requis pour qemu).
func CheckGuest(ctx context.Context, run Runner, nodeIP string, g inventory.Guest) []Check {
	var checks []Check
	if g.Kind == "qemu" {
		out, err := run(ctx, nodeIP, fmt.Sprintf("qm agent %d ping", g.VMID))
		if err != nil {
			return []Check{{Name: "agent", Detail: "agent QEMU KO : " + firstLine(out) + " " + err.Error()}}
		}
		checks = append(checks, Check{Name: "agent", OK: true, Detail: "ping OK"})
	}
	out, err := run(ctx, nodeIP, execGuest(g, "systemctl --failed --no-pager"))
	if err != nil {
		checks = append(checks, Check{Name: "failed-units", Detail: "exec guest : " + err.Error()})
		return checks
	}
	if g.Kind == "qemu" {
		// qm guest exec rend du JSON : extrait exitcode + sortie.
		code, data := ParseGuestExec(out)
		if code != 0 {
			checks = append(checks, Check{Name: "failed-units", Detail: fmt.Sprintf("exitcode=%d : %s", code, data)})
			return checks
		}
		out = data
	}
	n, bad := FailedUnits(out)
	if n == 0 {
		checks = append(checks, Check{Name: "failed-units", OK: true, Detail: "0 unité en échec"})
	} else {
		checks = append(checks, Check{Name: "failed-units", Detail: fmt.Sprintf("%d en échec : %s", n, strings.Join(bad, " | "))})
	}
	return checks
}

var reExitCode = regexp.MustCompile(`"exitcode"\s*:\s*(\d+)`)

func jsonStrRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`"` + key + `"\s*:\s*"((?:[^"\\]|\\.)*)"`)
}

// extractJSONStr extrait une valeur string JSON (avec déséchappement).
func extractJSONStr(out, key string) string {
	m := jsonStrRe(key).FindStringSubmatch(out)
	if len(m) != 2 {
		return ""
	}
	if s, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
		return s
	}
	return m[1]
}

// decodeB64 décode le out-data base64 du guest agent (tolère les retours ligne).
func decodeB64(s string) (string, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return "", fmt.Errorf("vide")
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("base64 invalide")
}

// ParseGuestExec extrait exitcode et out-data (base64 décodé si possible) du JSON qm.
func ParseGuestExec(out string) (int, string) {
	code := -1
	if m := reExitCode.FindStringSubmatch(out); len(m) == 2 {
		code, _ = strconv.Atoi(m[1])
	}
	data := extractJSONStr(out, "out-data")
	if data == "" {
		data = extractJSONStr(out, "err-data")
	}
	if decoded, err := decodeB64(data); err == nil && decoded != "" {
		data = decoded
	}
	return code, strings.TrimSpace(data)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
