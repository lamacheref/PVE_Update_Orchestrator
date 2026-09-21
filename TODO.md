# ✅ TODO — PVE Update Orchestrator

> Suivi opérationnel. Détail des lots : [ROADMAP.md](ROADMAP.md). Cocher au fil de l'eau, chaque item coché = commit.

## 📚 Socle docs & outillage

- [x] PROJET.md — spec logiciel Go + SSH mutualisé
- [x] README.md / TODO.md / CHANGELOG.md / ROADMAP.md
- [x] VERSION `0.0.1` + `scripts/bump.sh` (M manuel, m/f auto)
- [x] CI GitHub → artefacts vers Gitea
- [ ] Releases vérifiées des deux côtés (GitHub + Gitea, binaires `linux-amd64/arm64` + `SHA256SUMS.txt`) 📦
- [ ] Régénérer le webhook Discord exposé + `.env` (mode 600) 🔒

## 🌱 Lot 0 — Socle (~1 sem) → v0.2.0

- [x] Squelette Go compilable (`main.go`, `internal/version`)
- [x] `internal/config` (YAML + `.env`, jamais de secrets en git)
- [x] `bootstrap-ssh` (clé dédiée via seed, `known_hosts`, rotation/révocation)
- [x] `inventory sync` réel sur les 7 nodes (code + tests fixtures)
- [ ] Run réel `bootstrap-ssh --yes` + `inventory sync` sur le cluster (touche la prod — à confirmer)
- [ ] CI verte (`build/vet/test`) ✅

## 🚀 Lot 1 — MVP (~1-2 sem) → v0.3.0

- [ ] `runner` (rolling 1 node, worker-pool guests, resume, verrou)
- [ ] `health` (pré/post-checks)
- [ ] `update` node + guest (topgrade, autoremove, kernel)
- [ ] `discord` (start + embed/cible + synthèse)
- [ ] `serve` lecture seule + dashboard carte/timeline
- [ ] Canary Janus + 1 guest `--dry-run` puis réel 🐤

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
