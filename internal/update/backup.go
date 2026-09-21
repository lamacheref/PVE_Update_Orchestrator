// Gestion des backups PBS : anti-double-backup, timeouts longs/illimités.
//
// Règle : jamais de vzdump si un backup est déjà en cours sur le guest
// (sinon erreur Proxmox). On attend sa fin (borne configurable), puis on
// lance le nôtre — ou on reporte la cible (skip, rejouée au prochain run).
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
)

// ErrBackupBusy signale un backup déjà en cours (report, pas échec).
var ErrBackupBusy = errors.New("backup déjà en cours sur ce guest")

type taskEntry struct {
	UPID   string `json:"upid"`
	Type   string `json:"type"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

// findRunningVzdump cherche un vzdump actif pour vmid dans `pvesh … tasks`.
func findRunningVzdump(jsonOut string, vmid int) (string, bool) {
	var tasks []taskEntry
	if err := json.Unmarshal([]byte(jsonOut), &tasks); err != nil {
		return "", false
	}
	want := fmt.Sprintf(":vzdump:%d:", vmid)
	for _, t := range tasks {
		if t.Type != "vzdump" || !strings.Contains(t.UPID, want) {
			continue
		}
		if t.Status == "running" || t.Status == "" {
			return t.UPID, true
		}
	}
	return "", false
}

// BackupInProgress détecte un backup en cours (pvesh, fallback ps).
func BackupInProgress(ctx context.Context, run Runner, nodeName, nodeIP string, vmid int) (bool, string) {
	out, err := run(ctx, nodeIP, fmt.Sprintf("pvesh get /nodes/%s/tasks --limit 100 --output-format json 2>/dev/null || true", nodeName))
	if err == nil {
		if upid, ok := findRunningVzdump(out, vmid); ok {
			return true, upid
		}
		// pvesh muet ou vide : on ne conclut pas, on tente le fallback.
	}
	out, err = run(ctx, nodeIP, fmt.Sprintf("ps -eo args | grep '[v]zdump %d' || true", vmid))
	if err != nil {
		return false, ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, fmt.Sprintf("vzdump %d", vmid)) {
			return true, strings.TrimSpace(line)
		}
	}
	return false, ""
}

// waitBackupClear attend la fin du backup en cours (maxWait<=0 = ne pas attendre).
func waitBackupClear(ctx context.Context, run Runner, nodeName, nodeIP string, vmid int, target string, maxWait time.Duration) error {
	if maxWait <= 0 {
		return ErrBackupBusy
	}
	deadline := time.Now().Add(maxWait)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	n := 0
	for {
		busy, _ := BackupInProgress(ctx, run, nodeName, nodeIP, vmid)
		if !busy {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrBackupBusy
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			n++
			if n%5 == 0 {
				progress(target, fmt.Sprintf("backup tiers en cours, attente (reste %s)", time.Until(deadline).Round(time.Minute)))
			}
		}
	}
}

// PBSOnline vérifie que le datastore PBS est présent et actif (fail-closed).
func PBSOnline(ctx context.Context, run Runner, nodeIP, storage string) (bool, string) {
	out, err := run(ctx, nodeIP, "pvesm status 2>/dev/null || true")
	if err != nil {
		return false, err.Error()
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == storage {
			if f[2] == "active" {
				return true, "actif"
			}
			return false, "status=" + f[2]
		}
	}
	return false, "storage absent de pvesm status"
}

// BackupGuest snapshotte un guest vers PBS (fail-closed).
// BackupTimeout<=0 = suivi quasi-illimité (plafond technique 24h, annulable
// via ctx), heartbeat toutes les 5 min.
func BackupGuest(ctx context.Context, run Runner, nodeIP string, g inventory.Guest, o Options, runID string) (string, error) {
	if busy, _ := BackupInProgress(ctx, run, g.Node, nodeIP, g.VMID); busy {
		if err := waitBackupClear(ctx, run, g.Node, nodeIP, g.VMID,
			fmt.Sprintf("%s/%d@%s", g.Kind, g.VMID, g.Name), o.BackupWait); err != nil {
			return "", err
		}
	}
	bctx := ctx
	cancel := context.CancelFunc(func() {})
	if o.BackupTimeout > 0 {
		bctx, cancel = context.WithTimeout(ctx, o.BackupTimeout)
	} else {
		bctx, cancel = context.WithTimeout(ctx, 24*time.Hour)
	}
	defer cancel()

	target := fmt.Sprintf("%s/%d@%s", g.Kind, g.VMID, g.Name)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				progress(target, "backup PBS en cours")
			}
		}
	}()

	out, err := run(bctx, nodeIP, fmt.Sprintf("vzdump %d --mode snapshot --storage %s --compress zstd --notes-template 'pre-update %s'",
		g.VMID, o.PBSStorage, runID))
	if err != nil {
		return "", fmt.Errorf("vzdump : %w (sortie : %s)", err, truncate(out, 300))
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Finished Backup") {
			return backupVolume(out), nil
		}
	}
	return "", fmt.Errorf("vzdump sans marqueur Finished Backup (sortie : %s)", truncate(out, 300))
}
