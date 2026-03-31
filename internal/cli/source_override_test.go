package cli

import (
	"testing"
)

func TestParseSourceOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		values      []string
		want        map[string]string
		expectError bool
	}{
		{
			name:   "single override",
			values: []string{"https://github.com/example/repo.git|main"},
			want: map[string]string{
				"https://github.com/example/repo.git": "main",
			},
		},
		{
			name: "multiple overrides",
			values: []string{
				"https://github.com/example/repo.git|main",
				"https://github.com/example/other.git|release-1",
			},
			want: map[string]string{
				"https://github.com/example/repo.git":  "main",
				"https://github.com/example/other.git": "release-1",
			},
		},
		{
			name:        "missing separator",
			values:      []string{"https://github.com/example/repo.git"},
			expectError: true,
		},
		{
			name:        "empty repo",
			values:      []string{"|main"},
			expectError: true,
		},
		{
			name:        "empty revision",
			values:      []string{"https://github.com/example/repo.git|"},
			expectError: true,
		},
		{
			name: "duplicate repo",
			values: []string{
				"https://github.com/example/repo.git|main",
				"https://github.com/example/repo.git|release-1",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSourceOverrides(tt.values)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSourceOverrides returned error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d overrides, got %d", len(tt.want), len(got))
			}
			for repoURL, revision := range tt.want {
				if got[repoURL] != revision {
					t.Fatalf("expected revision %q for %q, got %q", revision, repoURL, got[repoURL])
				}
			}
		})
	}
}

func TestRootCommandSourceRevisionOverrideFlag(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()
	if err := cmd.ParseFlags([]string{
		"--source-revision-override", "https://github.com/example/repo.git|main",
		"--source-revision-override", "https://github.com/example/other.git|release-1",
	}); err != nil {
		t.Fatalf("ParseFlags returned error: %v", err)
	}

	got, err := cmd.Flags().GetStringArray(sourceRevisionOverrideFlag)
	if err != nil {
		t.Fatalf("GetStringArray returned error: %v", err)
	}

	want := []string{
		"https://github.com/example/repo.git|main",
		"https://github.com/example/other.git|release-1",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d values, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected value %q at index %d, got %q", want[i], i, got[i])
		}
	}
}
