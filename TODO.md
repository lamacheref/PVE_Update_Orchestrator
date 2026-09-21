# ✅ TODO — PVE Update Orchestrator

> Suivi opérationnel. Détail des lots : [ROADMAP.md](ROADMAP.md). Cocher au fil de l'eau, chaque item coché = commit.

## 📚 Socle docs & outillage

- [x] PROJET.md — spec logiciel Go + SSH mutualisé
- [x] README.md / TODO.md / CHANGELOG.md / ROADMAP.md
- [x] VERSION `0.0.1` + `scripts/bump.sh` (M manuel, m/f auto)
- [x] CI GitHub → artefacts vers Gitea
- [x] Releases vérifiées des deux côtés (GitHub + Gitea, binaires `linux-amd64/arm64` + `SHA256SUMS.txt`) 📦
- [x] Webhook Discord : rotation refusée (canal privé verrouillé, risque assumé — voir PROJET.md §8) ; reste à l'externaliser dans `/etc/pve-orchestrator/.env` (mode 600) au Lot 1 🔒

## 🌱 Lot 0 — Socle (~1 sem) → v0.2.0

- [x] Squelette Go compilable (`main.go`, `internal/version`)
- [x] `internal/config` (YAML + `.env`, jamais de secrets en git)
- [x] `bootstrap-ssh` (clé dédiée via seed, `known_hosts`, rotation/révocation)
- [x] `inventory sync` réel sur les 7 nodes (code + tests fixtures)
- [x] `cluster discover` (pvecm nodes + getent, match insensible à la casse, `--sync` vers nodes.yaml) + `bootstrap-ssh --reconcile/--rotate/--revoke` (convergence et purge autonomes) 🤖
- [x] Run réel `bootstrap-ssh --yes` + `inventory sync` : Janus ✅ (quorum, 3 guests, kernel `7.0.14-17-pve`) — 6 nodes restants
- [x] CI verte des deux côtés (`build/vet/test` + releases) ✅

## 🚀 Lot 1 — MVP (~1-2 sem) → v0.3.0

- [x] `runner` (rolling 1 node, worker-pool guests, resume, verrou)
- [x] `health` (pré/post-checks — HOLD réel validé sur Janus : 2 unités tierces en échec)
- [x] `update` node + guest (topgrade, autoremove, kernel, kernel-clean natif, backup PBS fail-closed)
- [x] `discord` (start + embed/cible + synthèse, retry, idempotent)
- [x] `serve` lecture seule + dashboard (runs, détail, polling 5s)
- [x] Run dry-run complet 7 nodes : 29 OK / 1 FAIL (HOLD Janus connu) / 3 HOLD-skipped — 33 cibles, agents QEMU OK 🔍
- [x] Assainissement flotte : checkmk éradiqué partout (nodes Aphrodite/Loki, CT104/108, VM120 — scripts maintainer neutrés, user conservé sur CT104 car UID partagé avec postgres docker) ; `datacenter.cfg` re-encodé UTF-8 (é latin-1 → fini l'erreur vzdump) ; CIFS sftp en `x-systemd.automount` 🧹
- [ ] Canary Janus + 1 guest `--apply --yes` puis réel 🐤
- [ ] Webhook Discord dans `/etc/pve-orchestrator/.env` + premier run notifié 🔔

## 💪 Lot 2 — Durcissement (~1-2 sem) → v0.3.0+

- [ ] Reboot auto + resume, kernel-clean natif
- [ ] Backup PBS snapshot + rollback testé (1 LXC + 1 VM poubelle 🗑️)
- [ ] Timer systemd + prune auto
- [ ] Run hebdo supervisé ~30 min ☕

## 🎨 Lot 3 — Docker + UI (~1 sem) → v1.0.0 (promotion manuelle 🔴)

- [ ] Module docker + healthchecks applicatifs
- [ ] Boutons `retry/rollback` authentifiés + audit
- [ ] Runbook incident
- [ ] Promotion `CONFIRM_MAJOR=yes scripts/bump.sh major` → `v1.0.0` 🎉
