package appdiff

import (
	"context"
	"fmt"
	"io"

	"argocd-app-diff/internal/repocreds"
	"argocd-app-diff/internal/reposerver"

	argocdclient "github.com/argoproj/argo-cd/v3/pkg/apiclient"
	applicationpkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	clusterpkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/cluster"
	projectpkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/project"
	repositorypkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/repository"
	settingspkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/settings"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// Request describes one diff run.
type Request struct {
	Application     *argoappv1.Application
	LiveNamespace   string
	RepoServerURL   string
	RepoCredentials *repocreds.Set
	Refresh         bool
	HardRefresh     bool
}

// Result reports whether the diff produced changes.
type Result struct {
	FoundDiffs bool
}

// Run compares a local Application spec with live managed resources.
func Run(ctx context.Context, apiClient argocdclient.Client, req Request) (Result, error) {
	return run(ctx, apiClient, req, stdoutPrinter{})
}

func run(ctx context.Context, apiClient argocdclient.Client, req Request, printer diffPrinter) (Result, error) {
	if req.Application == nil {
		return Result{}, fmt.Errorf("application is required")
	}
	if !hasApplicationSources(req.Application) {
		return Result{}, fmt.Errorf("application has no sources configured")
	}

	clients, err := openAPIClients(apiClient)
	if err != nil {
		return Result{}, err
	}
	defer clients.Close()

	projectResp, err := loadProject(ctx, clients.project, req.Application)
	if err != nil {
		return Result{}, err
	}
	clusterResp, err := loadDestinationCluster(ctx, clients.cluster, req.Application)
	if err != nil {
		return Result{}, err
	}
	projectClusters, err := clients.cluster.List(ctx, &clusterpkg.ClusterQuery{})
	if err != nil {
		return Result{}, fmt.Errorf("list clusters: %w", err)
	}
	settingsResp, err := clients.settings.Get(ctx, &settingspkg.SettingsQuery{})
	if err != nil {
		return Result{}, fmt.Errorf("get settings: %w", err)
	}

	if err := validateAppProject(projectResp, clusterResp, projectClusters.Items, req.Application); err != nil {
		return Result{}, err
	}

	repoAddress, tlsConfig, err := reposerver.ParseURL(req.RepoServerURL)
	if err != nil {
		return Result{}, err
	}
	repoServerConn, repoClient, err := reposerver.Connect(ctx, repoAddress, tlsConfig)
	if err != nil {
		return Result{}, fmt.Errorf("connect to repo server: %w", err)
	}
	defer closeQuietly(repoServerConn)

	liveResources, err := resolveLiveResources(ctx, clients.application, req)
	if err != nil {
		return Result{}, err
	}
	targets, err := resolveDesiredObjects(ctx, targetParams{
		App:             req.Application,
		Project:         projectResp,
		Cluster:         clusterResp,
		Settings:        settingsResp,
		RepoClient:      clients.repository,
		RepoServer:      repoClient,
		RepoCredentials: req.RepoCredentials,
		HardRefresh:     req.HardRefresh,
	})
	if err != nil {
		return Result{}, err
	}

	return compare(CompareRequest{
		Application:    req.Application,
		Settings:       settingsResp,
		LiveResources:  liveResources,
		DesiredObjects: targets,
	}, printer)
}

type apiClients struct {
	application applicationpkg.ApplicationServiceClient
	project     projectpkg.ProjectServiceClient
	cluster     clusterpkg.ClusterServiceClient
	settings    settingspkg.SettingsServiceClient
	repository  repositorypkg.RepositoryServiceClient
	closers     []io.Closer
}

func openAPIClients(apiClient argocdclient.Client) (result *apiClients, err error) {
	var closers []io.Closer
	defer func() {
		if err != nil {
			for _, c := range closers {
				closeQuietly(c)
			}
		}
	}()

	appConn, appIf, err := apiClient.NewApplicationClient()
	if err != nil {
		return nil, fmt.Errorf("create application client: %w", err)
	}
	closers = append(closers, appConn)

	projectConn, projectIf, err := apiClient.NewProjectClient()
	if err != nil {
		return nil, fmt.Errorf("create project client: %w", err)
	}
	closers = append(closers, projectConn)

	clusterConn, clusterIf, err := apiClient.NewClusterClient()
	if err != nil {
		return nil, fmt.Errorf("create cluster client: %w", err)
	}
	closers = append(closers, clusterConn)

	settingsConn, settingsIf, err := apiClient.NewSettingsClient()
	if err != nil {
		return nil, fmt.Errorf("create settings client: %w", err)
	}
	closers = append(closers, settingsConn)

	repoConn, repoIf, err := apiClient.NewRepoClient()
	if err != nil {
		return nil, fmt.Errorf("create repository client: %w", err)
	}
	closers = append(closers, repoConn)

	return &apiClients{
		application: appIf,
		project:     projectIf,
		cluster:     clusterIf,
		settings:    settingsIf,
		repository:  repoIf,
		closers:     closers,
	}, nil
}

func (c *apiClients) Close() {
	for _, closer := range c.closers {
		closeQuietly(closer)
	}
}

func loadProject(
	ctx context.Context,
	projectIf projectpkg.ProjectServiceClient,
	app *argoappv1.Application,
) (*argoappv1.AppProject, error) {
	projectName := app.Spec.Project
	projectResp, err := projectIf.Get(ctx, &projectpkg.ProjectQuery{Name: projectName})
	if err != nil {
		return nil, fmt.Errorf("get project %q: %w", projectName, err)
	}
	return projectResp, nil
}

func loadDestinationCluster(
	ctx context.Context,
	clusterIf clusterpkg.ClusterServiceClient,
	app *argoappv1.Application,
) (*argoappv1.Cluster, error) {
	clusterResp, err := clusterIf.Get(ctx, &clusterpkg.ClusterQuery{
		Name:   app.Spec.Destination.Name,
		Server: app.Spec.Destination.Server,
	})
	if err != nil {
		return nil, fmt.Errorf("get destination cluster: %w", err)
	}
	return clusterResp, nil
}

func hasApplicationSources(app *argoappv1.Application) bool {
	if len(app.Spec.GetSources()) > 0 {
		return true
	}
	return app.Spec.GetSource().RepoURL != ""
}

func closeQuietly(closer io.Closer) {
	if closer != nil {
		_ = closer.Close()
	}
}
