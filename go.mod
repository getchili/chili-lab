module github.com/getchili/chili-lab

go 1.22

require (
	github.com/getchili/chili/compiler v0.0.0
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	go.bug.st/serial v1.6.2 // indirect
	golang.org/x/sys v0.0.0-20220829200755-d48e67d00261 // indirect
)

// Tant que github.com/getchili/chili n'est pas publié, on utilise le dépôt
// voisin (../chili). À retirer une fois le dépôt en ligne et tagué.
replace github.com/getchili/chili/compiler => ../chili/compiler
