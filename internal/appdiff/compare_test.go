package appdiff

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	settingspkg "github.com/argoproj/argo-cd/v3/pkg/apiclient/settings"
	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/utils/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPrintResourceDiffRespectsKubectlExternalDiff(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "external-diff.sh")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > " + shellQuote(outputPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write external diff script: %v", err)
	}

	t.Setenv("KUBECTL_EXTERNAL_DIFF", scriptPath+" --sentinel")

	live := &unstructured.Unstructured{}
	live.SetAPIVersion("v1")
	live.SetKind("ConfigMap")
	live.SetName("example")
	live.SetNamespace("default")
	live.Object["data"] = map[string]any{"key": "old"}

	target := live.DeepCopy()
	target.Object["data"] = map[string]any{"key": "new"}

	if err := printResourceDiff("", "ConfigMap", "default", "example", live, target); err != nil {
		t.Fatalf("printResourceDiff returned error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read external diff output: %v", err)
	}

	args := strings.Fields(string(data))
	if len(args) != 3 {
		t.Fatalf("expected 3 arguments, got %d: %q", len(args), string(data))
	}
	if args[0] != "--sentinel" {
		t.Fatalf("expected first arg to be sentinel flag, got %q", args[0])
	}
	if !strings.HasSuffix(args[1], "example-live.yaml") {
		t.Fatalf("expected live file path, got %q", args[1])
	}
	if !strings.HasSuffix(args[2], "example") {
		t.Fatalf("expected target file path, got %q", args[2])
	}
}

func TestPrintResourceDiffIgnoresDiffExitStatusOne(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "external-diff.sh")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write external diff script: %v", err)
	}

	t.Setenv("KUBECTL_EXTERNAL_DIFF", scriptPath)

	live := &unstructured.Unstructured{}
	live.SetAPIVersion("v1")
	live.SetKind("ConfigMap")
	live.SetName("example")
	live.SetNamespace("default")

	target := live.DeepCopy()
	target.Object["data"] = map[string]any{"key": "new"}

	if err := printResourceDiff("", "ConfigMap", "default", "example", live, target); err != nil {
		t.Fatalf("printResourceDiff returned error for diff exit code 1: %v", err)
	}
}

func TestPrintResourceDiffReturnsUnexpectedDiffExitStatus(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "external-diff.sh")
	script := "#!/bin/sh\nexit 2\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write external diff script: %v", err)
	}

	t.Setenv("KUBECTL_EXTERNAL_DIFF", scriptPath)

	live := &unstructured.Unstructured{}
	live.SetAPIVersion("v1")
	live.SetKind("ConfigMap")
	live.SetName("example")
	live.SetNamespace("default")

	target := live.DeepCopy()
	target.Object["data"] = map[string]any{"key": "new"}

	err := printResourceDiff("", "ConfigMap", "default", "example", live, target)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exit error, got %v", err)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("expected exit code 2, got %d", exitErr.ExitCode())
	}
}

