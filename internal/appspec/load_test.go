package appspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		content         string
		expectError     bool
		wantName        string
		wantProjectName string
	}{
		{
			name: "valid application defaults project",
			content: `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-app
spec:
  source:
    repoURL: https://github.com/example/repo.git
    path: manifests
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`,
			wantName:        "test-app",
			wantProjectName: "default",
		},
		{
			name: "multiple documents",
			content: `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: one
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: two
`,
			expectError: true,
		},
		{
			name: "wrong kind",
			content: `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test
`,
			expectError: true,
		},
		{
			name:        "empty file",
			content:     ``,
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "app.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			app, err := Load(path)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			if app.Name != tt.wantName {
				t.Fatalf("expected app name %q, got %q", tt.wantName, app.Name)
			}
			if app.Spec.Project != tt.wantProjectName {
				t.Fatalf("expected project %q, got %q", tt.wantProjectName, app.Spec.Project)
			}
		})
	}
}
