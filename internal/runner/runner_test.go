package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/lamacheref/pve-update-orchestrator/internal/config"
)

func fakeOK(failNode string) Runner {
	return func(_ context.Context, host, cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "pvecm status"):
			return "Quorate:          Yes\nNodes:            1\n", nil
		case strings.Contains(cmd, "systemctl --failed"):
			if strings.Contains(cmd, "pct exec") {
				return "0 loaded units listed.\n", nil
			}
			if host == failNode {
				return "● foo.service loaded failed failed X\n", nil
			}
			return "0 loaded units listed.\n", nil
		case strings.HasPrefix(cmd, "journalctl"), strings.HasPrefix(cmd, "dmesg"):
			return "", nil
		case strings.HasPrefix(cmd, "df -P"):
			return "/dev/sda1 1 2 3 23% /\n", nil
		case strings.HasPrefix(cmd, "pveversion"):
			return "pve-manager/9.0.1/x (running kernel: 6.8.12-4-pve)\n", nil
		case strings.Contains(cmd, "---APT---") || cmd == "uname -r":
			return "6.8.12-4-pve\n---APT---\n0\n", nil
		case cmd == "pct list":
			if host == "10.0.0.2" {
				return "VMID       Status     Lock         Name\n101        running                 lxc2\n", nil
			}
			return "VMID       Status     Lock         Name\n100        running                 lxc1\n", nil
		case cmd == "qm list":
			return "VMID NAME STATUS MEM\n", nil
		case strings.HasPrefix(cmd, "vzdump"):
			return "INFO: Backup Volume: pbs:backup/vzdump-lxc-100-x.zst\nINFO: Finished Backup\n", nil
		case strings.Contains(cmd, "dpkg -l"):
			return "ii  pve-kernel-6.8.12-4-pve amd64 x\nii  pve-kernel-6.8 amd64 meta\n", nil
		case strings.Contains(cmd, "topgrade"), strings.Contains(cmd, "apt-get"), strings.Contains(cmd, "proxmox-boot-tool"):
			return "", nil
		case strings.Contains(cmd, "docker"):
			return "", nil
		case cmd == "true" || cmd == "hostname":
			return "ok\n", nil
		}
		return "", nil
	}
}

func testNodes() []config.Node {
	return []config.Node{
		{Name: "Zeus", IP: "10.0.0.2", Role: "standard"},
		{Name: "Janus", IP: "10.0.0.1", Role: "canary"},
	}
}

func TestRunAllOK(t *testing.T) {
	dir := t.TempDir()
	st, err := Run(context.Background(), fakeOK(""), testNodes(), Options{
		RunID: "t1", Workers: 2, PBSStorage: "pbs", KeepKernels: 2, StateDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 2 nodes + 2 guests identiques (même fake pour les 2 nodes) → au moins 4 cibles.
	if len(st.Targets) < 4 {
		t.Fatalf("%d cibles : %+v", len(st.Targets), st.Targets)
	}
	for k, v := range st.Targets {
		if !v.OK {
			t.Errorf("%s en échec : %+v", k, v.Result)
		}
	}
	if st.Targets["node/Janus"].Result.BackupID != "" {
		t.Error("un node ne doit pas avoir de backup")
	}
}

func TestRunHoldAndResume(t *testing.T) {
	dir := t.TempDir()
	st, err := Run(context.Background(), fakeOK("10.0.0.2"), testNodes(), Options{
		RunID: "t2", Workers: 2, PBSStorage: "pbs", KeepKernels: 2, StateDir: dir, CanaryFirst: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	zeus := st.Targets["node/Zeus"]
	if zeus.OK || !zeus.Hold {
		t.Errorf("Zeus doit être en HOLD : %+v", zeus.Result)
	}
	// Reprise : Janus déjà OK → skippé, Zeus rejoué (toujours HOLD).
	st2, err := Run(context.Background(), fakeOK(""), testNodes(), Options{
		Workers: 2, PBSStorage: "pbs", KeepKernels: 2, StateDir: dir, Resume: "t2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st2.RunID != "t2" {
		t.Errorf("run-id repris attendu, got %s", st2.RunID)
	}
}

func TestOrderCanary(t *testing.T) {
	got := orderNodes(testNodes(), true)
	if got[0].Name != "Janus" {
		t.Errorf("canary d'abord : %+v", got)
	}
	got = orderNodes(testNodes(), false)
	if got[0].Name != "Zeus" {
		t.Errorf("ordre fichier : %+v", got)
	}
}
