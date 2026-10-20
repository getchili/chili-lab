# Chili Lab

Application de bureau Windows pour programmer des ESP32 en Chili (langage : dépôt voisin `..\chili`, voir son CLAUDE.md).

- Go, module `github.com/getchili/chili-lab`, importe `github.com/getchili/chili/compiler` (device, codegen, firmware) via `replace => ../chili/compiler` tant que le dépôt Chili n'est pas publié.
- Exe sans console (`-ldflags -H=windowsgui`), icône via go-winres (`rsrc_windows_amd64.syso`). Erreurs de démarrage : MessageBox (`alert_windows.go`).
- `internal/lab` : serveur HTTP 127.0.0.1 + jeton, SSE, interface `static/index.html` (sans dépendance, hors ligne) ouverte dans Edge/Chrome `--app` avec profil dédié. Instance unique via `%APPDATA%\Chili Lab\instance.txt` (contient l'URL ; `--dev` n'ouvre pas de fenêtre, lire l'URL dans ce fichier pour tester).
- Réglages `%APPDATA%\Chili Lab\config.json` ; dossier des programmes par défaut `Documents\Chili`, rempli avec les exemples au premier lancement.
- Build : `build.ps1 [-Installer]` → `Chili Lab.exe`, `dist\ChiliLabSetup-x.y.z.exe` (Inno Setup, par utilisateur).
- Terminal de l'utilisateur : Windows PowerShell 5.1 (pas de `&&`). Communication directe, en français.
- Accès téléphone (internal/lab/phone.go) : à la demande, 2e écoute sur l IP LAN du PC (port aléatoire, même jeton), QR code PNG (skip2/go-qrcode). Coupé à la fermeture. Interface responsive (<900 px : barre du haut, pas de barre latérale).
