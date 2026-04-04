package appdiff

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"

	enginediff "github.com/argoproj/gitops-engine/pkg/diff"
	"github.com/argoproj/gitops-engine/pkg/sync/hook"
	"github.com/argoproj/gitops-engine/pkg/sync/ignore"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"

	settingspkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/settings"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v3/util/argo"
	argodiff "github.com/argoproj/argo-cd/v3/util/argo/diff"
	"github.com/argoproj/argo-cd/v3/util/argo/normalizers"
	argocdcli "github.com/argoproj/argo-cd/v3/util/cli"
	logutils "github.com/argoproj/argo-cd/v3/util/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// compareRequest describes a diff run over already-resolved live and desired state.
type compareRequest struct {
	application    *argoappv1.Application
	settings       *settingspkg.Settings
	liveResources  []liveResource
	desiredObjects []*unstructured.Unstructured
}

// liveResource identifies one managed live resource and its normalized live state.
type liveResource struct {
	key  kube.ResourceKey
	live *unstructured.Unstructured
}

type diffPrinter interface {
	Print(group, kind, namespace, name string, live, target *unstructured.Unstructured) error
}

type stdoutPrinter struct{}

// Print renders a single resource diff to stdout.
func (stdoutPrinter) Print(group, kind, namespace, name string, live, target *unstructured.Unstructured) error {
	return printResourceDiff(group, kind, namespace, name, live, target)
}

type diffItem struct {
	key    kube.ResourceKey
	live   *unstructured.Unstructured
	target *unstructured.Unstructured
}

// compare validates the request, prepares diff inputs, and prints each diff item.
func compare(req compareRequest, printer diffPrinter) (Result, error) {
	if req.application == nil {
		return Result{}, fmt.Errorf("application is required")
	}
	if req.settings == nil {
		return Result{}, fmt.Errorf("settings are required")
	}

	groupedTargets := groupObjsByKey(cloneObjects(req.desiredObjects), req.liveResources, req.application.Spec.Destination.Namespace)
	items, err := groupObjsForDiff(
		req.liveResources,
		groupedTargets,
		req.settings,
		req.application.InstanceName(req.settings.ControllerNamespace),
		req.application.Spec.Destination.Namespace,
	)
	if err != nil {
		return Result{}, fmt.Errorf("prepare diff items: %w", err)
	}

	diffConfig, err := buildDiffConfig(req.application, req.settings)
	if err != nil {
		return Result{}, err
	}

	foundDiffs, err := printDiffItems(items, diffConfig, printer)
	if err != nil {
		return Result{}, err
	}

	return Result{FoundDiffs: foundDiffs}, nil
}

func cloneObjects(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
	clones := make([]*unstructured.Unstructured, 0, len(objs))
	for _, obj := range objs {
		if obj == nil {
			clones = append(clones, nil)
			continue
		}
		clones = append(clones, obj.DeepCopy())
	}
	return clones
}

// groupObjsByKey normalizes desired objects and indexes diffable targets by resource key.
func groupObjsByKey(localObjs []*unstructured.Unstructured, liveObjs []liveResource, appNamespace string) map[kube.ResourceKey]*unstructured.Unstructured {
	namespacedByGk := make(map[schema.GroupKind]bool)
	for _, liveRes := range liveObjs {
		if liveRes.live == nil {
			continue
		}
		key := kube.GetResourceKey(liveRes.live)
		namespacedByGk[schema.GroupKind{Group: key.Group, Kind: key.Kind}] = key.Namespace != ""
	}

	localObjs = deduplicateTargetObjects(appNamespace, localObjs, namespacedByGk)

	objByKey := make(map[kube.ResourceKey]*unstructured.Unstructured, len(localObjs))
	for _, obj := range localObjs {
		if hook.IsHook(obj) || ignore.Ignore(obj) {
			continue
		}
		objByKey[kube.GetResourceKey(obj)] = obj
	}
	return objByKey
}

