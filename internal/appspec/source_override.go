package appspec

import (
	"errors"
	"fmt"

	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// ApplySourceRevisionOverrides updates application source revisions keyed by repo URL.
func ApplySourceRevisionOverrides(app *argoappv1.Application, overrides map[string]string) error {
	if app == nil {
		return errors.New("application is required")
	}
	if len(overrides) == 0 {
		return nil
	}

	matched := make(map[string]struct{}, len(overrides))
	if app.Spec.HasMultipleSources() {
		for i := range app.Spec.Sources {
			revision, ok := overrides[app.Spec.Sources[i].RepoURL]
			if !ok {
				continue
			}
			app.Spec.Sources[i].TargetRevision = revision
			matched[app.Spec.Sources[i].RepoURL] = struct{}{}
		}
	} else if app.Spec.Source != nil {
		if revision, ok := overrides[app.Spec.Source.RepoURL]; ok {
			app.Spec.Source.TargetRevision = revision
			matched[app.Spec.Source.RepoURL] = struct{}{}
		}
	}

	for repoURL := range overrides {
		if _, ok := matched[repoURL]; !ok {
			return fmt.Errorf("source override for repository %q did not match any application source", repoURL)
		}
	}
	return nil
}
