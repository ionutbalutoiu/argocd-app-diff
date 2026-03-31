package cli

import (
	"strings"
	"testing"

	"argocd-app-diff/internal/repocreds"
)

func TestDiffCommandApplicationFileFlag(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()
	if err := cmd.ParseFlags([]string{"--application-file", "path/to/application.yaml"}); err != nil {
		t.Fatalf("ParseFlags returned error: %v", err)
	}

	got, err := cmd.Flags().GetString(applicationFileFlag)
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if got != "path/to/application.yaml" {
		t.Fatalf("expected %q, got %q", "path/to/application.yaml", got)
	}
}

func TestDiffCommandHelpMentionsRepoCredentialsEnvVar(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()
	if !strings.Contains(cmd.Long, repocreds.EnvVarJSONName) {
		t.Fatalf("expected help text to mention %q, got %q", repocreds.EnvVarJSONName, cmd.Long)
	}
	if !strings.Contains(cmd.Long, repocreds.EnvVarJSONPathName) {
		t.Fatalf("expected help text to mention %q, got %q", repocreds.EnvVarJSONPathName, cmd.Long)
	}
}
