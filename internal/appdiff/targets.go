package appdiff

import (
	"context"
	"fmt"
	"slices"

	repositorypkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/repository"
	settingspkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/settings"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	repoapiclient "github.com/argoproj/argo-cd/v3/reposerver/apiclient"
	"github.com/argoproj/argo-cd/v3/util/argo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type targetParams struct {
	app         *argoappv1.Application
	project     *argoappv1.AppProject
	cluster     *argoappv1.Cluster
	settings    *settingspkg.Settings
	repoClient  repositorypkg.RepositoryServiceClient
	repoServer  repoapiclient.RepoServerServiceClient
	hardRefresh bool
}

func resolveDesiredObjects(ctx context.Context, params targetParams) ([]*unstructured.Unstructured, error) {
	app := params.app
	proj := params.project
	cluster := params.cluster
	settings := params.settings
	repoIf := params.repoClient
	repoClient := params.repoServer
	hardRefresh := params.hardRefresh

	repos, err := loadPermittedRepos(ctx, repoIf, proj)
	if err != nil {
		return nil, err
	}

	sources := applicationSources(app)
	revisions := make([]string, len(sources))
	for i := range sources {
		revisions[i] = sources[i].TargetRevision
	}
	refSources, err := argo.GetRefSources(ctx, sources, app.Spec.Project, sourceRepositoryGetter(repoIf), revisions)
	if err != nil {
		return nil, fmt.Errorf("resolve ref sources: %w", err)
	}

	var targetObjects []*unstructured.Unstructured
	for _, source := range sources {
		repo, err := getRepository(ctx, repoIf, source.RepoURL, proj.Name)
		if err != nil {
			return nil, fmt.Errorf("get repository %q: %w", source.RepoURL, err)
		}

		manifestResp, err := repoClient.GenerateManifest(ctx, &repoapiclient.ManifestRequest{
			Repo:                            repo,
			Repos:                           repos.forSource(source),
			Revision:                        source.TargetRevision,
			NoCache:                         hardRefresh,
			NoRevisionCache:                 hardRefresh,
			AppLabelKey:                     settings.AppLabelKey,
			AppName:                         app.InstanceName(settings.ControllerNamespace),
			Namespace:                       app.Spec.Destination.Namespace,
			ApplicationSource:               &source,
			KustomizeOptions:                settings.KustomizeOptions,
			KubeVersion:                     cluster.Info.ServerVersion,
			ApiVersions:                     cluster.Info.APIVersions,
			TrackingMethod:                  settings.TrackingMethod,
			ProjectName:                     proj.Name,
			ProjectSourceRepos:              proj.Spec.SourceRepos,
			HasMultipleSources:              app.Spec.HasMultipleSources(),
			RefSources:                      refSources,
			AnnotationManifestGeneratePaths: app.GetAnnotation(argoappv1.AnnotationKeyManifestGeneratePaths),
			InstallationID:                  settings.InstallationID,
		})
		if err != nil {
			return nil, fmt.Errorf("generate manifests for source %q: %w", source.RepoURL, err)
		}

		objects, err := decodeManifestObjects(manifestResp.Manifests)
		if err != nil {
			return nil, err
		}
		targetObjects = append(targetObjects, objects...)
	}

	return targetObjects, nil
}

func applicationSources(app *argoappv1.Application) argoappv1.ApplicationSources {
	sources := app.Spec.GetSources()
	if len(sources) > 0 {
		return sources
	}

	return argoappv1.ApplicationSources{app.Spec.GetSource()}
}

func decodeManifestObjects(manifests []string) ([]*unstructured.Unstructured, error) {
	objects := make([]*unstructured.Unstructured, 0, len(manifests))
	for _, manifest := range manifests {
		obj, err := argoappv1.UnmarshalToUnstructured(manifest)
		if err != nil {
			return nil, fmt.Errorf("decode generated manifest: %w", err)
		}
		objects = append(objects, obj)
	}

	return objects, nil
}

func sourceRepositoryGetter(repoIf repositorypkg.RepositoryServiceClient) func(context.Context, string, string) (*argoappv1.Repository, error) {
	return func(ctx context.Context, repoURL string, project string) (*argoappv1.Repository, error) {
		return getRepository(ctx, repoIf, repoURL, project)
	}
}

func getRepository(
	ctx context.Context,
	repoIf repositorypkg.RepositoryServiceClient,
	repoURL string,
	project string,
) (*argoappv1.Repository, error) {
	repo, err := repoIf.Get(ctx, &repositorypkg.RepoQuery{
		Repo:       repoURL,
		AppProject: project,
	})
	return repositoryOrFallback(repo, err, repoURL)
}

func repositoryOrFallback(repo *argoappv1.Repository, err error, repoURL string) (*argoappv1.Repository, error) {
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound, codes.PermissionDenied:
			return &argoappv1.Repository{Repo: repoURL}, nil
		default:
			return nil, err
		}
	}
	if repo == nil {
		return &argoappv1.Repository{Repo: repoURL}, nil
	}
	return repo, nil
}

type permittedRepos struct {
	helm []*argoappv1.Repository
	oci  []*argoappv1.Repository
}

func (r permittedRepos) forSource(source argoappv1.ApplicationSource) []*argoappv1.Repository {
	if !source.IsOCI() {
		return r.helm
	}

	repos := slices.Clone(r.helm)
	return append(repos, r.oci...)
}

func loadPermittedRepos(
	ctx context.Context,
	repoIf repositorypkg.RepositoryServiceClient,
	proj *argoappv1.AppProject,
) (permittedRepos, error) {
	configuredRepos, err := repoIf.ListRepositories(ctx, &repositorypkg.RepoQuery{})
	if err != nil {
		return permittedRepos{}, fmt.Errorf("list configured repositories: %w", err)
	}

	var helmRepos []*argoappv1.Repository
	var ociRepos []*argoappv1.Repository
	for _, repo := range configuredRepos.Items {
		switch {
		case repo.EnableOCI:
			ociRepos = append(ociRepos, repo)
		case repo.Type == "helm":
			helmRepos = append(helmRepos, repo)
		}
	}

	permittedHelmRepos, err := argo.GetPermittedRepos(proj, helmRepos)
	if err != nil {
		return permittedRepos{}, fmt.Errorf("filter permitted helm repositories: %w", err)
	}
	permittedOCIRepos, err := argo.GetPermittedRepos(proj, ociRepos)
	if err != nil {
		return permittedRepos{}, fmt.Errorf("filter permitted OCI repositories: %w", err)
	}

	return permittedRepos{
		helm: permittedHelmRepos,
		oci:  permittedOCIRepos,
	}, nil
}
