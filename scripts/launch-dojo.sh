#!/bin/sh
# SUPER+L target: focus dojo if it's already running, otherwise launch it on
# workspace 7 (window rules force fullscreen; workspace rule is belt-and-braces).
#
# Note: this Hyprland build (Lua-config fork) evaluates `hyprctl dispatch`
# arguments as Lua, so dispatches must use hl.dsp.* call syntax — the legacy
# `dispatch exec "[workspace 7] ..."` form is a Lua syntax error here.

dojo="$HOME/.local/bin/dojo"
resurrect_last="$HOME/.local/share/tmux/resurrect/last"

# Is dojo actually running in the session's first pane? A session restored by
# tmux-resurrect has the right name but only a bare zsh in it.
dojo_alive() {
    [ "$(tmux display-message -p -t dojo:1.1 '#{pane_current_command}' 2>/dev/null)" = dojo ]
}
windows() { tmux list-windows -t dojo 2>/dev/null | wc -l; }

server_was_up=0
tmux has-session 2>/dev/null && server_was_up=1

# Throw the session away (and start over) if:
#  - it holds no dojo (a resurrect leftover, or dojo fell back to a shell), or
#  - the binary was rebuilt after the running instance started, so updates take
#    effect.
# Never while an editor window is open (>1 window means a solve is in progress).
if tmux has-session -t dojo 2>/dev/null && [ "$(windows)" -le 1 ]; then
    pid=$(tmux display-message -p -t dojo:1.1 '#{pane_pid}')
    if ! dojo_alive || [ "$(readlink -f "$dojo")" -nt "/proc/$pid" ]; then
        tmux kill-session -t dojo
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
tmux has-session -t dojo 2>/dev/null \
    || tmux new-session -d -s dojo -e "DOJO_GRAPHICS=$gfx" "$dojo"

# If that just started the tmux server (first launch after boot), continuum
# runs a resurrect restore ~1s later, and its switch-client calls would drag an
# attached dojo window through every restored session. So the terminal waits
# for the post-restore hook (set in ~/.tmux.conf) before attaching.
wait_restore=0
[ "$server_was_up" = 0 ] && [ -e "$resurrect_last" ] && wait_restore=1

# shellcheck disable=SC2086
setsid -f $term -e sh -c '
    if [ "$1" = 1 ]; then
        i=0   # give up after ~6s; a restore normally takes 1-3s
        while [ "$(tmux show -gv @restore-done 2>/dev/null)" != 1 ] && [ $i -lt 60 ]; do
            sleep 0.1; i=$((i + 1))
        done
    fi
    exec tmux attach-session -t dojo
' sh "$wait_restore" >/dev/null 2>&1
