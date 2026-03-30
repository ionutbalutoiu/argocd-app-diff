package cli

import "testing"

func TestNewRootCommandUsesCanonicalName(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()
	if cmd.Use != "argocd-app-diff" {
		t.Fatalf("expected canonical command name %q, got %q", "argocd-app-diff", cmd.Use)
	}
	if len(cmd.Aliases) != 0 {
		t.Fatalf("expected no aliases, got %#v", cmd.Aliases)
	}
}

func TestRootCommandRejectsPositionalArgs(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()
	cmd.SetArgs([]string{"diff"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected positional args to be rejected")
	}
}
