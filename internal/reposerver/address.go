package reposerver

import (
	"fmt"
	"net/url"
	"os"

	repoapiclient "github.com/argoproj/argo-cd/v3/reposerver/apiclient"
)

// ParseURL resolves the repo-server address and connection settings from CLI input.
func ParseURL(input string) (string, repoapiclient.TLSConfiguration, error) {
	if input == "" {
		input = os.Getenv("ARGOCD_REPO_SERVER_URL")
	}
	if input == "" {
		return "", repoapiclient.TLSConfiguration{}, fmt.Errorf("repo server URL is required via --repo-server-url or ARGOCD_REPO_SERVER_URL")
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return "", repoapiclient.TLSConfiguration{}, fmt.Errorf("parse repo server URL: %w", err)
	}
	if parsed.Scheme != "grpc" && parsed.Scheme != "grpcs" {
		return "", repoapiclient.TLSConfiguration{}, fmt.Errorf("repo server URL must use grpc:// or grpcs://")
	}
	if parsed.Host == "" {
		return "", repoapiclient.TLSConfiguration{}, fmt.Errorf("repo server URL is missing host")
	}

	return parsed.Host, repoapiclient.TLSConfiguration{
		DisableTLS:       parsed.Scheme == "grpc",
		StrictValidation: false,
	}, nil
}
