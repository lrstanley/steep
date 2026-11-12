#!/usr/bin/env bash
# Records what the CLI pages show with the reel CLI itself: a tape of the tea
# shop, exported with one flag changed at a time, and the output of its commands.
# Run by "task docs:demos", after the Go demos.
set -euo pipefail

docs="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
demos="$docs/public/demos"
fixtures="$docs/fixtures"
bin="$(mktemp -d)"
# Keeps the demo's session apart from any the user has running.
export REEL_RUNTIME_DIR="$bin/run"
unset REEL_SESSION
trap 'reel s stop >/dev/null 2>&1 || true; rm -rf "$bin"' EXIT

go -C "$docs/../cmd/reel" build -o "$bin/reel" .
go -C "$docs/../_examples" build -o "$bin/teashop" ./docs-demos
PATH="$bin:$PATH"
cd "$bin"

# Twice the default size of 14.67px, and the frame, as the Go demos draw theirs.
size=(--font-size 29.33)
frame=("${size[@]}" --chrome --padding 20 --margin 32 --margin-fill "#0b150f" --radius 20)

cast="$docs/public/casts/cli-tour.cast"
reel run "$docs/tapes/tour.tape" -o "$cast" -o "$demos/cli-tape.webp" "${frame[@]}"

# removes the paths of this temporary directory, and of this checkout.
tidy() {
    sed -e "s#$bin/##g" -e "s#$docs/##g"
}

# runs each line of stdin, keeping it and its output as a console transcript.
transcript() {
    local name="$1" line
    while IFS= read -r line; do
        echo "\$ $line"
        eval "$line" 2>&1
    done | tidy >"$fixtures/$name.output"
}

# exports the tour, stopping before the shop quits, with a flag or more added.
export_demo() {
    local name="$1"
    shift
    reel export "$cast" --no-cast-effects --between ,brew:done "${size[@]}" -o "$demos/$name" "$@"
}

export_demo cli-chrome-on.png --chrome
export_demo cli-chrome-off.png --no-chrome
export_demo cli-padding.png --chrome --padding 48 --margin 48 --margin-fill "#0b150f" --radius 24
export_demo cli-gradient.png --chrome --padding 48 --margin 48 --margin-fill "135deg #0b150f #2d6a4f #95d5b2" --radius 24

export_demo cli-keys-off.webp --no-keys "${frame[@]}"
export_demo cli-keys-on.webp --keys "${frame[@]}"
# The toast shows once the brew is done, so these run on until it has gone.
export_demo cli-toasts-off.webp --no-toasts --between ,brew:toasted "${frame[@]}"
export_demo cli-toasts-on.webp --toasts --between ,brew:toasted "${frame[@]}"
export_demo cli-pause.webp --pause --pause-at filter:typed "${frame[@]}"
export_demo cli-cards.webp --intro 1s --intro-line "The Tea Shop" --outro 1s --outro-line "Thanks" "${frame[@]}"
export_demo cli-hide.webp --hide filter:start,filter:done "${frame[@]}"
export_demo cli-highlight.webp --highlight 're= Matcha.*0m ;dim=0.6;between=brew:start,brew:done' "${frame[@]}"
export_demo cli-zoom.webp --zoom 'cells=1,10,38,14;between=brew:start,brew:done' "${frame[@]}"
export_demo cli-redact.webp --redact-regexp 'tea-[0-9a-f]+' "${frame[@]}"

cp "$docs/tapes/theme-sample" "$bin/"
for theme in "Tokyo Night" "Gruvbox Dark" "Cyberpunk" "Nord" "Catppuccin Latte" "Rose Pine Dawn"; do
    reel run "$docs/tapes/themes.tape" "${size[@]}" --chrome --theme "$theme" -o "$demos/cli-theme-$(tr 'A-Z ' 'a-z-' <<<"$theme").png"
done

# The commands' own output.
export NO_COLOR=1
cp -r "$docs/tapes/tests" tests
reel test tests/shop.tape --update >/dev/null
reel still "$cast" --mark brew:done -o "$fixtures/cli-still.snap"

# The build's version block changes with every commit.
transcript cli-help <<'EOF'
reel --help | sed '/version:/,/go version:/d'
EOF
transcript cli-info <<EOF
reel info $cast
EOF
transcript cli-themes <<'EOF'
reel themes tokyo
reel themes --dark --hue green
EOF
transcript cli-fonts <<'EOF'
reel fonts | grep Mono | head -n 5
EOF
# One of the tapes fails on purpose, which is exit code 3.
{ echo '$ reel test tests/'; reel test tests/ 2>&1 || [ $? -eq 3 ]; } | tidy >"$fixtures/cli-test.output"
transcript cli-session <<'EOF'
reel s start --name shop --size 72x16 -- teashop
reel s wait --text Sencha
reel s key down down
reel s type /green
reel s wait --text "Filter: green" --print
reel s key esc
reel s mark brew:start
reel s key enter
reel s wait --text "Sencha is ready" --timeout 5s
reel s list | cut -f1,3,4,6
reel s stop -o shop.gif
EOF
