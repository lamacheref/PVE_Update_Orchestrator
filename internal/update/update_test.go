package update

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
)

func TestUpgradableCount(t *testing.T) {
	out := "Listing... Done\npkg1/stable 1.0 amd64 [upgradable]\n\npkg2/stable 2.0 amd64 [upgradable]\n"
	if n := UpgradableCount(out); n != 2 {
		t.Errorf("got %d", n)
	}
}

func TestParseBeforeAfter(t *testing.T) {
	k, n := parseBeforeAfter("6.8.12-4-pve\n---APT---\n12\n")
	if k != "6.8.12-4-pve" || n != 12 {
		t.Errorf("got %q %d", k, n)
	}
	// Shell pollué (starship sur stderr fusionnée) : motif version quand même trouvé.
	k, _ = parseBeforeAfter("/root/.bashrc: line 19: starship: command not found\n6.8.12-4-pve\n---APT---\n84\n")
	if k != "6.8.12-4-pve" {
		t.Errorf("got %q", k)
	}
}

func TestSelectKernelsToPurge(t *testing.T) {
	installed := []string{
		"pve-kernel-6.8",
		"pve-kernel-6.8.12-4-pve",
		"pve-kernel-6.8.12-5-pve",
		"pve-kernel-7.0.14-17-pve",
	}
	// Booté = 7.0.14, keep=2 → purge le 6.8.12-4, jamais le booté ni le meta.
	purge := SelectKernelsToPurge(installed, "7.0.14-17-pve", 2)
	if len(purge) != 1 || purge[0] != "pve-kernel-6.8.12-4-pve" {
		t.Errorf("purge = %v", purge)
	}
	// Booté = vieux kernel → il survit malgré keep=1.
	purge = SelectKernelsToPurge(installed, "6.8.12-4-pve", 1)
	for _, p := range purge {
		if strings.Contains(p, "6.8.12-4-pve") {
			t.Errorf("le kernel booté ne doit jamais être purgé : %v", purge)
		}
	}
	if len(purge) == 0 {
		t.Error("purge vide inattendue")
	}
}

func TestKernelPackages(t *testing.T) {
	out := "ii  pve-kernel-6.8.12-4-pve amd64 stuff\nii  pve-kernel-6.8 amd64 meta\nii  pve-headers-6.8.12-4-pve amd64 h\n" +
		"ii  proxmox-kernel-7.0 amd64 meta\nii  proxmox-kernel-7.0.14-17-pve-signed amd64 x\nii  proxmox-kernel-7.0.14-19-pve-signed amd64 x\nii  proxmox-kernel-helper amd64 tool\n"
	pkgs := KernelPackages(out)
	if len(pkgs) != 6 {
		t.Errorf("got %v", pkgs)
	}
	// PVE 9 : keep=2 → purge du plus vieux, jamais le booté ni les metas.
	purge := SelectKernelsToPurge(pkgs, "7.0.14-19-pve", 2)
	if len(purge) != 1 || purge[0] != "pve-kernel-6.8.12-4-pve" {
		t.Errorf("purge = %v", purge)
	}
}

func TestNewestBootImage(t *testing.T) {
	run := func(_ context.Context, host, cmd string) (string, error) {
		return "vmlinuz-7.0.14-19-pve\n", nil
	}
	if got := newestBootImage(context.Background(), run, "h"); got != "7.0.14-19-pve" {
		t.Errorf("got %q", got)
	}
}

func TestTopgradeBase(t *testing.T) {
	if s := topgradeBase(nil); !strings.Contains(s, "--allow-root") {
		t.Errorf("moderne attendu : %q", s)
	}
	if s := topgradeBase(errors.New("vieux")); strings.Contains(s, "--allow-root") {
		t.Errorf("ancien attendu sans flag : %q", s)
	}
}

func TestBackupGuestVerify(t *testing.T) {
	okOut := "INFO: Starting Backup\nINFO: Backup Volume: local:backup/vzdump-lxc-100-xyz.zst\nINFO: Finished Backup\n"
	run := func(_ context.Context, host, cmd string) (string, error) {
		if strings.HasPrefix(cmd, "vzdump") {
			return okOut, nil
		}
		return "", errors.New("x")
	}
	id, err := BackupGuest(context.Background(), run, "h", inventory.Guest{VMID: 100, Kind: "lxc", Name: "t", Node: "Janus"}, Options{PBSStorage: "pbs"}, "r1")
	if err != nil || id == "" {
		t.Errorf("id=%q err=%v", id, err)
	}
	runFail := func(_ context.Context, host, cmd string) (string, error) { return "INFO: boom", nil }
	if _, err := BackupGuest(context.Background(), runFail, "h", inventory.Guest{VMID: 100, Kind: "lxc", Name: "t", Node: "Janus"}, Options{PBSStorage: "pbs"}, "r1"); err == nil {
		t.Error("fail-closed attendu sans marqueur Finished Backup")
	}
}
