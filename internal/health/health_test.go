package health

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func TestFailedUnits(t *testing.T) {
	n, _ := FailedUnits("0 loaded units listed.\n")
	if n != 0 {
		t.Errorf("attendu 0, got %d", n)
	}
	n, bad := FailedUnits("UNIT LOAD ACTIVE SUB DESCRIPTION\n● nginx.service loaded failed failed Foo\n")
	if n != 1 || len(bad) != 1 {
		t.Errorf("attendu 1 : %d %+v", n, bad)
	}
}

func TestDiskUsePct(t *testing.T) {
	if got := DiskUsePct("Filesystem 1K-blocks Used Available Use% Mounted on\n/dev/sda1 1 2 3 23% /\n"); got != 23 {
		t.Errorf("got %d", got)
	}
	if got := DiskUsePct("vide"); got != -1 {
		t.Errorf("got %d", got)
	}
}

func TestQuorate(t *testing.T) {
	if !Quorate("Quorate:          Yes\n") || !Quorate("Quorum: Yes\n") {
		t.Error("quorum Yes attendu")
	}
	if Quorate("Quorate:          No\n") {
		t.Error("quorum No attendu")
	}
}

func TestParseGuestExec(t *testing.T) {
	// out-data base64 de "0 loaded units listed.\n"
	sample := `{"exitcode" : 0, "exited" : 1, "out-data" : "` + base64.StdEncoding.EncodeToString([]byte("0 loaded units listed.\n")) + `"}`
	code, data := ParseGuestExec(sample)
	if code != 0 || data != "0 loaded units listed." {
		t.Errorf("code=%d data=%q", code, data)
	}
	code, _ = ParseGuestExec(`{"exitcode" : 1, "exited" : 1}`)
	if code != 1 {
		t.Errorf("code=%d", code)
	}
}

func TestCheckNodeFake(t *testing.T) {
	run := func(_ context.Context, host, cmd string) (string, error) {
		switch {
		case cmd == "pvecm status":
			return "Quorate:          Yes\nNodes:            7\n", nil
		case cmd == "systemctl --failed --no-pager":
			return "0 loaded units listed.\n", nil
		case len(cmd) >= 10 && cmd[:10] == "journalctl":
			return "", nil
		case len(cmd) >= 6 && cmd[:6] == "dmesg ":
			return "", nil
		case cmd == "df -P / | tail -1":
			return "/dev/sda1 1 2 3 23% /\n", nil
		case cmd == "pveversion | head -1":
			return "pve-manager/9.0.1/xxxx (running kernel: 7.0.14-17-pve)\n", nil
		}
		return "", errors.New("cmd inconnue: " + cmd)
	}
	checks := CheckNode(context.Background(), run, "h")
	if len(checks) != 6 {
		t.Fatalf("%d checks", len(checks))
	}
	if Critical(checks) {
		t.Errorf("aucun critique attendu : %+v", checks)
	}
}