// deduplicateTargetObjects normalizes target identity and keeps only the last
// desired object for each logical resource key.
func deduplicateTargetObjects(appNamespace string, objs []*unstructured.Unstructured, namespacedByGk map[schema.GroupKind]bool) []*unstructured.Unstructured {
	targetByKey := make(map[kube.ResourceKey][]*unstructured.Unstructured)

	for i, obj := range objs {
		if obj == nil {
			continue
		}

		// Normalize namespace handling before computing the key so equivalent
		// targets collapse to the same entry.
		normalizeTargetNamespace(obj, appNamespace, namespacedByGk)

		key := kube.GetResourceKey(obj)
		if key.Name == "" && obj.GetGenerateName() != "" {
			// generateName objects do not have a stable name yet, so synthesize
			// one to avoid merging distinct entries under an empty-name key.
			key.Name = fmt.Sprintf("%s%d", obj.GetGenerateName(), i)
		}
		targetByKey[key] = append(targetByKey[key], obj)
	}

	result := make([]*unstructured.Unstructured, 0, len(targetByKey))
	for _, targets := range targetByKey {
		// Preserve the last manifest for a duplicated resource, matching the
		// later overwrite behavior used when grouping by key.
		result = append(result, targets[len(targets)-1])
	}
	return result
}

// normalizeTargetNamespace aligns target namespaces with the live resource scope before keying.
func normalizeTargetNamespace(obj *unstructured.Unstructured, appNamespace string, namespacedByGk map[schema.GroupKind]bool) {
	isNamespaced, ok := namespacedByGk[obj.GroupVersionKind().GroupKind()]
	if !ok {
		// Preserve explicit namespaces for kinds with no live counterpart. We cannot safely infer
		// whether an unseen kind is namespaced, and stripping the namespace can merge distinct
		// resources from different sources into one diff entry.
		return
	}
	if !isNamespaced {
		obj.SetNamespace("")
		return
	}
	if obj.GetNamespace() == "" {
		obj.SetNamespace(appNamespace)
	}
}

// groupObjsForDiff pairs live and desired objects into the diff items to evaluate.
func groupObjsForDiff(resources []liveResource, objs map[kube.ResourceKey]*unstructured.Unstructured, argoSettings *settingspkg.Settings, appName, namespace string) ([]diffItem, error) {
	resourceTracking := argo.NewResourceTracking()
	items := make([]diffItem, 0, len(resources)+len(objs))

	for _, liveRes := range resources {
		key := liveRes.key
		local, ok := objs[key]
		if !ok && liveRes.live == nil {
			continue
		}
		if local != nil && !kube.IsCRD(local) {
			if err := resourceTracking.SetAppInstance(local, argoSettings.AppLabelKey, appName, namespace, argoappv1.TrackingMethod(argoSettings.GetTrackingMethod()), argoSettings.GetInstallationID()); err != nil {
				return nil, err
			}
		}

		items = append(items, diffItem{key: key, live: liveRes.live, target: local})
		delete(objs, key)
	}

	for key, local := range objs {
		items = append(items, diffItem{key: key, target: local})
	}
	return items, nil
}

// isSecret reports whether the key refers to a core Secret resource.
func isSecret(key kube.ResourceKey) bool {
	return key.Kind == kube.SecretKind && key.Group == ""
}

// buildDiffConfig constructs the Argo CD diff configuration for the comparison run.
func buildDiffConfig(app *argoappv1.Application, argoSettings *settingspkg.Settings) (argodiff.DiffConfig, error) {
	diffConfig, err := argodiff.NewDiffConfigBuilder().
		WithDiffSettings(app.Spec.IgnoreDifferences, resourceOverrides(argoSettings.ResourceOverrides), false, normalizers.IgnoreNormalizerOpts{}).
		WithTracking(argoSettings.AppLabelKey, argoSettings.TrackingMethod).
		WithNoCache().
		WithLogger(logutils.NewLogrusLogger(logutils.NewWithCurrentConfig())).
		Build()
	if err != nil {
		return nil, fmt.Errorf("build diff config: %w", err)
	}

	return diffConfig, nil
}

