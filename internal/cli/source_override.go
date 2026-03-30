package cli

import (
	"fmt"
	"strings"
)

const sourceRevisionOverrideFlag = "source-revision-override"

func parseSourceOverrides(values []string) (map[string]string, error) {
	overrides := make(map[string]string, len(values))
	for _, value := range values {
		repoURL, revision, err := parseSourceOverride(value)
		if err != nil {
			return nil, err
		}
		if _, exists := overrides[repoURL]; exists {
			return nil, fmt.Errorf("duplicate source override for repository %q", repoURL)
		}
		overrides[repoURL] = revision
	}
	return overrides, nil
}

func parseSourceOverride(value string) (string, string, error) {
	repoURL, revision, ok := strings.Cut(value, "|")
	if !ok {
		return "", "", fmt.Errorf("invalid --%s value %q: expected <REPO_URL>|<NEW_REVISION>", sourceRevisionOverrideFlag, value)
	}

	repoURL = strings.TrimSpace(repoURL)
	revision = strings.TrimSpace(revision)
	if repoURL == "" || revision == "" {
		return "", "", fmt.Errorf("invalid --%s value %q: expected non-empty repo URL and revision", sourceRevisionOverrideFlag, value)
	}
	return repoURL, revision, nil
}
