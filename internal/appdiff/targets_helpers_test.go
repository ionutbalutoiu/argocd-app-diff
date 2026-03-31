package appdiff

import (
	"context"
	"fmt"

	repositorypkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/repository"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	repoapiclient "github.com/argoproj/argo-cd/v3/reposerver/apiclient"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeRepositoryServiceClient struct {
	getRepoFunc          func(context.Context, string, string) (*argoappv1.Repository, error)
	listRepositoriesFunc func(context.Context) (*argoappv1.RepositoryList, error)
}

func (f fakeRepositoryServiceClient) List(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*argoappv1.RepositoryList, error) {
	panic("unexpected call to List")
}

func (f fakeRepositoryServiceClient) Get(ctx context.Context, query *repositorypkg.RepoQuery, _ ...grpc.CallOption) (*argoappv1.Repository, error) {
	if f.getRepoFunc == nil {
		panic("unexpected call to Get")
	}
	return f.getRepoFunc(ctx, query.Repo, query.AppProject)
}

func (f fakeRepositoryServiceClient) GetWrite(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*argoappv1.Repository, error) {
	panic("unexpected call to GetWrite")
}

func (f fakeRepositoryServiceClient) ListRepositories(ctx context.Context, _ *repositorypkg.RepoQuery, _ ...grpc.CallOption) (*argoappv1.RepositoryList, error) {
	if f.listRepositoriesFunc != nil {
		return f.listRepositoriesFunc(ctx)
	}
	return &argoappv1.RepositoryList{}, nil
}

func (f fakeRepositoryServiceClient) ListWriteRepositories(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*argoappv1.RepositoryList, error) {
	panic("unexpected call to ListWriteRepositories")
}

func (f fakeRepositoryServiceClient) ListRefs(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*repoapiclient.Refs, error) {
	panic("unexpected call to ListRefs")
}

func (f fakeRepositoryServiceClient) ListOCITags(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*repoapiclient.Refs, error) {
	panic("unexpected call to ListOCITags")
}

func (f fakeRepositoryServiceClient) ListApps(context.Context, *repositorypkg.RepoAppsQuery, ...grpc.CallOption) (*repositorypkg.RepoAppsResponse, error) {
	panic("unexpected call to ListApps")
}

func (f fakeRepositoryServiceClient) GetAppDetails(context.Context, *repositorypkg.RepoAppDetailsQuery, ...grpc.CallOption) (*repoapiclient.RepoAppDetailsResponse, error) {
	panic("unexpected call to GetAppDetails")
}

func (f fakeRepositoryServiceClient) GetHelmCharts(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*repoapiclient.HelmChartsResponse, error) {
	panic("unexpected call to GetHelmCharts")
}

func (f fakeRepositoryServiceClient) Create(context.Context, *repositorypkg.RepoCreateRequest, ...grpc.CallOption) (*argoappv1.Repository, error) {
	panic("unexpected call to Create")
}

func (f fakeRepositoryServiceClient) CreateRepository(context.Context, *repositorypkg.RepoCreateRequest, ...grpc.CallOption) (*argoappv1.Repository, error) {
	panic("unexpected call to CreateRepository")
}

func (f fakeRepositoryServiceClient) CreateWriteRepository(context.Context, *repositorypkg.RepoCreateRequest, ...grpc.CallOption) (*argoappv1.Repository, error) {
	panic("unexpected call to CreateWriteRepository")
}

func (f fakeRepositoryServiceClient) Update(context.Context, *repositorypkg.RepoUpdateRequest, ...grpc.CallOption) (*argoappv1.Repository, error) {
	panic("unexpected call to Update")
}

func (f fakeRepositoryServiceClient) UpdateRepository(context.Context, *repositorypkg.RepoUpdateRequest, ...grpc.CallOption) (*argoappv1.Repository, error) {
	panic("unexpected call to UpdateRepository")
}

func (f fakeRepositoryServiceClient) UpdateWriteRepository(context.Context, *repositorypkg.RepoUpdateRequest, ...grpc.CallOption) (*argoappv1.Repository, error) {
	panic("unexpected call to UpdateWriteRepository")
}

func (f fakeRepositoryServiceClient) Delete(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*repositorypkg.RepoResponse, error) {
	panic("unexpected call to Delete")
}

func (f fakeRepositoryServiceClient) DeleteRepository(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*repositorypkg.RepoResponse, error) {
	panic("unexpected call to DeleteRepository")
}

func (f fakeRepositoryServiceClient) DeleteWriteRepository(context.Context, *repositorypkg.RepoQuery, ...grpc.CallOption) (*repositorypkg.RepoResponse, error) {
	panic("unexpected call to DeleteWriteRepository")
}

func (f fakeRepositoryServiceClient) ValidateAccess(context.Context, *repositorypkg.RepoAccessQuery, ...grpc.CallOption) (*repositorypkg.RepoResponse, error) {
	panic("unexpected call to ValidateAccess")
}