func TestCompareReturnsPrinterError(t *testing.T) {
	t.Parallel()

	app := &argoappv1.Application{}
	app.SetName("example-app")
	app.Spec.Destination.Namespace = "default"

	live := &unstructured.Unstructured{}
	live.SetAPIVersion("v1")
	live.SetKind("ConfigMap")
	live.SetName("example")
	live.SetNamespace("default")
	live.Object["data"] = map[string]any{"key": "old"}

	target := live.DeepCopy()
	target.Object["data"] = map[string]any{"key": "new"}

	liveJSON, err := json.Marshal(live)
	if err != nil {
		t.Fatalf("marshal live object: %v", err)
	}

	expectedErr := errors.New("boom")

	_, err = compare(
		CompareRequest{
			Application: app,
			Settings: &settingspkg.Settings{
				AppLabelKey:       "argocd.argoproj.io/instance",
				ResourceOverrides: map[string]*argoappv1.ResourceOverride{},
			},
			LiveResources: []LiveResource{
				{
					Key: kube.ResourceKey{Name: "example", Namespace: "default", Group: "", Kind: "ConfigMap"},
					Live: func() *unstructured.Unstructured {
						obj := &unstructured.Unstructured{}
						if err := json.Unmarshal(liveJSON, obj); err != nil {
							t.Fatalf("unmarshal live object: %v", err)
						}
						return obj
					}(),
				},
			},
			DesiredObjects: []*unstructured.Unstructured{target},
		},
		failingPrinter{err: expectedErr},
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestCompareIncludesLocalOnlyTarget(t *testing.T) {
	t.Parallel()

	app := &argoappv1.Application{}
	app.SetName("example-app")
	app.Spec.Destination.Namespace = "default"

	target := &unstructured.Unstructured{}
	target.SetAPIVersion("v1")
	target.SetKind("ConfigMap")
	target.SetName("example")
	target.SetNamespace("default")
	target.Object["data"] = map[string]any{"key": "new"}

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application: app,
			Settings: &settingspkg.Settings{
				AppLabelKey:       "argocd.argoproj.io/instance",
				ResourceOverrides: map[string]*argoappv1.ResourceOverride{},
			},
			DesiredObjects: []*unstructured.Unstructured{target},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if !result.FoundDiffs {
		t.Fatal("expected diffs to be found")
	}
	if len(printer.calls) != 1 {
		t.Fatalf("expected 1 printed diff, got %d", len(printer.calls))
	}
	got := printer.calls[0]
	if got.kind != "ConfigMap" || got.namespace != "default" || got.name != "example" {
		t.Fatalf("unexpected printed diff: %#v", got)
	}
	if got.live != nil {
		t.Fatalf("expected no live object, got %#v", got.live)
	}
	if got.target == nil {
		t.Fatal("expected target object to be printed")
	}
}

func TestComparePrintsRedactedSecretDiff(t *testing.T) {
	t.Parallel()

	app := newTestApplication()
	labels := map[string]string{"argocd.argoproj.io/instance": "example-app"}

	live := newSecretObject("example", "default", map[string]string{"token": "super-secret-old"}, labels)
	target := newSecretObject("example", "default", map[string]string{"token": "super-secret-new"}, labels)

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application: app,
			Settings:    newTestSettings(),
			LiveResources: []LiveResource{
				{
					Key:  kube.GetResourceKey(live),
					Live: live,
				},
			},
			DesiredObjects: []*unstructured.Unstructured{target},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if !result.FoundDiffs {
		t.Fatal("expected diffs to be found")
	}
	if len(printer.calls) != 1 {
		t.Fatalf("expected 1 printed diff, got %d", len(printer.calls))
	}

	got := printer.calls[0]
	if got.kind != "Secret" || got.namespace != "default" || got.name != "example" {
		t.Fatalf("unexpected printed diff: %#v", got)
	}
	assertRedactedSecretObject(t, got.live, "super-secret-old", "super-secret-new")
	assertRedactedSecretObject(t, got.target, "super-secret-old", "super-secret-new")
}

func TestCompareSkipsUnchangedSecretDiff(t *testing.T) {
	t.Parallel()

	app := newTestApplication()
	labels := map[string]string{"argocd.argoproj.io/instance": "example-app"}

	live := newSecretObject("example", "default", map[string]string{"token": "super-secret"}, labels)
	target := newSecretObject("example", "default", map[string]string{"token": "super-secret"}, labels)

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application: app,
			Settings:    newTestSettings(),
			LiveResources: []LiveResource{
				{
					Key:  kube.GetResourceKey(live),
					Live: live,
				},
			},
			DesiredObjects: []*unstructured.Unstructured{target},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if result.FoundDiffs {
		t.Fatal("expected no diffs to be found")
	}
	if len(printer.calls) != 0 {
		t.Fatalf("expected no printed diffs, got %d", len(printer.calls))
	}
}

func TestComparePrintsRedactedLocalOnlySecret(t *testing.T) {
	t.Parallel()

	target := newSecretObject("example", "default", map[string]string{"token": "super-secret"}, nil)

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application:    newTestApplication(),
			Settings:       newTestSettings(),
			DesiredObjects: []*unstructured.Unstructured{target},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if !result.FoundDiffs {
		t.Fatal("expected diffs to be found")
	}
	if len(printer.calls) != 1 {
		t.Fatalf("expected 1 printed diff, got %d", len(printer.calls))
	}

	got := printer.calls[0]
	if got.live != nil {
		t.Fatalf("expected no live object, got %#v", got.live)
	}
	assertRedactedSecretObject(t, got.target, "super-secret")
}

func TestComparePrintsRedactedLiveOnlySecret(t *testing.T) {
	t.Parallel()

	live := newSecretObject("example", "default", map[string]string{"token": "super-secret"}, nil)

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application: newTestApplication(),
			Settings:    newTestSettings(),
			LiveResources: []LiveResource{
				{
					Key:  kube.GetResourceKey(live),
					Live: live,
				},
			},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if !result.FoundDiffs {
		t.Fatal("expected diffs to be found")
	}
	if len(printer.calls) != 1 {
		t.Fatalf("expected 1 printed diff, got %d", len(printer.calls))
	}

	got := printer.calls[0]
	assertRedactedSecretObject(t, got.live, "super-secret")
	if got.target != nil {
		t.Fatalf("expected no target object, got %#v", got.target)
	}
}

func TestCompareIncludesLocalOnlyNamespacedResourceWithoutLiveMatch(t *testing.T) {
	t.Parallel()

	app := &argoappv1.Application{}
	app.SetName("example-app")
	app.Spec.Destination.Namespace = "default"

	target := &unstructured.Unstructured{}
	target.SetAPIVersion("apps/v1")
	target.SetKind("Deployment")
	target.SetName("example")
	target.SetNamespace("default")
	target.Object["spec"] = map[string]any{
		"selector": map[string]any{
			"matchLabels": map[string]any{"app": "example"},
		},
		"template": map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{"app": "example"},
			},
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":  "example",
						"image": "nginx:latest",
					},
				},
			},
		},
	}

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application: app,
			Settings: &settingspkg.Settings{
				AppLabelKey:       "argocd.argoproj.io/instance",
				ResourceOverrides: map[string]*argoappv1.ResourceOverride{},
			},
			DesiredObjects: []*unstructured.Unstructured{target},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if !result.FoundDiffs {
		t.Fatal("expected diffs to be found")
	}
	if len(printer.calls) != 1 {
		t.Fatalf("expected 1 printed diff, got %d", len(printer.calls))
	}
	got := printer.calls[0]
	if got.kind != "Deployment" || got.namespace != "default" || got.name != "example" {
		t.Fatalf("unexpected printed diff: %#v", got)
	}
}

