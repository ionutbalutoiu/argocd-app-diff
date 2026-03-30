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
