package appdiff

import (
	"context"
	"encoding/json"
	"fmt"

	applicationpkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func resolveLiveResources(
	ctx context.Context,
	appIf applicationpkg.ApplicationServiceClient,
	req Request,
) ([]liveResource, error) {
	resources, err := getManagedResources(ctx, appIf, req)
	if err != nil {
		return nil, err
	}

	liveResources, err := decodeLiveResources(resources.Items)
	if err != nil {
		return nil, fmt.Errorf("decode live objects: %w", err)
	}

	return liveResources, nil
}

func decodeLiveResources(resources []*argoappv1.ResourceDiff) ([]liveResource, error) {
	decoded := make([]liveResource, 0, len(resources))
	for _, res := range resources {
		live, err := decodeResourceState(res.NormalizedLiveState)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, liveResource{
			key: kube.ResourceKey{
				Name:      res.Name,
				Namespace: res.Namespace,
				Group:     res.Group,
				Kind:      res.Kind,
			},
			live: live,
		})
	}
	return decoded, nil
}

func decodeResourceState(state string) (*unstructured.Unstructured, error) {
	if state == "" || state == "null" {
		return nil, nil
	}

	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal([]byte(state), obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func getManagedResources(
	ctx context.Context,
	appIf applicationpkg.ApplicationServiceClient,
	req Request,
) (*applicationpkg.ManagedResourcesResponse, error) {
	appName := req.Application.Name
	projectName := req.Application.Spec.Project
	refreshType := getRefreshType(req.Refresh, req.HardRefresh)

	_, err := appIf.Get(ctx, &applicationpkg.ApplicationQuery{
		Name:         &appName,
		AppNamespace: stringPtr(req.LiveNamespace),
		Refresh:      refreshType,
		Project:      []string{projectName},
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return &applicationpkg.ManagedResourcesResponse{}, nil
		}
		return nil, fmt.Errorf("get live application %q: %w", appName, err)
	}

	resources, err := appIf.ManagedResources(ctx, &applicationpkg.ResourcesQuery{
		ApplicationName: &appName,
		AppNamespace:    stringPtr(req.LiveNamespace),
		Project:         &projectName,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return &applicationpkg.ManagedResourcesResponse{}, nil
		}
		return nil, fmt.Errorf("get managed resources for %q: %w", appName, err)
	}
	return resources, nil
}

func getRefreshType(refresh bool, hardRefresh bool) *string {
	if hardRefresh {
		value := string(argoappv1.RefreshTypeHard)
		return &value
	}
	if refresh {
		value := string(argoappv1.RefreshTypeNormal)
		return &value
	}
	return nil
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