func TestCompareKeepsDistinctNamespacesForLocalOnlyTargets(t *testing.T) {
	t.Parallel()

	app := &argoappv1.Application{}
	app.SetName("example-app")
	app.Spec.Destination.Namespace = "default"

	targetA := &unstructured.Unstructured{}
	targetA.SetAPIVersion("v1")
	targetA.SetKind("ConfigMap")
	targetA.SetName("shared")
	targetA.SetNamespace("team-a")

	targetB := &unstructured.Unstructured{}
	targetB.SetAPIVersion("v1")
	targetB.SetKind("ConfigMap")
	targetB.SetName("shared")
	targetB.SetNamespace("team-b")

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application: app,
			Settings: &settingspkg.Settings{
				AppLabelKey:       "argocd.argoproj.io/instance",
				ResourceOverrides: map[string]*argoappv1.ResourceOverride{},
			},
			DesiredObjects: []*unstructured.Unstructured{targetA, targetB},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if !result.FoundDiffs {
		t.Fatal("expected diffs to be found")
	}
	if len(printer.calls) != 2 {
		t.Fatalf("expected 2 printed diffs, got %d", len(printer.calls))
	}

	namespaces := []string{printer.calls[0].namespace, printer.calls[1].namespace}
	if !slices.Contains(namespaces, "team-a") || !slices.Contains(namespaces, "team-b") {
		t.Fatalf("unexpected printed namespaces: %#v", namespaces)
	}
}

func TestCompareDoesNotMutateInputObjects(t *testing.T) {
	t.Parallel()

	app := &argoappv1.Application{}
	app.SetName("example-app")
	app.Spec.Destination.Namespace = "default"

	live := &unstructured.Unstructured{}
	live.SetAPIVersion("v1")
	live.SetKind("ConfigMap")
	live.SetName("example")
	live.SetNamespace("default")
	live.Object["data"] = map[string]any{"key": "old"}

	target := &unstructured.Unstructured{}
	target.SetAPIVersion("v1")
	target.SetKind("ConfigMap")
	target.SetName("example")
	target.Object["data"] = map[string]any{"key": "new"}

	original := target.DeepCopy()

	_, err := compare(
		CompareRequest{
			Application: app,
			Settings: &settingspkg.Settings{
				AppLabelKey:         "argocd.argoproj.io/instance",
				ControllerNamespace: "argocd",
				ResourceOverrides:   map[string]*argoappv1.ResourceOverride{},
			},
			LiveResources: []LiveResource{
				{
					Key:  kube.GetResourceKey(live),
					Live: live,
				},
			},
			DesiredObjects: []*unstructured.Unstructured{target},
		},
		&recordingPrinter{},
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}

	if target.GetNamespace() != original.GetNamespace() {
		t.Fatalf("expected namespace %q to remain unchanged, got %q", original.GetNamespace(), target.GetNamespace())
	}
	if _, ok := target.GetLabels()["argocd.argoproj.io/instance"]; ok {
		t.Fatalf("expected original target labels to remain unchanged, got %#v", target.GetLabels())
	}
}

