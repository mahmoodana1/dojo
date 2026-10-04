#!/bin/sh
# SUPER+L target: focus dojo if it's already running, otherwise launch it on
# workspace 7 (window rules force fullscreen; workspace rule is belt-and-braces).
#
# Note: this Hyprland build (Lua-config fork) evaluates `hyprctl dispatch`
# arguments as Lua, so dispatches must use hl.dsp.* call syntax — the legacy
# `dispatch exec "[workspace 7] ..."` form is a Lua syntax error here.

dojo="$HOME/.local/bin/dojo"

# The pane dojo runs in: the first window of the session named exactly "dojo".
# (Not "dojo:1.1": with base-index 0 that is the first *solution* window, and
# display-message silently falls back to some other pane when a target is
# missing, so it can't be used to test for one.)
dojo_pane() {
    tmux list-panes -t '=dojo:^' -F "#{$1}" 2>/dev/null | head -n1
}
dojo_alive() { [ "$(dojo_pane pane_current_command)" = dojo ]; }
windows() { tmux list-windows -t '=dojo' 2>/dev/null | wc -l; }

# Throw the session away (and start over) if:
#  - it holds no dojo (dojo fell back to a shell), or
#  - the binary was rebuilt after the running instance started, so updates take
#    effect.
# Never while a solution window is open (>1 window means a solve is in progress).
if tmux has-session -t '=dojo' 2>/dev/null && [ "$(windows)" -le 1 ]; then
    pid=$(dojo_pane pane_pid)
    if ! dojo_alive || [ "$(readlink -f "$dojo")" -nt "/proc/$pid" ]; then
        tmux kill-session -t '=dojo'
    fi
fi

hyprctl dispatch 'hl.dsp.focus({ workspace = 7 })'

# A dojo window already attached to a live dojo: focusing was enough.
win_pid=$(hyprctl clients -j | jq -r '.[] | select(.class == "dojo") | .pid' | head -n1)
if [ -n "$win_pid" ]; then
    dojo_alive && exit 0
    # Its session is gone — with detach-on-destroy off, that client got bounced
    # into some other session. Close it and start clean.
    kill "$win_pid" 2>/dev/null
    sleep 0.2
fi

# kitty, not alacritty: it speaks the graphics protocol, so problem diagrams
# render as real pixels instead of block approximations. Requires
# `set -g allow-passthrough on` in ~/.tmux.conf so the escapes survive tmux.
if command -v kitty >/dev/null 2>&1; then
    term="kitty --class dojo"
    gfx=kitty
else
    term="alacritty --class dojo"   # fallback: half-block images
    gfx=none
fi

# Create the session detached, *then* attach. DOJO_GRAPHICS is set explicitly:
# a server started from another terminal keeps its own stale environment.
tmux has-session -t '=dojo' 2>/dev/null \
    || tmux new-session -d -s dojo -e "DOJO_GRAPHICS=$gfx" "$dojo"

# shellcheck disable=SC2086
setsid -f $term -e tmux attach-session -t '=dojo' >/dev/null 2>&1
