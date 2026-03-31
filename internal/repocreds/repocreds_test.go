package repocreds

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

func TestLoadFromEnvEmpty(t *testing.T) {
	t.Setenv(EnvVarJSONName, "")
	t.Setenv(EnvVarJSONPathName, "")

	set, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if set != nil {
		t.Fatalf("expected nil set, got %#v", set)
	}
}

func TestLoadFromEnvPrefersInlineJSONOverPath(t *testing.T) {
	path := writeCredentialFile(t, `[{"repo":"https://github.com/example/from-file.git","username":"file-user","password":"file-pass"}]`)
	t.Setenv(EnvVarJSONName, `[{"repo":"https://github.com/example/from-env.git","username":"env-user","password":"env-pass"}]`)
	t.Setenv(EnvVarJSONPathName, path)

	set, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	repo, ok, err := set.MatchRepository("https://github.com/example/from-env")
	if err != nil {
		t.Fatalf("MatchRepository returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected inline JSON credentials to match")
	}
	if repo.Username != "env-user" {
		t.Fatalf("expected inline JSON credentials to win, got %q", repo.Username)
	}
}

func TestLoadFromEnvRejectsInvalidInlineJSONWithoutPathFallback(t *testing.T) {
	path := writeCredentialFile(t, `[{"repo":"https://github.com/example/from-file.git","username":"file-user","password":"file-pass"}]`)
	t.Setenv(EnvVarJSONName, `[`)
	t.Setenv(EnvVarJSONPathName, path)

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), EnvVarJSONName) {
		t.Fatalf("expected error to mention %q, got %v", EnvVarJSONName, err)
	}
}

