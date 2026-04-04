package appdiff

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	settingspkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/settings"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	repoapiclient "github.com/argoproj/argo-cd/v3/reposerver/apiclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRepositoryOrFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		repo        *argoappv1.Repository
		err         error
		repoURL     string
		expectError bool
		wantRepoURL string
	}{
		{
			name:        "uses configured repo",
			repo:        &argoappv1.Repository{Repo: "https://github.com/example/repo.git"},
			repoURL:     "https://github.com/example/repo.git",
			wantRepoURL: "https://github.com/example/repo.git",
		},
		{
			name:        "falls back on nil repo",
			repoURL:     "https://github.com/example/repo.git",
			wantRepoURL: "https://github.com/example/repo.git",
		},
		{
			name:        "falls back on not found",
			err:         status.Error(codes.NotFound, "not found"),
			repoURL:     "https://github.com/example/repo.git",
			wantRepoURL: "https://github.com/example/repo.git",
		},
		{
			name:        "falls back on permission denied",
			err:         status.Error(codes.PermissionDenied, "permission denied"),
			repoURL:     "https://github.com/example/repo.git",
			wantRepoURL: "https://github.com/example/repo.git",
		},
		{
			name:        "returns other errors",
			err:         errors.New("boom"),
			repoURL:     "https://github.com/example/repo.git",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, err := repositoryOrFallback(tt.repo, tt.err, tt.repoURL)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("repositoryOrFallback returned error: %v", err)
			}
			if repo == nil {
				t.Fatalf("expected repository, got nil")
			}
			if repo.Repo != tt.wantRepoURL {
				t.Fatalf("expected repository URL %q, got %q", tt.wantRepoURL, repo.Repo)
			}
		})
	}
}

func TestResolveDesiredObjectsDoesNotSkipMultiSourceEntriesBeforeRepoServer(t *testing.T) {
	t.Parallel()

	repoIf := fakeRepositoryServiceClient{
		getRepoFunc: func(_ context.Context, repoURL string, _ string) (*argoappv1.Repository, error) {
			return &argoappv1.Repository{Repo: repoURL}, nil
		},
	}

	var calls []string
	repoClient := fakeRepoServerServiceClient{
		generateManifestFunc: func(_ context.Context, req *repoapiclient.ManifestRequest, _ ...grpc.CallOption) (*repoapiclient.ManifestResponse, error) {
			calls = append(calls, req.ApplicationSource.RepoURL)
			switch req.ApplicationSource.Ref {
			case "":
				return &repoapiclient.ManifestResponse{
					Manifests: []string{configMapManifest("base")},
				}, nil
			case "values":
				return &repoapiclient.ManifestResponse{
					Revision: "main",
				}, nil
			default:
				return nil, fmt.Errorf("unexpected source ref %q", req.ApplicationSource.Ref)
			}
		},
	}

	app := &argoappv1.Application{}
	app.SetName("example")
	app.Spec.Destination.Namespace = "default"
	app.Spec.Project = "default"
	app.Spec.Sources = argoappv1.ApplicationSources{
		{
			RepoURL:        "https://github.com/example/base.git",
			Path:           "manifests",
			TargetRevision: "main",
		},
		{
			RepoURL:        "https://github.com/example/values.git",
			Ref:            "values",
			TargetRevision: "main",
		},
	}
	proj := &argoappv1.AppProject{}
	proj.SetName("default")

	targets, err := resolveDesiredObjects(context.Background(), targetParams{
		app:     app,
		project: proj,
		cluster: &argoappv1.Cluster{Info: argoappv1.ClusterInfo{ServerVersion: "v1.30.0"}},
		settings: &settingspkg.Settings{
			AppLabelKey:         "argocd.argoproj.io/instance",
			ControllerNamespace: "argocd",
		},
		repoClient: repoIf,
		repoServer: repoClient,
	})
	if err != nil {
		t.Fatalf("resolveDesiredObjects returned error: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 generated object, got %d", len(targets))
	}
	if targets[0].GetName() != "base" {
		t.Fatalf("expected generated object %q, got %q", "base", targets[0].GetName())
	}
	if len(calls) != 2 {
		t.Fatalf("expected repo-server to be called for both sources, got %d calls", len(calls))
	}
}

func TestResolveDesiredObjectsIncludesObjectsFromEveryManifestSource(t *testing.T) {
	t.Parallel()

	repoIf := fakeRepositoryServiceClient{
		getRepoFunc: func(_ context.Context, repoURL string, _ string) (*argoappv1.Repository, error) {
			return &argoappv1.Repository{Repo: repoURL}, nil
		},
	}

	repoClient := fakeRepoServerServiceClient{
		generateManifestFunc: func(_ context.Context, req *repoapiclient.ManifestRequest, _ ...grpc.CallOption) (*repoapiclient.ManifestResponse, error) {
			return &repoapiclient.ManifestResponse{
				Manifests: []string{configMapManifest(req.ApplicationSource.Path)},
			}, nil
		},
	}

	app := &argoappv1.Application{}
	app.SetName("example")
	app.Spec.Destination.Namespace = "default"
	app.Spec.Project = "default"
	app.Spec.Sources = argoappv1.ApplicationSources{
		{
			RepoURL:        "https://github.com/example/base.git",
			Path:           "base",
			TargetRevision: "main",
		},
		{
			RepoURL:        "https://github.com/example/extra.git",
			Path:           "extra",
			TargetRevision: "main",
		},
	}
	proj := &argoappv1.AppProject{}
	proj.SetName("default")

	targets, err := resolveDesiredObjects(context.Background(), targetParams{
		app:     app,
		project: proj,
		cluster: &argoappv1.Cluster{Info: argoappv1.ClusterInfo{ServerVersion: "v1.30.0"}},
		settings: &settingspkg.Settings{
			AppLabelKey:         "argocd.argoproj.io/instance",
			ControllerNamespace: "argocd",
		},
		repoClient: repoIf,
		repoServer: repoClient,
	})
	if err != nil {
		t.Fatalf("resolveDesiredObjects returned error: %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("expected 2 generated objects, got %d", len(targets))
	}

	names := []string{targets[0].GetName(), targets[1].GetName()}
	if !slices.Contains(names, "base") || !slices.Contains(names, "extra") {
		t.Fatalf("expected generated objects %q and %q, got %#v", "base", "extra", names)
	}
}
