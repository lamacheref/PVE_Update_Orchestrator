# 🚀 PVE Update Orchestrator

> 🖥️➡️✨ Fini les 4h de MAJ manuelle : **un seul binaire Go** pilote 7 nodes Proxmox, LXC, VM & Docker — avec rapports Discord et dashboard intégré.

[![CI](https://github.com/lamacheref/PVE_Update_Orchestrator/actions/workflows/ci.yml/badge.svg)](https://github.com/lamacheref/PVE_Update_Orchestrator/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white)
![Proxmox](https://img.shields.io/badge/Proxmox_VE-8.x-E57000?style=flat-square&logo=proxmox&logoColor=white)
![SQLite](https://img.shields.io/badge/SQLite-embedded-003B57?style=flat-square&logo=sqlite&logoColor=white)
![Discord](https://img.shields.io/badge/Discord-bot--alarm-5865F2?style=flat-square&logo=discord&logoColor=white)

📖 Spec complète : [PROJET.md](PROJET.md) • 🗺️ [ROADMAP.md](ROADMAP.md) • ✅ [TODO.md](TODO.md) • 🧾 [CHANGELOG.md](CHANGELOG.md)

---

## ⚡ Démarrage rapide

```bash
go build -ldflags "-X main.version=$(cat VERSION)" -o pve-orchestrator .
./pve-orchestrator --version   # pve-orchestrator 0.0.1
```

| 📄 Doc | 🎯 Contenu |
|---|---|
| [PROJET.md](PROJET.md) | Spécification : archi Go, pool SSH, workflow, Discord, dashboard |
| [ROADMAP.md](ROADMAP.md) | Lots 0→3, jalons de versions, promotion v1.0.0 |
| [TODO.md](TODO.md) | Suivi des tâches (cases à cocher) |
| [CHANGELOG.md](CHANGELOG.md) | Historique des versions (Keep a Changelog) |
| [VERSION](VERSION) | Version courante `M.m.f` (source de vérité) |

---

## 🔖 Versionning M.m.f

Format `MAJEUR.mineur.fix` dans [VERSION](VERSION), tags git `vM.m.f`.

| Bump | 📌 Règle | 🔧 Commande |
|---|---|---|
| 🔴 `M` majeur (breaking) | **Utilisateur uniquement** — jamais en CI | `CONFIRM_MAJOR=yes scripts/bump.sh major` |
| 🟡 `m` mineur (features) | 🤖 **Automatisé** : CI détecte `feat:` depuis le dernier tag | `scripts/bump.sh minor` (local) |
| 🟢 `f` fix (tout le reste) | 🤖 **Automatisé** : défaut CI | `scripts/bump.sh fix` (local) |

Conventions de commits : `feat:` → bump `m`, sinon bump `f`. Le bump `M` exige confirmation explicite et reste un geste volontaire (ex. passage en v1.0.0, voir [ROADMAP.md](ROADMAP.md)).

---

## 🏗️ CI : GitHub compile → Gitea reçoit

`[.github/workflows/ci.yml](.github/workflows/ci.yml)` sur push `main` :

1. 🔖 **version** — `scripts/bump.sh auto`, entrée CHANGELOG, commit `chore(release): vX.Y.Z` + tag (ignoré si le commit est déjà une release → pas de boucle ♾️)
2. 🏗️ **build** — `go vet`, `go test`, binaires `linux/amd64` + `linux/arm64` + `SHA256SUMS.txt` (artefacts GitHub)
3. 📦 **gitea-release** — crée la release sur Gitea et y téléverse les binaires

🔑 Secrets GitHub requis :

| Secret | 📝 Valeur |
|---|---|
| `GITEA_URL` | URL HTTP(S) du Gitea (ex. `https://gitea.smiden.eu`) |
| `GITEA_TOKEN` | Token utilisateur Gitea (droits `write:repository`) |

---

## 🌐 Dépôts synchronisés

| 🏷️ Remote | 🔗 URL | 🎯 Rôle |
|---|---|---|
| `origin` | `ssh://gitea@gitea.smiden.eu:2222/flamachere/PVE_Update_Orchestrator.git` | 🥇 Principal (Gitea) |
| `github` | `git@github.com:lamacheref/PVE_Update_Orchestrator.git` | 🪞 Miroir + CI (GitHub) |
| `all` | les deux (push groupé) | 🔄 Synchro |

```bash
git push all main --tags   # pousse Gitea + GitHub en une commande
```

---

## 📊 Statut

🌱 **Lot 0 — socle en cours** : squelette Go compilable, versionning, CI. Orchestration réelle (runner, SSH, Discord) : voir [TODO.md](TODO.md).

*Usage privé — licence à définir.*
