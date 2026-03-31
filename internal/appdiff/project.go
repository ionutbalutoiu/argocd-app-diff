package appdiff

import (
	"fmt"

	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

func validateAppProject(proj *argoappv1.AppProject, destCluster *argoappv1.Cluster, allClusters []argoappv1.Cluster, app *argoappv1.Application) error {
	for _, source := range app.Spec.GetSources() {
		if !proj.IsSourcePermitted(source) {
			return fmt.Errorf("source repository %q is not permitted by project %q", source.RepoURL, proj.Name)
		}
	}

	projectClustersFn := func(project string) ([]*argoappv1.Cluster, error) {
		var filtered []*argoappv1.Cluster
		for i := range allClusters {
			if allClusters[i].Project == project {
				filtered = append(filtered, &allClusters[i])
			}
		}
		return filtered, nil
	}
	permitted, err := proj.IsDestinationPermitted(destCluster, app.Spec.Destination.Namespace, projectClustersFn)
	if err != nil {
		return fmt.Errorf("validate destination against project %q: %w", proj.Name, err)
	}
	if !permitted {
		return fmt.Errorf("destination %q/%q is not permitted by project %q", destCluster.Name, destCluster.Server, proj.Name)
	}
	return nil
}
