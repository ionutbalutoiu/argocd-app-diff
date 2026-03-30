package appspec

import (
	"testing"

	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

func TestApplySourceRevisionOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		app         *argoappv1.Application
		overrides   map[string]string
		expectError bool
		wantSource  string
		wantSources []string
		checkSingle bool
	}{
		{
			name: "single source",
			app: &argoappv1.Application{
				Spec: argoappv1.ApplicationSpec{
					Source: &argoappv1.ApplicationSource{
						RepoURL:        "https://github.com/example/repo.git",
						TargetRevision: "HEAD",
					},
				},
			},
			overrides: map[string]string{
				"https://github.com/example/repo.git": "main",
			},
			checkSingle: true,
			wantSource:  "main",
		},
		{
			name: "multiple matching sources",
			app: &argoappv1.Application{
				Spec: argoappv1.ApplicationSpec{
					Sources: argoappv1.ApplicationSources{
						{
							RepoURL:        "https://github.com/example/repo.git",
							TargetRevision: "HEAD",
							Path:           "apps/a",
						},
						{
							RepoURL:        "https://github.com/example/repo.git",
							TargetRevision: "HEAD",
							Ref:            "values",
						},
						{
							RepoURL:        "https://github.com/example/other.git",
							TargetRevision: "stable",
							Path:           "apps/b",
						},
					},
				},
			},
			overrides: map[string]string{
				"https://github.com/example/repo.git": "release-1",
			},
			wantSources: []string{"release-1", "release-1", "stable"},
		},
		{
			name: "unmatched override",
			app: &argoappv1.Application{
				Spec: argoappv1.ApplicationSpec{
					Source: &argoappv1.ApplicationSource{
						RepoURL:        "https://github.com/example/repo.git",
						TargetRevision: "HEAD",
					},
				},
			},
			overrides: map[string]string{
				"https://github.com/example/missing.git": "main",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ApplySourceRevisionOverrides(tt.app, tt.overrides)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplySourceRevisionOverrides returned error: %v", err)
			}

			if tt.checkSingle {
				if tt.app.Spec.Source == nil {
					t.Fatalf("expected single source to be present")
				}
				if tt.app.Spec.Source.TargetRevision != tt.wantSource {
					t.Fatalf("expected target revision %q, got %q", tt.wantSource, tt.app.Spec.Source.TargetRevision)
				}
			}

			if len(tt.wantSources) > 0 {
				if len(tt.app.Spec.Sources) != len(tt.wantSources) {
					t.Fatalf("expected %d sources, got %d", len(tt.wantSources), len(tt.app.Spec.Sources))
				}
				for i, wantRevision := range tt.wantSources {
					if tt.app.Spec.Sources[i].TargetRevision != wantRevision {
						t.Fatalf("expected source %d target revision %q, got %q", i, wantRevision, tt.app.Spec.Sources[i].TargetRevision)
					}
				}
			}
		})
	}
}

func TestApplySourceRevisionOverridesNilApplication(t *testing.T) {
	t.Parallel()

	err := ApplySourceRevisionOverrides(nil, map[string]string{
		"https://github.com/example/repo.git": "main",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
