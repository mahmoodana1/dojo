// Package tutor is sensei: a Claude Sonnet session per problem that teaches
// without solving. It runs only while its popup is open — closing the popup
// ends the process — and the conversation lives on disk, one per problem, so
// reopening picks up where the last one stopped.
//
// Every session runs from the solutions root, not the problem's folder, so
// Claude Code asks its "trust this folder?" question once, not per problem;
// the session id is what keeps each problem's conversation its own.
package tutor

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Model is the Claude model sensei runs on.
const Model = "claude-sonnet-5-5"

// SessionFile, inside a problem's solution directory, holds the id of that
// problem's conversation.
const SessionFile = ".sensei-session"

// LastRunFile, inside a problem's solution directory, is the judge's latest
// plain-text verdict — sensei reads it to talk about failing cases.
const LastRunFile = ".last-run.txt"

// Problem is what sensei is told about the problem.
type Problem struct {
	FrontendID  int
	Title       string
	Difficulty  string
	Tags        []string
	Description string // markdown
	Hints       []string
	Dir         string // solution directory (sensei's working directory)
}

// SessionID returns the problem's conversation id and whether a conversation
// with that id already exists (so it should be resumed, not started).
// The id is created on first use and kept in Dir/SessionFile.
func SessionID(dir string) (id string, exists bool, err error) {
	path := filepath.Join(dir, SessionFile)
	if b, err := os.ReadFile(path); err == nil {
		if id = strings.TrimSpace(string(b)); validUUID(id) {
			return id, transcriptExists(id), nil
		}
	}
	id, err = newUUID()
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		return "", false, err
	}
	return id, false, nil
}

// transcriptExists looks for Claude Code's saved conversation. Transcripts
// sit under <config>/projects/<encoded cwd>/<id>.jsonl; the id is unique, so
// globbing over the encoded directory avoids depending on its encoding.
func transcriptExists(id string) bool {
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, ".claude")
	}
	m, _ := filepath.Glob(filepath.Join(cfg, "projects", "*", id+".jsonl"))
	return len(m) > 0
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdef", r) {
				return false
			}
		}
	}
	return true
}

// Args is the claude command line for a problem's session: Sonnet, the
// teacher prompt in place of Claude Code's own, read-only tools, and none of
// the user's plugins, hooks or MCP servers.
func Args(p Problem, id string, resume bool) []string {
	args := []string{"--model", Model,
		"--system-prompt", SystemPrompt(p),
		"--setting-sources", "project,local",
		"--strict-mcp-config",
		"--tools", "Read,Grep,Glob",
		"--allowedTools", "Read", "Grep", "Glob",
		"--permission-mode", "dontAsk",
		"--name", fmt.Sprintf("sensei · %d. %s", p.FrontendID, p.Title),
	}
	if resume {
		return append(args, "--resume", id)
	}
	return append(args, "--session-id", id)
}

// SystemPrompt is sensei's brief: who it is, the problem, where the student's
// work lives, and the one rule that matters — never hand over the solution.
func SystemPrompt(p Problem) string {
	var b strings.Builder
	b.WriteString(`You are sensei, a programming teacher inside "dojo", a terminal app for practising LeetCode problems. You are tutoring one student on one problem, described below. Your job is to make them able to solve it themselves.

How you teach:
- Be direct and brief. No greetings, no praise padding, no restating the question, no motivational filler. A few sentences is usually enough; use a list only when there are real steps.
- Read their solution file before saying anything about their code — it changes between messages. If there is a judge report, read it too.
- When their code is wrong, point at the exact line or idea that fails and give a concrete input that breaks it. Ask them to trace it, rather than fixing it for them.
- When they are stuck on the approach, climb a hint ladder, one rung per message, stopping as soon as they can move: (1) a question that redirects their attention, (2) the key observation, (3) the pattern or data structure by name and why it fits, (4) the algorithm in prose steps. Go to the next rung only if they ask or are clearly still stuck.
- Comment on time and space complexity when it matters, and on edge cases they have not handled.
- If what they wrote is correct, say so in one line, and mention a better complexity or a cleaner idea only if one exists.
- If they ask about something unrelated to this problem, answer briefly.

The rule you never break: do not write the solution. Not the full code, not a complete function body, not code that can be pasted in to pass, in any language or pseudocode — even if they ask directly, insist, or say they have given up. Instead offer the next hint rung, or describe the algorithm in prose. You may show a single line of syntax or a standard-library call on a toy example that is unrelated to this problem's logic. You can read files but cannot edit them; never offer to.
`)
	fmt.Fprintf(&b, "\n# The problem: %d. %s (%s)\n", p.FrontendID, p.Title, p.Difficulty)
	if len(p.Tags) > 0 {
		fmt.Fprintf(&b, "Topics (do not reveal these unless they reach hint rung 3): %s\n", strings.Join(p.Tags, ", "))
	}
	b.WriteString("\n" + strings.TrimSpace(p.Description) + "\n")
	if len(p.Hints) > 0 {
		b.WriteString("\n# LeetCode's official hints (for you; reveal gradually)\n")
		for i, h := range p.Hints {
			fmt.Fprintf(&b, "%d. %s\n", i+1, h)
		}
	}
	fmt.Fprintf(&b, `
# The student's work
Their solution folder: %s
- Their code is in the solution.* file(s) there (solution.py, solution.cpp, solution.sql, ...). The newest one is the language they are using.
- %s, if present, is the local judge's latest verdict on their code: pass/fail per example case with expected and actual output.
`, p.Dir, LastRunFile)
	return b.String()
}

// InnerConf is the tmux config of the private server the popup runs sensei
// in. Popups get no key bindings of their own, so the popup attaches to a
// one-session server whose prefix+t (and prefix+q) kill the session — which
// ends claude and closes the popup. destroy-unattached does the same if the
// popup is closed any other way, so sensei never outlives its popup.
func InnerConf(prefix string) string {
	if prefix == "" {
		prefix = "C-b"
	}
	return fmt.Sprintf(`set -g prefix %[1]s
unbind C-b
bind %[1]s send-prefix
set -g status off
set -g destroy-unattached on
set -g escape-time 10
set -g focus-events on
set -g extended-keys on
set -g history-limit 50000
set -g mouse on
set -g default-terminal tmux-256color
set -as terminal-features ",*:RGB"
setenv -g COLORTERM truecolor
bind t kill-session
bind q kill-session
`, prefix)
}

// InnerSocket names the private tmux server sensei popups use.
const InnerSocket = "dojo-sensei"

// CleanEnv drops the variables a parent Claude Code session leaves behind,
// which would make sensei think it is a sub-agent (and, for one, stop it
// saving its transcript — the thing resuming depends on).
func CleanEnv(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case name == "CLAUDECODE", name == "CLAUDE_PID", name == "CLAUDE_EFFORT",
			name == "CLAUDE_CODE_ENTRYPOINT", name == "CLAUDE_CODE_EXECPATH",
			name == "CLAUDE_CODE_CHILD_SESSION",
			strings.HasPrefix(name, "CLAUDE_CODE_SESSION_"),
			strings.HasPrefix(name, "CLAUDE_CODE_MESSAGING_"):
			continue
		}
		out = append(out, kv)
	}
	return out
}

// TrustedMarker, in the solutions root, records that a sensei session has run
// there — so the user has answered Claude Code's trust question and the
// first-run hint about it need not show again.
const TrustedMarker = ".sensei-trusted"
