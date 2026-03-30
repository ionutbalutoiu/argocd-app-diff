package cli

import "testing"

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
