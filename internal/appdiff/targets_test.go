package appdiff

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"argocd-app-diff/internal/repocreds"

	settingspkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/settings"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	repoapiclient "github.com/argoproj/argo-cd/v3/reposerver/apiclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestResolveRepository(t *testing.T) {
	t.Parallel()

	envCredentials, err := repocreds.Parse(`[
		{"match":"exact","repo":"https://github.com/example/repo.git","username":"env-user","password":"env-pass"},
		{"match":"prefix","repo":"https://github.com/example","username":"prefix-user","password":"prefix-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	tests := []struct {
		name         string
		repo         *argoappv1.Repository
		err          error
		repoURL      string
		repoCreds    *repocreds.Set
		expectError  bool
		wantRepoURL  string
		wantUsername string
	}{
		{
			name:         "uses configured repo as-is when it already has credentials",
			repo:         &argoappv1.Repository{Repo: "https://github.com/example/repo.git", Username: "api-user"},
			repoURL:      "https://github.com/example/repo.git",
			repoCreds:    envCredentials,
			wantRepoURL:  "https://github.com/example/repo.git",
			wantUsername: "api-user",
		},
		{
			name:         "hydrates sanitized repo from env",
			repo:         &argoappv1.Repository{Repo: "https://github.com/example/repo.git"},
			repoURL:      "https://github.com/example/repo.git",
			repoCreds:    envCredentials,
			wantRepoURL:  "https://github.com/example/repo.git",
			wantUsername: "env-user",
		},
		{
			name:        "falls back on nil repo",
			repoURL:     "https://github.com/example/repo.git",
			wantRepoURL: "https://github.com/example/repo.git",
		},
		{
			name:         "uses env creds on not found",
			err:          status.Error(codes.NotFound, "not found"),
			repoURL:      "https://github.com/example/repo.git",
			repoCreds:    envCredentials,
			wantRepoURL:  "https://github.com/example/repo.git",
			wantUsername: "env-user",
		},
		{
			name:         "uses env creds on permission denied",
			err:          status.Error(codes.PermissionDenied, "permission denied"),
			repoURL:      "https://github.com/example/repo.git",
			repoCreds:    envCredentials,
			wantRepoURL:  "https://github.com/example/repo.git",
			wantUsername: "env-user",
		},
		{
			name:         "uses prefix creds when no exact match exists",
			err:          status.Error(codes.NotFound, "not found"),
			repoURL:      "https://github.com/example/other.git",
			repoCreds:    envCredentials,
			wantRepoURL:  "https://github.com/example/other.git",
			wantUsername: "prefix-user",
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

			repo, err := resolveRepository(tt.repo, tt.err, tt.repoURL, tt.repoCreds)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRepository returned error: %v", err)
			}
			if repo == nil {
				t.Fatalf("expected repository, got nil")
			}
			if repo.Repo != tt.wantRepoURL {
				t.Fatalf("expected repository URL %q, got %q", tt.wantRepoURL, repo.Repo)
			}
			if repo.Username != tt.wantUsername {
				t.Fatalf("expected username %q, got %q", tt.wantUsername, repo.Username)
			}
		})
	}
}

func TestResolveDesiredObjectsDoesNotSkipMultiSourceEntriesBeforeRepoServer(t *testing.T) {
	t.Parallel()

	repoCredentials, err := repocreds.Parse(`[
		{"match":"exact","repo":"https://github.com/example/values.git","username":"values-user","password":"values-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repoIf := fakeRepositoryServiceClient{
		getRepoFunc: func(_ context.Context, repoURL string, _ string) (*argoappv1.Repository, error) {
			if repoURL == "https://github.com/example/values.git" {
				return nil, status.Error(codes.PermissionDenied, "permission denied")
			}
			return &argoappv1.Repository{Repo: repoURL}, nil
		},
	}

	var calls []string
	repoClient := fakeRepoServerServiceClient{
		generateManifestFunc: func(_ context.Context, req *repoapiclient.ManifestRequest, _ ...grpc.CallOption) (*repoapiclient.ManifestResponse, error) {
			calls = append(calls, req.ApplicationSource.RepoURL)
			if refSource, ok := req.RefSources["$values"]; ok {
				if refSource.Repo.Username != "values-user" {
					return nil, fmt.Errorf("expected ref source credentials to be hydrated, got %q", refSource.Repo.Username)
				}
			}
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
		App:     app,
		Project: proj,
		Cluster: &argoappv1.Cluster{Info: argoappv1.ClusterInfo{ServerVersion: "v1.30.0"}},
		Settings: &settingspkg.Settings{
			AppLabelKey:         "argocd.argoproj.io/instance",
			ControllerNamespace: "argocd",
		},
		RepoClient:      repoIf,
		RepoServer:      repoClient,
		RepoCredentials: repoCredentials,
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

	repoCredentials, err := repocreds.Parse(`[
		{"match":"exact","repo":"https://charts.example.com/private","username":"chart-user","password":"chart-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repoIf := fakeRepositoryServiceClient{
		getRepoFunc: func(_ context.Context, repoURL string, _ string) (*argoappv1.Repository, error) {
			return &argoappv1.Repository{Repo: repoURL}, nil
		},
		listRepositoriesFunc: func(_ context.Context) (*argoappv1.RepositoryList, error) {
			return &argoappv1.RepositoryList{
				Items: []*argoappv1.Repository{
					{
						Repo: "https://charts.example.com/private",
						Type: "helm",
						Name: "private-chart",
					},
				},
			}, nil
		},
	}

	var manifestRequests []*repoapiclient.ManifestRequest
	repoClient := fakeRepoServerServiceClient{
		generateManifestFunc: func(_ context.Context, req *repoapiclient.ManifestRequest, _ ...grpc.CallOption) (*repoapiclient.ManifestResponse, error) {
			manifestRequests = append(manifestRequests, req)
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
	proj.Spec.SourceRepos = []string{"*"}

	targets, err := resolveDesiredObjects(context.Background(), targetParams{
		App:     app,
		Project: proj,
		Cluster: &argoappv1.Cluster{Info: argoappv1.ClusterInfo{ServerVersion: "v1.30.0"}},
		Settings: &settingspkg.Settings{
			AppLabelKey:         "argocd.argoproj.io/instance",
			ControllerNamespace: "argocd",
		},
		RepoClient:      repoIf,
		RepoServer:      repoClient,
		RepoCredentials: repoCredentials,
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
	if len(manifestRequests) != 2 {
		t.Fatalf("expected 2 manifest requests, got %d", len(manifestRequests))
	}
	for _, req := range manifestRequests {
		if len(req.Repos) != 1 {
			t.Fatalf("expected 1 hydrated permitted repo, got %d", len(req.Repos))
		}
		if req.Repos[0].Username != "chart-user" {
			t.Fatalf("expected permitted repo credentials to be hydrated, got %q", req.Repos[0].Username)
		}
		if len(req.HelmRepoCreds) != 1 {
			t.Fatalf("expected 1 helm repo credential, got %d", len(req.HelmRepoCreds))
		}
		if req.HelmRepoCreds[0].Username != "chart-user" {
			t.Fatalf("expected helm repo credentials to be passed through, got %q", req.HelmRepoCreds[0].Username)
		}
	}
}

func TestResolveDesiredObjectsUsesPrefixRepoCredsForHelmDependencies(t *testing.T) {
	t.Parallel()

	repoCredentials, err := repocreds.Parse(`[
		{"match":"prefix","repo":"https://charts.example.com/team","username":"team-user","password":"team-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repoIf := fakeRepositoryServiceClient{
		getRepoFunc: func(_ context.Context, repoURL string, _ string) (*argoappv1.Repository, error) {
			return &argoappv1.Repository{Repo: repoURL}, nil
		},
	}

	var manifestRequest *repoapiclient.ManifestRequest
	repoClient := fakeRepoServerServiceClient{
		generateManifestFunc: func(_ context.Context, req *repoapiclient.ManifestRequest, _ ...grpc.CallOption) (*repoapiclient.ManifestResponse, error) {
			manifestRequest = req
			return &repoapiclient.ManifestResponse{
				Manifests: []string{configMapManifest("chart")},
			}, nil
		},
	}

	app := &argoappv1.Application{}
	app.SetName("example")
	app.Spec.Destination.Namespace = "default"
	app.Spec.Project = "default"
	app.Spec.Source = &argoappv1.ApplicationSource{
		RepoURL:        "https://github.com/example/chart.git",
		Path:           "chart",
		TargetRevision: "main",
	}
	proj := &argoappv1.AppProject{}
	proj.SetName("default")
	proj.Spec.SourceRepos = []string{"*"}

	_, err = resolveDesiredObjects(context.Background(), targetParams{
		App:     app,
		Project: proj,
		Cluster: &argoappv1.Cluster{Info: argoappv1.ClusterInfo{ServerVersion: "v1.30.0"}},
		Settings: &settingspkg.Settings{
			AppLabelKey:         "argocd.argoproj.io/instance",
			ControllerNamespace: "argocd",
		},
		RepoClient:      repoIf,
		RepoServer:      repoClient,
		RepoCredentials: repoCredentials,
	})
	if err != nil {
		t.Fatalf("resolveDesiredObjects returned error: %v", err)
	}
	if manifestRequest == nil {
		t.Fatal("expected GenerateManifest to be called")
	}
	if len(manifestRequest.HelmRepoCreds) != 1 {
		t.Fatalf("expected 1 helm repo credential, got %d", len(manifestRequest.HelmRepoCreds))
	}
	if manifestRequest.HelmRepoCreds[0].Username != "team-user" {
		t.Fatalf("expected prefix repo credential to be included, got %q", manifestRequest.HelmRepoCreds[0].Username)
	}
}

func TestResolveDesiredObjectsIncludesOCIHelmRepoCreds(t *testing.T) {
	t.Parallel()

	repoCredentials, err := repocreds.Parse(`[
		{"match":"prefix","repo":"oci://registry.example.com/team","username":"oci-user","password":"oci-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repoIf := fakeRepositoryServiceClient{
		getRepoFunc: func(_ context.Context, repoURL string, _ string) (*argoappv1.Repository, error) {
			return &argoappv1.Repository{Repo: repoURL}, nil
		},
	}

	var manifestRequest *repoapiclient.ManifestRequest
	repoClient := fakeRepoServerServiceClient{
		generateManifestFunc: func(_ context.Context, req *repoapiclient.ManifestRequest, _ ...grpc.CallOption) (*repoapiclient.ManifestResponse, error) {
			manifestRequest = req
			return &repoapiclient.ManifestResponse{
				Manifests: []string{configMapManifest("chart")},
			}, nil
		},
	}

	app := &argoappv1.Application{}
	app.SetName("example")
	app.Spec.Destination.Namespace = "default"
	app.Spec.Project = "default"
	app.Spec.Source = &argoappv1.ApplicationSource{
		RepoURL:        "https://github.com/example/chart.git",
		Path:           "chart",
		TargetRevision: "main",
	}
	proj := &argoappv1.AppProject{}
	proj.SetName("default")
	proj.Spec.SourceRepos = []string{"*"}

	_, err = resolveDesiredObjects(context.Background(), targetParams{
		App:     app,
		Project: proj,
		Cluster: &argoappv1.Cluster{Info: argoappv1.ClusterInfo{ServerVersion: "v1.30.0"}},
		Settings: &settingspkg.Settings{
			AppLabelKey:         "argocd.argoproj.io/instance",
			ControllerNamespace: "argocd",
		},
		RepoClient:      repoIf,
		RepoServer:      repoClient,
		RepoCredentials: repoCredentials,
	})
	if err != nil {
		t.Fatalf("resolveDesiredObjects returned error: %v", err)
	}
	if manifestRequest == nil {
		t.Fatal("expected GenerateManifest to be called")
	}
	if len(manifestRequest.HelmRepoCreds) != 1 {
		t.Fatalf("expected 1 helm repo credential, got %d", len(manifestRequest.HelmRepoCreds))
	}
	if manifestRequest.HelmRepoCreds[0].Username != "oci-user" {
		t.Fatalf("expected OCI repo credential username %q, got %q", "oci-user", manifestRequest.HelmRepoCreds[0].Username)
	}
	if !manifestRequest.HelmRepoCreds[0].EnableOCI {
		t.Fatal("expected OCI repo credential to preserve OCI inference")
	}
	if manifestRequest.HelmRepoCreds[0].Type != "oci" {
		t.Fatalf("expected OCI repo credential type %q, got %q", "oci", manifestRequest.HelmRepoCreds[0].Type)
	}
}