// resourceOverrides dereferences configured overrides while skipping nil entries.
func resourceOverrides(overrides map[string]*argoappv1.ResourceOverride) map[string]argoappv1.ResourceOverride {
	result := make(map[string]argoappv1.ResourceOverride, len(overrides))
	for key, override := range overrides {
		if override == nil {
			continue
		}
		result[key] = *override
	}
	return result
}

// printDiffItems evaluates each diff item and prints the ones that should be shown.
func printDiffItems(items []diffItem, diffConfig argodiff.DiffConfig, printer diffPrinter) (bool, error) {
	slices.SortFunc(items, compareDiffItems)

	foundDiffs := false
	for _, item := range items {
		// Skip hooks: target hooks were already filtered in groupObjsByKey,
		// but live objects may also be hooks that have no local counterpart.
		if (item.target != nil && hook.IsHook(item.target)) || (item.live != nil && hook.IsHook(item.live)) {
			continue
		}

		diffItem, err := redactDiffItem(item)
		if err != nil {
			return false, fmt.Errorf("redact diff %s/%s: %w", item.key.Kind, item.key.Name, err)
		}

		diffRes, err := argodiff.StateDiff(diffItem.live, diffItem.target, diffConfig)
		if err != nil {
			return false, fmt.Errorf("diff %s/%s: %w", item.key.Kind, item.key.Name, err)
		}
		if !diffRes.Modified && diffItem.target != nil && diffItem.live != nil {
			continue
		}

		live, target, err := resolveDiffDisplayObjects(diffItem, diffRes.PredictedLive)
		if err != nil {
			return false, err
		}

		foundDiffs = true
		if err := printer.Print(item.key.Group, item.key.Kind, item.key.Namespace, item.key.Name, live, target); err != nil {
			return false, fmt.Errorf("print diff %s/%s: %w", item.key.Kind, item.key.Name, err)
		}
	}

	return foundDiffs, nil
}

// redactDiffItem returns a sanitized copy of a core Secret diff item before it is rendered.
func redactDiffItem(item diffItem) (diffItem, error) {
	if !isSecret(item.key) {
		return item, nil
	}

	target, live, err := enginediff.HideSecretData(item.target, item.live, nil)
	if err != nil {
		return diffItem{}, err
	}

	return diffItem{
		key:    item.key,
		live:   live,
		target: target,
	}, nil
}

func compareDiffItems(left diffItem, right diffItem) int {
	if diff := cmp.Compare(left.key.Group, right.key.Group); diff != 0 {
		return diff
	}
	if diff := cmp.Compare(left.key.Kind, right.key.Kind); diff != 0 {
		return diff
	}
	if diff := cmp.Compare(left.key.Namespace, right.key.Namespace); diff != 0 {
		return diff
	}
	return cmp.Compare(left.key.Name, right.key.Name)
}

// resolveDiffDisplayObjects returns the objects to display in the diff output.
// When both live and target exist, the "target" side is replaced with the
// predicted live state (what the cluster would look like after applying),
// which produces a more accurate diff for the user.
func resolveDiffDisplayObjects(item diffItem, predictedLive []byte) (*unstructured.Unstructured, *unstructured.Unstructured, error) {
	if item.target == nil || item.live == nil {
		return item.live, item.target, nil
	}
	if len(predictedLive) == 0 {
		return item.live, item.target, nil
	}

	predicted := &unstructured.Unstructured{}
	if err := json.Unmarshal(predictedLive, predicted); err != nil {
		return nil, nil, fmt.Errorf("decode predicted live state for %s/%s: %w", item.key.Kind, item.key.Name, err)
	}
	return item.live, predicted, nil
}

// printResourceDiff renders one resource diff using the Argo CD CLI diff formatter.
func printResourceDiff(group, kind, namespace, name string, live, target *unstructured.Unstructured) error {
	fmt.Printf("\n===== %s/%s %s/%s =====\n", group, kind, namespace, name)
	err := argocdcli.PrintDiff(name, live, target)
	if isExpectedDiffExit(err) {
		return nil
	}
	return err
}

// isExpectedDiffExit reports whether diff returned exit status 1 to signal differences.
func isExpectedDiffExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}
