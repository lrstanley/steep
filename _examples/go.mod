module getsteep.dev/steep/_examples

go 1.27.3

// Examples build against the copies of steep and reel in this repository.
replace (
	getsteep.dev/steep => ../
	getsteep.dev/steep/reel => ../reel
)

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.1.1
	charm.land/lipgloss/v2 v2.0.6
	github.com/charmbracelet/harmonica v0.2.0
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/lrstanley/clix/v2 v2.0.7
	github.com/lrstanley/go-nf v0.0.0-20261103002006-052bb0b267f6
	getsteep.dev/steep v0.0.0
	getsteep.dev/steep/reel v0.0.0
)

require (
	github.com/alecthomas/kong v1.17.0 // indirect
	github.com/aymanbagabas/go-udiff v0.4.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261023060817-a45b7c86333f // indirect
	github.com/charmbracelet/x/conpty v0.2.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/charmbracelet/x/xpty v0.1.4 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/lmittmann/tint v1.2.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v1.3.1 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