func TestLoadFromEnvReadsJSONFromPath(t *testing.T) {
	path := writeCredentialFile(t, `[{"repo":"https://github.com/example/from-file.git","username":"file-user","password":"file-pass"}]`)
	t.Setenv(EnvVarJSONName, "")
	t.Setenv(EnvVarJSONPathName, path)

	set, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	repo, ok, err := set.MatchRepository("https://github.com/example/from-file.git")
	if err != nil {
		t.Fatalf("MatchRepository returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected file-backed credentials to match")
	}
	if repo.Username != "file-user" {
		t.Fatalf("expected file-backed credentials, got %q", repo.Username)
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	if _, err := Parse(`[`); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	t.Parallel()

	_, err := Parse(`[{"repo":"https://github.com/example/repo.git","username":"user","password":"pass","wat":"nope"}]`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `unknown field "wat"`) {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestParseDefaultsMissingMatchToExact(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[{"repo":"https://github.com/example/repo.git","username":"env-user","password":"env-pass"}]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repo, ok, err := set.MatchRepository("https://github.com/example/repo")
	if err != nil {
		t.Fatalf("MatchRepository returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected exact match, got none")
	}
	if repo.Username != "env-user" {
		t.Fatalf("expected default exact match to apply, got %q", repo.Username)
	}
}

func TestParseRejectsInvalidMatch(t *testing.T) {
	t.Parallel()

	_, err := Parse(`[{"match":"wild","repo":"https://github.com/example/repo.git","username":"user","password":"pass"}]`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseRejectsDuplicateExactEntries(t *testing.T) {
	t.Parallel()

	_, err := Parse(`[
		{"repo":"https://github.com/example/repo.git","username":"one","password":"one"},
		{"match":"exact","repo":" https://github.com/EXAMPLE/repo ","username":"two","password":"two"}
	]`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate exact entry") {
		t.Fatalf("expected duplicate exact error, got %v", err)
	}
}

func TestParseRejectsMixedAuthModes(t *testing.T) {
	t.Parallel()

	_, err := Parse(`[{"repo":"git@github.com:example/repo.git","username":"user","password":"pass","sshPrivateKey":"PRIVATE KEY"}]`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected mixed auth mode error, got %v", err)
	}
}

func TestParseRejectsMissingAuth(t *testing.T) {
	t.Parallel()

	_, err := Parse(`[{"repo":"https://github.com/example/repo.git"}]`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "either sshPrivateKey or username/password is required") {
		t.Fatalf("expected missing auth error, got %v", err)
	}
}

func TestParseRejectsPartialBasicAuth(t *testing.T) {
	t.Parallel()

	_, err := Parse(`[{"repo":"https://github.com/example/repo.git","username":"user"}]`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "username and password must both be provided") {
		t.Fatalf("expected partial basic auth error, got %v", err)
	}
}

func TestMatchRepositoryPrefersExactOverPrefix(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[
		{"match":"prefix","repo":"https://github.com/example","username":"prefix","password":"prefix-pass"},
		{"repo":"https://github.com/example/repo.git","username":"exact","password":"exact-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repo, ok, err := set.MatchRepository("https://github.com/example/repo")
	if err != nil {
		t.Fatalf("MatchRepository returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected a match, got none")
	}
	if repo.Username != "exact" {
		t.Fatalf("expected exact username, got %q", repo.Username)
	}
	if repo.Repo != "https://github.com/example/repo" {
		t.Fatalf("expected requested repo URL to be preserved, got %q", repo.Repo)
	}
}

func TestMatchRepositoryPrefersLongestPrefix(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[
		{"match":"prefix","repo":"https://github.com/example","username":"short","password":"short-pass"},
		{"match":"prefix","repo":"https://github.com/example/team","username":"long","password":"long-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repo, ok, err := set.MatchRepository("https://github.com/example/team/repo.git")
	if err != nil {
		t.Fatalf("MatchRepository returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected a match, got none")
	}
	if repo.Username != "long" {
		t.Fatalf("expected longest prefix to win, got %q", repo.Username)
	}
}

func TestMatchRepositoryRejectsAmbiguousPrefixMatches(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[
		{"match":"prefix","repo":"git@github.com:example","username":"one","password":"one"},
		{"match":"prefix","repo":"ssh://git@github.com/example","username":"two","password":"two"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if _, _, err := set.MatchRepository("git@github.com:example/repo.git"); err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
}

func TestMatchRepositoryNormalizesGitURLs(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[{"repo":"git@github.com:example/repo.git","sshPrivateKey":"PRIVATE KEY"}]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repo, ok, err := set.MatchRepository("ssh://git@github.com/example/repo")
	if err != nil {
		t.Fatalf("MatchRepository returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected a normalized match, got none")
	}
	if repo.SSHPrivateKey != "PRIVATE KEY" {
		t.Fatalf("expected SSH key to be preserved, got %q", repo.SSHPrivateKey)
	}
}

func TestHydrateRepositoryFillsMissingBasicAuth(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[{"repo":"https://github.com/example/repo.git","username":"env-user","password":"env-pass"}]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repo, hydrated, err := set.HydrateRepository(&argoappv1.Repository{
		Repo: "https://github.com/example/repo.git",
	})
	if err != nil {
		t.Fatalf("HydrateRepository returned error: %v", err)
	}
	if !hydrated {
		t.Fatal("expected repository to be hydrated")
	}
	if repo.Username != "env-user" || repo.Password != "env-pass" {
		t.Fatalf("expected credentials to be copied, got %#v", repo)
	}
	if !repo.InheritedCreds {
		t.Fatal("expected InheritedCreds to be true")
	}
}

func TestHydrateRepositoryFillsMissingSSHKey(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[{"repo":"git@github.com:example/repo.git","sshPrivateKey":"PRIVATE KEY"}]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	repo, hydrated, err := set.HydrateRepository(&argoappv1.Repository{
		Repo: "ssh://git@github.com/example/repo",
	})
	if err != nil {
		t.Fatalf("HydrateRepository returned error: %v", err)
	}
	if !hydrated {
		t.Fatal("expected repository to be hydrated")
	}
	if repo.SSHPrivateKey != "PRIVATE KEY" {
		t.Fatalf("expected SSH key to be copied, got %q", repo.SSHPrivateKey)
	}
}

func TestHydrateRepositoryDoesNotOverrideExistingCredentials(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[{"repo":"https://github.com/example/repo.git","username":"env-user","password":"env-pass"}]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	original := &argoappv1.Repository{
		Repo:     "https://github.com/example/repo.git",
		Username: "api-user",
	}

	repo, hydrated, err := set.HydrateRepository(original)
	if err != nil {
		t.Fatalf("HydrateRepository returned error: %v", err)
	}
	if hydrated {
		t.Fatal("expected repository not to be hydrated")
	}
	if repo.Username != "api-user" {
		t.Fatalf("expected existing credentials to be preserved, got %q", repo.Username)
	}
}

func TestHelmRepoCredsAreSortedBySpecificity(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[
		{"match":"prefix","repo":"https://charts.example.com","username":"prefix","password":"prefix-pass"},
		{"repo":"https://charts.example.com/team/app","username":"exact","password":"exact-pass"},
		{"match":"prefix","repo":"https://charts.example.com/team","username":"team","password":"team-pass"}
	]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	creds := set.HelmRepoCreds()
	if len(creds) != 3 {
		t.Fatalf("expected 3 repo creds, got %d", len(creds))
	}
	if creds[0].Username != "exact" || creds[1].Username != "team" || creds[2].Username != "prefix" {
		t.Fatalf("expected creds to be sorted by specificity, got %#v", []string{creds[0].Username, creds[1].Username, creds[2].Username})
	}
}

func TestHelmRepoCredsInferOCIFromOCIURL(t *testing.T) {
	t.Parallel()

	set, err := Parse(`[{"match":"prefix","repo":"oci://registry.example.com/team","username":"oci-user","password":"oci-pass"}]`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	creds := set.HelmRepoCreds()
	if len(creds) != 1 {
		t.Fatalf("expected 1 repo credential, got %d", len(creds))
	}
	if creds[0].Type != "oci" {
		t.Fatalf("expected repo credential type %q, got %q", "oci", creds[0].Type)
	}
	if !creds[0].EnableOCI {
		t.Fatal("expected repo credential to enable OCI")
	}
}

func writeCredentialFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "repo-creds.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}
