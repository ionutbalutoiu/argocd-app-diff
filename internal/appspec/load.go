package appspec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"k8s.io/apimachinery/pkg/util/yaml"

	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

const (
	applicationAPIVersion = "argoproj.io/v1alpha1"
	applicationKind       = "Application"
	defaultProjectName    = "default"
	yamlDecoderBufferSize = 4096
)

// Load reads a single Argo CD Application manifest from disk.
func Load(path string) (*argoappv1.Application, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read application file: %w", err)
	}

	document, err := decodeSingleDocument(path, data)
	if err != nil {
		return nil, err
	}

	if err := validateApplicationDocument(path, document); err != nil {
		return nil, err
	}

	app, err := unmarshalApplication(document)
	if err != nil {
		return nil, err
	}
	if app.Name == "" {
		return nil, fmt.Errorf("application in %q is missing metadata.name", path)
	}

	defaultProject(app)
	return app, nil
}

func decodeSingleDocument(path string, data []byte) (map[string]any, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), yamlDecoderBufferSize)

	var (
		document map[string]any
		count    int
	)
	for {
		var raw map[string]any
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode application file: %w", err)
		}
		if len(raw) == 0 {
			continue
		}

		document = raw
		count++
	}

	if count != 1 {
		return nil, fmt.Errorf("expected exactly one Kubernetes object in %q, got %d", path, count)
	}

	return document, nil
}

func validateApplicationDocument(path string, document map[string]any) error {
	apiVersion, _ := document["apiVersion"].(string)
	kind, _ := document["kind"].(string)
	if kind != applicationKind {
		return fmt.Errorf("expected kind %s in %q, got %q", applicationKind, path, kind)
	}
	if apiVersion != applicationAPIVersion {
		return fmt.Errorf("expected apiVersion %s in %q, got %q", applicationAPIVersion, path, apiVersion)
	}

	return nil
}

func unmarshalApplication(document map[string]any) (*argoappv1.Application, error) {
	rawJSON, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal application document: %w", err)
	}

	var app argoappv1.Application
	if err := json.Unmarshal(rawJSON, &app); err != nil {
		return nil, fmt.Errorf("unmarshal application: %w", err)
	}

	return &app, nil
}

func defaultProject(app *argoappv1.Application) {
	if app.Spec.Project == "" {
		app.Spec.Project = defaultProjectName
	}
}