func (f fakeRepositoryServiceClient) ValidateWriteAccess(context.Context, *repositorypkg.RepoAccessQuery, ...grpc.CallOption) (*repositorypkg.RepoResponse, error) {
	panic("unexpected call to ValidateWriteAccess")
}

func configMapManifest(name string) string {
	return fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"%s"},"data":{"key":"value"}}`, name)
}

type fakeRepoServerServiceClient struct {
	generateManifestFunc func(context.Context, *repoapiclient.ManifestRequest, ...grpc.CallOption) (*repoapiclient.ManifestResponse, error)
}

func (f fakeRepoServerServiceClient) GenerateManifest(ctx context.Context, req *repoapiclient.ManifestRequest, opts ...grpc.CallOption) (*repoapiclient.ManifestResponse, error) {
	if f.generateManifestFunc == nil {
		panic("unexpected call to GenerateManifest")
	}
	return f.generateManifestFunc(ctx, req, opts...)
}

func (f fakeRepoServerServiceClient) GenerateManifestWithFiles(context.Context, ...grpc.CallOption) (repoapiclient.RepoServerService_GenerateManifestWithFilesClient, error) {
	panic("unexpected call to GenerateManifestWithFiles")
}

func (f fakeRepoServerServiceClient) TestRepository(context.Context, *repoapiclient.TestRepositoryRequest, ...grpc.CallOption) (*repoapiclient.TestRepositoryResponse, error) {
	panic("unexpected call to TestRepository")
}

func (f fakeRepoServerServiceClient) ResolveRevision(context.Context, *repoapiclient.ResolveRevisionRequest, ...grpc.CallOption) (*repoapiclient.ResolveRevisionResponse, error) {
	panic("unexpected call to ResolveRevision")
}

func (f fakeRepoServerServiceClient) ListRefs(context.Context, *repoapiclient.ListRefsRequest, ...grpc.CallOption) (*repoapiclient.Refs, error) {
	panic("unexpected call to ListRefs")
}

func (f fakeRepoServerServiceClient) ListOCITags(context.Context, *repoapiclient.ListRefsRequest, ...grpc.CallOption) (*repoapiclient.Refs, error) {
	panic("unexpected call to ListOCITags")
}

func (f fakeRepoServerServiceClient) ListApps(context.Context, *repoapiclient.ListAppsRequest, ...grpc.CallOption) (*repoapiclient.AppList, error) {
	panic("unexpected call to ListApps")
}

func (f fakeRepoServerServiceClient) ListPlugins(context.Context, *emptypb.Empty, ...grpc.CallOption) (*repoapiclient.PluginList, error) {
	panic("unexpected call to ListPlugins")
}

func (f fakeRepoServerServiceClient) GetAppDetails(context.Context, *repoapiclient.RepoServerAppDetailsQuery, ...grpc.CallOption) (*repoapiclient.RepoAppDetailsResponse, error) {
	panic("unexpected call to GetAppDetails")
}

func (f fakeRepoServerServiceClient) GetRevisionMetadata(context.Context, *repoapiclient.RepoServerRevisionMetadataRequest, ...grpc.CallOption) (*argoappv1.RevisionMetadata, error) {
	panic("unexpected call to GetRevisionMetadata")
}

func (f fakeRepoServerServiceClient) GetOCIMetadata(context.Context, *repoapiclient.RepoServerRevisionChartDetailsRequest, ...grpc.CallOption) (*argoappv1.OCIMetadata, error) {
	panic("unexpected call to GetOCIMetadata")
}

func (f fakeRepoServerServiceClient) GetRevisionChartDetails(context.Context, *repoapiclient.RepoServerRevisionChartDetailsRequest, ...grpc.CallOption) (*argoappv1.ChartDetails, error) {
	panic("unexpected call to GetRevisionChartDetails")
}

func (f fakeRepoServerServiceClient) GetHelmCharts(context.Context, *repoapiclient.HelmChartsRequest, ...grpc.CallOption) (*repoapiclient.HelmChartsResponse, error) {
	panic("unexpected call to GetHelmCharts")
}

func (f fakeRepoServerServiceClient) GetGitFiles(context.Context, *repoapiclient.GitFilesRequest, ...grpc.CallOption) (*repoapiclient.GitFilesResponse, error) {
	panic("unexpected call to GetGitFiles")
}

func (f fakeRepoServerServiceClient) GetGitDirectories(context.Context, *repoapiclient.GitDirectoriesRequest, ...grpc.CallOption) (*repoapiclient.GitDirectoriesResponse, error) {
	panic("unexpected call to GetGitDirectories")
}

func (f fakeRepoServerServiceClient) UpdateRevisionForPaths(context.Context, *repoapiclient.UpdateRevisionForPathsRequest, ...grpc.CallOption) (*repoapiclient.UpdateRevisionForPathsResponse, error) {
	panic("unexpected call to UpdateRevisionForPaths")
}
