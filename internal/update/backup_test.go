package update

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
)

const tasksSample = `[
 {"upid": "UPID:janus:00012345:abcd:1234:vzdump:100:root@", "type": "vzdump", "id": "100", "status": "running"},
 {"upid": "UPID:janus:00011111:abcd:1230:vzdump:101:root@", "type": "vzdump", "id": "101", "status": "OK"},
 {"upid": "UPID:janus:00012222:abcd:1231:qmreboot:102:root@", "type": "qmreboot", "id": "102", "status": "running"}
]`

func TestFindRunningVzdump(t *testing.T) {
	if _, ok := findRunningVzdump("pas du json", 100); ok {
		t.Error("json invalide → false attendu")
	}
	if upid, ok := findRunningVzdump(tasksSample, 100); !ok || !strings.Contains(upid, "00012345") {
		t.Errorf("vzdump 100 actif attendu : %q %v", upid, ok)
	}
	if _, ok := findRunningVzdump(tasksSample, 101); ok {
		t.Error("vzdump 101 terminé → false attendu")
	}
	if _, ok := findRunningVzdump(tasksSample, 102); ok {
		t.Error("qmreboot ≠ vzdump → false attendu")
	}
	if _, ok := findRunningVzdump(tasksSample, 999); ok {
		t.Error("vmid inconnu → false attendu")
	}
}

func TestPBSOnline(t *testing.T) {
	pvesm := "Name             Type     Status\npbs              pbs      active     1 2 3\nlocal            dir      active     1 2 3\ndown             pbs      disabled   1 2 3\n"
	run := func(_ context.Context, host, cmd string) (string, error) { return pvesm, nil }
	if ok, _ := PBSOnline(context.Background(), run, "h", "pbs"); !ok {
		t.Error("pbs actif attendu")
	}
	if ok, info := PBSOnline(context.Background(), run, "h", "down"); ok {
		t.Errorf("down inactif attendu : %s", info)
	}
	if ok, info := PBSOnline(context.Background(), run, "h", "nope"); ok {
		t.Errorf("absent attendu : %s", info)
	}
}

func TestWaitBackupClear(t *testing.T) {
	nobody := func(_ context.Context, host, cmd string) (string, error) { return "[]", nil }
	if err := waitBackupClear(context.Background(), nobody, "Janus", "h", 100, "t", time.Minute); err != nil {
		t.Errorf("libre immédiatement : %v", err)
	}
	if err := waitBackupClear(context.Background(), nobody, "Janus", "h", 100, "t", 0); err == nil {
		t.Error("wait<=0 → report immédiat attendu")
	}
}

func TestBusySkip(t *testing.T) {
	run := func(_ context.Context, host, cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "pvesm status"):
			return "Name Type Status\npbs pbs active 1 2 3\n", nil
		case strings.Contains(cmd, "/tasks"):
			return `[{"upid":"UPID:janus:0001:x:1:vzdump:100:root@","type":"vzdump","id":"100","status":"running"}]`, nil
		case strings.Contains(cmd, "ps -eo args"):
			return "root 1 vzdump 100 --mode snapshot\n", nil
		}
		return "", nil
	}
	g := inventory.Guest{VMID: 100, Kind: "lxc", Name: "x", Node: "Janus"}
	r := UpdateGuest(context.Background(), run, "h", g,
		Options{PBSStorage: "pbs", BackupWait: time.Nanosecond}, "r1")
	if !r.Skipped || !r.OK {
		t.Errorf("skip attendu (busy) : %+v", r)
	}
}
