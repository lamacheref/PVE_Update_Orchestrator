package discord

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/update"
)

func TestTargetEmbed(t *testing.T) {
	ok := update.Result{Target: "node/Janus", Kind: "node", OK: true, PkgsBefore: 5, PkgsAfter: 0,
		KernelBefore: "6.8.12-4-pve", KernelAfter: "6.8.12-5-pve", Rebooted: true, Duration: "3m"}
	e := TargetEmbed(ok)
	if e.Color != ColorSuccess || !strings.HasPrefix(e.Title, "🟢") {
		t.Errorf("embed ok : %+v", e)
	}
	ko := update.Result{Target: "lxc/100@x", Kind: "lxc", Error: "boom", BackupID: "snap-1"}
	e = TargetEmbed(ko)
	if e.Color != ColorFail || len(e.Fields) == 0 {
		t.Errorf("embed ko : %+v", e)
	}
}

func TestSummary(t *testing.T) {
	m := SummaryMessage("r1", []update.Result{
		{Target: "a", OK: true},
		{Target: "b", Error: "x", BackupID: "s"},
	}, time.Minute)
	if len(m.Embeds) != 1 || m.Embeds[0].Color != ColorFail {
		t.Errorf("summary : %+v", m)
	}
}

func TestSendRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var m Message
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			t.Error(err)
		}
		if calls < 2 {
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	if err := Send(srv.Client(), srv.URL, StartMessage("r", "all", true)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("retry attendu, %d appels", calls)
	}
}