func TestComparePrintsDiffsInStableOrder(t *testing.T) {
	t.Parallel()

	app := &argoappv1.Application{}
	app.SetName("example-app")
	app.Spec.Destination.Namespace = "default"

	targetB := &unstructured.Unstructured{}
	targetB.SetAPIVersion("v1")
	targetB.SetKind("ConfigMap")
	targetB.SetName("zeta")
	targetB.SetNamespace("default")

	targetA := &unstructured.Unstructured{}
	targetA.SetAPIVersion("v1")
	targetA.SetKind("ConfigMap")
	targetA.SetName("alpha")
	targetA.SetNamespace("default")

	printer := &recordingPrinter{}
	result, err := compare(
		CompareRequest{
			Application: app,
			Settings: &settingspkg.Settings{
				AppLabelKey:       "argocd.argoproj.io/instance",
				ResourceOverrides: map[string]*argoappv1.ResourceOverride{},
			},
			DesiredObjects: []*unstructured.Unstructured{targetB, targetA},
		},
		printer,
	)
	if err != nil {
		t.Fatalf("compare returned error: %v", err)
	}
	if !result.FoundDiffs {
		t.Fatal("expected diffs to be found")
	}
	if len(printer.calls) != 2 {
		t.Fatalf("expected 2 printed diffs, got %d", len(printer.calls))
	}
	if printer.calls[0].name != "alpha" || printer.calls[1].name != "zeta" {
		t.Fatalf("expected stable lexical order, got %#v", []string{printer.calls[0].name, printer.calls[1].name})
	}
}

type failingPrinter struct {
	err error
}

func (f failingPrinter) Print(string, string, string, string, *unstructured.Unstructured, *unstructured.Unstructured) error {
	return f.err
}

type recordingPrinter struct {
	calls []printCall
}

type printCall struct {
	group     string
	kind      string
	namespace string
	name      string
	live      *unstructured.Unstructured
	target    *unstructured.Unstructured
}

func (r *recordingPrinter) Print(group, kind, namespace, name string, live, target *unstructured.Unstructured) error {
	r.calls = append(r.calls, printCall{
		group:     group,
		kind:      kind,
		namespace: namespace,
		name:      name,
		live:      cloneObject(live),
		target:    cloneObject(target),
	})
	return nil
}

func cloneObject(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	return obj.DeepCopy()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func newTestApplication() *argoappv1.Application {
	app := &argoappv1.Application{}
	app.SetName("example-app")
	app.Spec.Destination.Namespace = "default"
	return app
}

func newTestSettings() *settingspkg.Settings {
	return &settingspkg.Settings{
		AppLabelKey:       "argocd.argoproj.io/instance",
		TrackingMethod:    string(argoappv1.TrackingMethodAnnotation),
		ResourceOverrides: map[string]*argoappv1.ResourceOverride{},
	}
}

func newSecretObject(name, namespace string, stringData map[string]string, labels map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("v1")
	obj.SetKind("Secret")
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.Object["type"] = "Opaque"

	if len(labels) > 0 {
		obj.SetLabels(labels)
	}

	if len(stringData) > 0 {
		values := make(map[string]any, len(stringData))
		for key, value := range stringData {
			values[key] = value
		}
		obj.Object["stringData"] = values
	}

	return obj
}

func assertRedactedSecretObject(t *testing.T, obj *unstructured.Unstructured, rawValues ...string) {
	t.Helper()

	if obj == nil {
		t.Fatal("expected secret object to be present")
	}

	data, found, err := unstructured.NestedMap(obj.Object, "data")
	if err != nil {
		t.Fatalf("read secret data: %v", err)
	}
	if !found || len(data) == 0 {
		t.Fatalf("expected redacted secret data, got %#v", obj.Object)
	}

	for key, value := range data {
		redacted, ok := value.(string)
		if !ok {
			t.Fatalf("expected string redaction for key %q, got %#v", key, value)
		}
		if redacted == "" || strings.Trim(redacted, "+") != "" {
			t.Fatalf("expected plus-only redaction for key %q, got %q", key, redacted)
		}
	}

	if _, found, err := unstructured.NestedMap(obj.Object, "stringData"); err != nil {
		t.Fatalf("read secret stringData: %v", err)
	} else if found {
		t.Fatalf("expected stringData to be normalized away, got %#v", obj.Object["stringData"])
	}

	payload, err := json.Marshal(obj.Object)
	if err != nil {
		t.Fatalf("marshal secret object: %v", err)
	}
	for _, rawValue := range rawValues {
		if strings.Contains(string(payload), rawValue) {
			t.Fatalf("expected secret value %q to be redacted from %#v", rawValue, obj.Object)
		}
	}
}
