package tutor

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSessionIDIsStablePerProblem(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	a, b := t.TempDir(), t.TempDir()

	id1, exists, err := SessionID(a)
	if err != nil || exists || !validUUID(id1) {
		t.Fatalf("first SessionID = %q, %v, %v; want a fresh uuid", id1, exists, err)
	}
	again, _, _ := SessionID(a)
	if again != id1 {
		t.Errorf("SessionID changed between calls: %q then %q", id1, again)
	}
	other, _, _ := SessionID(b)
	if other == id1 {
		t.Error("two problems share a session id")
	}
}

func TestSessionIDResumesOnlyWhenTranscriptExists(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := t.TempDir()
	id, _, _ := SessionID(dir)

	proj := filepath.Join(cfg, "projects", "-some-cwd")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, exists, _ := SessionID(dir); !exists {
		t.Error("transcript on disk, but SessionID says start fresh")
	}
}

func TestSessionIDReplacesGarbage(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SessionFile), []byte("not-a-uuid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _, err := SessionID(dir)
	if err != nil || !validUUID(id) {
		t.Fatalf("SessionID = %q, %v", id, err)
	}
}

func TestArgs(t *testing.T) {
	p := Problem{FrontendID: 1, Title: "Two Sum", Difficulty: "Easy", Dir: "/x/0001-two-sum"}
	fresh := Args(p, "abc", false)
	if i := slices.Index(fresh, "--model"); i < 0 || fresh[i+1] != "claude-sonnet-5-5" {
		t.Errorf("model not sonnet 5.5: %v", fresh)
	}
	if i := slices.Index(fresh, "--session-id"); i < 0 || fresh[i+1] != "abc" {
		t.Errorf("fresh session not started by id: %v", fresh)
	}
	if slices.Contains(fresh, "--resume") {
		t.Error("fresh session resumes")
	}
	resumed := Args(p, "abc", true)
	if i := slices.Index(resumed, "--resume"); i < 0 || resumed[i+1] != "abc" {
		t.Errorf("resume missing: %v", resumed)
	}
	// read-only: no edit/write/shell tools
	i := slices.Index(fresh, "--tools")
	if i < 0 || fresh[i+1] != "Read,Grep,Glob" {
		t.Errorf("tools = %v", fresh)
	}
}

func TestSystemPrompt(t *testing.T) {
	p := Problem{FrontendID: 1, Title: "Two Sum", Difficulty: "Easy", Tags: []string{"Array"},
		Description: "Given an array nums...", Hints: []string{"use a map"}, Dir: "/x/0001-two-sum"}
	got := SystemPrompt(p)
	for _, want := range []string{"1. Two Sum (Easy)", "Given an array nums", "use a map",
		"/x/0001-two-sum", LastRunFile, "do not write the solution"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}

func TestCleanEnv(t *testing.T) {
	env := []string{"HOME=/h", "CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1",
		"CLAUDE_CODE_SESSION_ID=x", "CLAUDE_CODE_MESSAGING_SOCKET=/s", "CLAUDE_PID=9",
		"CLAUDE_CONFIG_DIR=/c", "CLAUDE_CODE_USE_BEDROCK=1"}
	got := CleanEnv(env)
	want := []string{"HOME=/h", "CLAUDE_CONFIG_DIR=/c", "CLAUDE_CODE_USE_BEDROCK=1"}
	if !slices.Equal(got, want) {
		t.Errorf("CleanEnv = %v, want %v", got, want)
	}
}

func TestInnerConf(t *testing.T) {
	c := InnerConf("C-a")
	for _, want := range []string{"set -g prefix C-a", "bind t kill-session",
		"destroy-unattached on", "status off"} {
		if !strings.Contains(c, want) {
			t.Errorf("inner conf lacks %q", want)
		}
	}
	if !strings.Contains(InnerConf(""), "set -g prefix C-b") {
		t.Error("empty prefix should default to C-b")
	}
}
