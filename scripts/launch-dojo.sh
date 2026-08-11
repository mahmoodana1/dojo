#!/bin/sh
# SUPER+L target: focus dojo if it's already running, otherwise launch it on
# workspace 7 (window rules force fullscreen; workspace rule is belt-and-braces).
#
# Note: this Hyprland build (Lua-config fork) evaluates `hyprctl dispatch`
# arguments as Lua, so dispatches must use hl.dsp.* call syntax — the legacy
# `dispatch exec "[workspace 7] ..."` form is a Lua syntax error here.

# If the binary was rebuilt after the running instance started, restart the
# session so updates actually take effect — but never while an editor window
# is open inside it (window count > 1 means a solve is in progress).
pid=$(pgrep -xo dojo)
if [ -n "$pid" ] && [ "$(readlink -f "$HOME/.local/bin/dojo")" -nt "/proc/$pid" ] \
   && [ "$(tmux list-windows -t dojo 2>/dev/null | wc -l)" -le 1 ]; then
    tmux kill-session -t dojo 2>/dev/null
    sleep 0.3
fi

hyprctl dispatch 'hl.dsp.focus({ workspace = 7 })'

if ! hyprctl clients -j | grep -q '"class": "dojo"'; then
    # tmux session: `i` opens the editor in a separate tmux window, and the
    # session (with your solve state) survives the terminal being closed.
    setsid -f alacritty --class dojo \
        -e tmux new-session -A -s dojo "$HOME/.local/bin/dojo" >/dev/null 2>&1
fi
