// Package bootstrap génère la clé SSH dédiée et la déploie sur les nodes
// via la clé seed (qui ne sert plus jamais ensuite).
package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/lamacheref/pve-update-orchestrator/internal/sshpool"
)

// Runner exécute une commande sur un node (ex. méthode *sshpool.Pool.Run).
type Runner func(ctx context.Context, host, cmd string) (string, error)

// GenerateKey crée une ed25519 à path (+ .pub), ou relit l'existante.
// Retourne la ligne authorized_keys et created=vrai si nouvelle.
func GenerateKey(path string) (pubLine string, created bool, err error) {
	path = sshpool.ExpandPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	if raw, rerr := os.ReadFile(path); rerr == nil {
		signer, perr := ssh.ParsePrivateKey(raw)
		if perr != nil {
			return "", false, fmt.Errorf("clé %s existante illisible : %w", path, perr)
		}
		return CanonicalLine(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), false, nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", false, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", false, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", false, err
	}
	pubLine = CanonicalLine(string(ssh.MarshalAuthorizedKey(sshPub)))
	if err := os.WriteFile(path+".pub", []byte(pubLine+"\n"), 0o644); err != nil {
		return "", false, err
	}
	return pubLine, true, nil
}

// KeyComment marque les lignes gérées par le projet.
const KeyComment = "pve-orchestrator"

// CanonicalLine normalise une ligne authorized_keys (ajoute le commentaire
// projet si absent) pour repérer les clés gérées.
func CanonicalLine(pubLine string) string {
	f := strings.Fields(strings.TrimSpace(pubLine))
	if len(f) < 2 {
		return strings.TrimSpace(pubLine)
	}
	if len(f) >= 3 {
		return strings.Join(f, " ")
	}
	return f[0] + " " + f[1] + " " + KeyComment
}

// KeyData rend le matériau de clé (2e champ), base de matching pour
// Ensure/Revoke : insensible au commentaire, robuste aux rotations.
func KeyData(pubLine string) (string, error) {
	f := strings.Fields(strings.TrimSpace(pubLine))
	if len(f) < 2 {
		return "", fmt.Errorf("ligne de clé invalide")
	}
	return f[1], nil
}

// Fingerprint rend l'empreinte SHA256 d'une ligne authorized_keys.
func Fingerprint(pubLine string) (string, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubLine))
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(key), nil
}

// EnsureAuthorizedKey converge /root/.authorized_keys vers la ligne canonique :
// supprime tout doublon/variante du même matériau puis ajoute la ligne
// canonique (idempotent, auto-répare les lignes sans commentaire).
func EnsureAuthorizedKey(ctx context.Context, run Runner, host, pubLine string) error {
	canon := CanonicalLine(pubLine)
	data, err := KeyData(canon)
	if err != nil {
		return err
	}
	script := fmt.Sprintf(
		`F=/root/.ssh/authorized_keys; mkdir -p /root/.ssh && chmod 700 /root/.ssh && touch $F && `+
			`grep -v -F %[1]q $F > $F.tmp || true; cat $F.tmp > $F; rm -f $F.tmp; `+
			`echo %[2]q >> $F; chmod 600 $F && echo ENSURED-OK`,
		data, canon,
	)
	out, err := run(ctx, host, script)
	if err != nil {
		return fmt.Errorf("déploiement clé sur %s : %w", host, err)
	}
	if !strings.Contains(out, "ENSURED-OK") {
		return fmt.Errorf("déploiement clé sur %s : sortie inattendue %q", host, out)
	}
	return nil
}

// RevokeAuthorizedKey purge toutes les lignes du matériau de clé (anciennes
// générations, clés révoquées). Rend le nombre de lignes supprimées.
func RevokeAuthorizedKey(ctx context.Context, run Runner, host, pubLine string) (int, error) {
	data, err := KeyData(pubLine)
	if err != nil {
		return 0, err
	}
	script := fmt.Sprintf(
		`F=/root/.ssh/authorized_keys; touch $F; BEFORE=$(wc -l < $F); `+
			`grep -v -F %[1]q $F > $F.tmp || true; cat $F.tmp > $F; rm -f $F.tmp; `+
			`chmod 600 $F; echo "REVOKED-$((BEFORE - $(wc -l < $F)))"`,
		data,
	)
	out, err := run(ctx, host, script)
	if err != nil {
		return 0, fmt.Errorf("révocation clé sur %s : %w", host, err)
	}
	var n int
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "REVOKED-") {
			fmt.Sscanf(f, "REVOKED-%d", &n)
		}
	}
	if !strings.Contains(out, "REVOKED-") {
		return 0, fmt.Errorf("révocation clé sur %s : sortie inattendue %q", host, out)
	}
	return n, nil
}

// RotateKeyFiles archive la clé courante en .prev et en génère une fraîche.
// Retourne (nouvellePub, anciennePub, nettoyage, erreur). Le nettoyage
// supprime les .prev après révocation réussie de l'ancienne.
func RotateKeyFiles(path string) (newPub, prevPub string, cleanup func(), err error) {
	path = sshpool.ExpandPath(path)
	curPub, created, err := GenerateKey(path)
	if err != nil {
		return "", "", nil, err
	}
	if created {
		return curPub, "", func() {}, nil // pas de rotation : première génération
	}
	for _, ext := range []string{"", ".pub"} {
		if err := os.Rename(path+ext, path+".prev"+ext); err != nil {
			return "", "", nil, fmt.Errorf("archive %s : %w", path+ext, err)
		}
	}
	newPub, _, err = GenerateKey(path)
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() {
		os.Remove(path + ".prev")
		os.Remove(path + ".prev.pub")
	}
	return newPub, curPub, cleanup, nil
}
