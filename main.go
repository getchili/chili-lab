// Chili Lab : l'application de bureau pour programmer ses cartes ESP32 en Chili.
//
// Compilée en application fenêtrée (pas de console) : voir build.ps1.
//
//	Chili Lab.exe            détecte la carte toute seule
//	Chili Lab.exe COM4       ou une carte précise (port, adresse IP)
//	Chili Lab.exe --dev      affiche l'adresse au lieu d'ouvrir la fenêtre
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/getchili/chili-lab/internal/lab"
)

const version = "0.1.0"

func main() {
	opts := lab.Options{Version: version, Examples: findExamples()}
	for _, a := range os.Args[1:] {
		if a == "--dev" {
			opts.NoWindow = true
		} else {
			opts.Target = a
		}
	}
	if err := lab.Run(opts); err != nil {
		alert("Chili Lab", fmt.Sprintf("Chili Lab n'a pas pu démarrer :\n\n%v", err))
		os.Exit(1)
	}
}

// findExamples cherche les exemples : à côté de l'application, sinon ceux
// installés avec Chili.
func findExamples() string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "examples"))
	}
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		dirs = append(dirs, filepath.Join(base, "Programs", "Chili", "examples"))
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	return ""
}
