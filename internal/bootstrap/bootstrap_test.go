package bootstrap

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalKeyData(t *testing.T) {
	bare := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest"
	if got := CanonicalLine(bare); got != bare+" "+KeyComment {
		t.Errorf("canonical = %q", got)
	}
	if got := CanonicalLine(bare + " " + KeyComment); got != bare+" "+KeyComment {
		t.Errorf("canonical idempotent = %q", got)
	}
	data, err := KeyData(bare + " autre-commentaire")
	if err != nil || data != "AAAAC3NzaC1lZDI1NTE5AAAAItest" {
		t.Errorf("keydata = %q %v", data, err)
	}
	if _, err := KeyData("invalide"); err == nil {
		t.Error("erreur attendue sur ligne invalide")
	}
}

func TestGenerateKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test_ed25519")
	pub1, created, err := GenerateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !strings.HasPrefix(pub1, "ssh-ed25519 ") || !strings.HasSuffix(pub1, " "+KeyComment) {
		t.Fatalf("clé inattendue : created=%v pub=%q", created, pub1)
	}
	pub2, created, err := GenerateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if created || pub1 != pub2 {
		t.Error("la 2e génération doit relire la clé existante")
	}
	if fp, err := Fingerprint(pub1); err != nil || !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("empreinte inattendue : %q %v", fp, err)
	}
}
