package repocreds

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	argoappv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v3/util/git"
)

const (
	EnvVarJSONName     = "ARGOCD_APP_DIFF_REPO_CREDENTIALS_JSON"
	EnvVarJSONPathName = "ARGOCD_APP_DIFF_REPO_CREDENTIALS_JSON_PATH"
	matchExact         = "exact"
	matchPrefix        = "prefix"
	ociPrefix          = "oci://"
)

var allowedFields = map[string]struct{}{
	"match":         {},
	"repo":          {},
	"username":      {},
	"password":      {},
	"sshPrivateKey": {},
}

type Set struct {
	exact         map[string]*argoappv1.Repository
	prefixes      []prefixEntry
	helmRepoCreds []*argoappv1.RepoCreds
}

type prefixEntry struct {
	normalizedRepo string
	template       *argoappv1.RepoCreds
}

type rawEntry struct {
	Match         string `json:"match"`
	Repo          string `json:"repo"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	SSHPrivateKey string `json:"sshPrivateKey"`
}

// LoadFromEnv loads repository credentials from ARGOCD_APP_DIFF_REPO_CREDENTIALS_JSON
// or, when unset, ARGOCD_APP_DIFF_REPO_CREDENTIALS_JSON_PATH.
func LoadFromEnv() (*Set, error) {
	inlineJSON := strings.TrimSpace(os.Getenv(EnvVarJSONName))
	if inlineJSON != "" {
		set, err := Parse(inlineJSON)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", EnvVarJSONName, err)
		}
		return set, nil
	}

	jsonPath := strings.TrimSpace(os.Getenv(EnvVarJSONPathName))
	if jsonPath == "" {
		return nil, nil
	}

	content, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read %s file %q: %w", EnvVarJSONPathName, jsonPath, err)
	}

	set, err := Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("parse %s file %q: %w", EnvVarJSONPathName, jsonPath, err)
	}
	return set, nil
}

// Parse validates and loads a credential set from JSON.
func Parse(raw string) (*Set, error) {
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}

	set := &Set{exact: make(map[string]*argoappv1.Repository)}
	var helmEntries []helmRepoCredEntry
	for i, item := range items {
		entry, err := parseEntry(item)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i+1, err)
		}

		normalized := normalizeForMatch(entry.Repo)
		if normalized == "" {
			return nil, fmt.Errorf("entry %d: repository must normalize to a non-empty value", i+1)
		}

		switch entry.Match {
		case matchExact:
			if _, exists := set.exact[normalized]; exists {
				return nil, fmt.Errorf("entry %d: duplicate exact entry for repository %q", i+1, entry.Repo)
			}
			set.exact[normalized] = entry.toRepository()
		case matchPrefix:
			set.prefixes = append(set.prefixes, prefixEntry{
				normalizedRepo: normalized,
				template:       entry.toRepoCreds(),
			})
		default:
			return nil, fmt.Errorf("entry %d: unsupported match %q", i+1, entry.Match)
		}

		helmEntries = append(helmEntries, helmRepoCredEntry{
			match:       entry.Match,
			rawRepo:     entry.Repo,
			specificity: normalized,
			credentials: entry.toRepoCreds(),
		})
	}

	sort.SliceStable(helmEntries, func(i, j int) bool {
		if len(helmEntries[i].specificity) != len(helmEntries[j].specificity) {
			return len(helmEntries[i].specificity) > len(helmEntries[j].specificity)
		}
		if helmEntries[i].match != helmEntries[j].match {
			return helmEntries[i].match == matchExact
		}
		return helmEntries[i].rawRepo < helmEntries[j].rawRepo
	})
	for _, entry := range helmEntries {
		set.helmRepoCreds = append(set.helmRepoCreds, cloneRepoCreds(entry.credentials))
	}

	return set, nil
}

// MatchRepository resolves the highest-precedence entry for the given repo URL.
func (s *Set) MatchRepository(repoURL string) (*argoappv1.Repository, bool, error) {
	if s == nil {
		return nil, false, nil
	}

	if exact, ok := s.exactRepository(repoURL); ok {
		return exact, true, nil
	}

	prefix, ok, err := s.prefixCredential(repoURL)
	if err != nil || !ok {
		return nil, false, err
	}

	repo := &argoappv1.Repository{Repo: repoURL}
	copyMissingFromRepoCreds(repo, prefix)
	return repo, true, nil
}

// HydrateRepository fills missing repository fields from a matching entry.
func (s *Set) HydrateRepository(repo *argoappv1.Repository) (*argoappv1.Repository, bool, error) {
	if s == nil || repo == nil || repo.HasCredentials() {
		return repo, false, nil
	}

	if exact, ok := s.exactRepository(repo.Repo); ok {
		hydrated := cloneRepository(repo)
		if !copyMissingFromRepository(hydrated, exact) {
			return repo, false, nil
		}
		hydrated.InheritedCreds = true
		return hydrated, true, nil
	}

	prefix, ok, err := s.prefixCredential(repo.Repo)
	if err != nil || !ok {
		return repo, false, err
	}

	hydrated := cloneRepository(repo)
	if !copyMissingFromRepoCreds(hydrated, prefix) {
		return repo, false, nil
	}
	hydrated.InheritedCreds = true
	return hydrated, true, nil
}

// HelmRepoCreds returns the configured repository credentials ordered by specificity.
func (s *Set) HelmRepoCreds() []*argoappv1.RepoCreds {
	if s == nil || len(s.helmRepoCreds) == 0 {
		return nil
	}

	result := make([]*argoappv1.RepoCreds, 0, len(s.helmRepoCreds))
	for _, cred := range s.helmRepoCreds {
		result = append(result, cloneRepoCreds(cred))
	}
	return result
}

func (s *Set) exactRepository(repoURL string) (*argoappv1.Repository, bool) {
	if s == nil {
		return nil, false
	}

	repo, ok := s.exact[normalizeForMatch(repoURL)]
	if !ok {
		return nil, false
	}

	matched := cloneRepository(repo)
	matched.Repo = repoURL
	return matched, true
}

func (s *Set) prefixCredential(repoURL string) (*argoappv1.RepoCreds, bool, error) {
	if s == nil {
		return nil, false, nil
	}

	normalizedRepoURL := normalizeForMatch(repoURL)
	var matched *argoappv1.RepoCreds
	var matchedKey string
	for _, candidate := range s.prefixes {
		if !strings.HasPrefix(normalizedRepoURL, candidate.normalizedRepo) {
			continue
		}
		if len(candidate.normalizedRepo) > len(matchedKey) {
			matched = candidate.template
			matchedKey = candidate.normalizedRepo
			continue
		}
		if len(candidate.normalizedRepo) == len(matchedKey) {
			return nil, false, fmt.Errorf("multiple prefix entries matched repository %q with the same specificity", repoURL)
		}
	}

	if matched == nil {
		return nil, false, nil
	}
	return cloneRepoCreds(matched), true, nil
}

func parseEntry(item json.RawMessage) (rawEntry, error) {
	var fieldMap map[string]json.RawMessage
	if err := json.Unmarshal(item, &fieldMap); err != nil {
		return rawEntry{}, err
	}

	for field := range fieldMap {
		if _, ok := allowedFields[field]; !ok {
			return rawEntry{}, fmt.Errorf("unknown field %q", field)
		}
	}

	var entry rawEntry
	if err := json.Unmarshal(item, &entry); err != nil {
		return rawEntry{}, err
	}

	entry.Match = strings.TrimSpace(entry.Match)
	if entry.Match == "" {
		entry.Match = matchExact
	}
	entry.Repo = strings.TrimSpace(entry.Repo)

	if entry.Match != matchExact && entry.Match != matchPrefix {
		return rawEntry{}, fmt.Errorf("match must be %q or %q", matchExact, matchPrefix)
	}
	if entry.Repo == "" {
		return rawEntry{}, fmt.Errorf("repo is required")
	}

	switch {
	case entry.SSHPrivateKey != "" && (entry.Username != "" || entry.Password != ""):
		return rawEntry{}, fmt.Errorf("sshPrivateKey cannot be combined with username/password")
	case entry.SSHPrivateKey != "":
		return entry, nil
	case entry.Username == "" && entry.Password == "":
		return rawEntry{}, fmt.Errorf("either sshPrivateKey or username/password is required")
	case entry.Username == "" || entry.Password == "":
		return rawEntry{}, fmt.Errorf("username and password must both be provided")
	default:
		return entry, nil
	}
}

func (e rawEntry) toRepository() *argoappv1.Repository {
	repo := &argoappv1.Repository{
		Repo:          e.Repo,
		Username:      e.Username,
		Password:      e.Password,
		SSHPrivateKey: e.SSHPrivateKey,
	}
	if isOCIReference(e.Repo) {
		repo.EnableOCI = true
	}
	return repo
}

func (e rawEntry) toRepoCreds() *argoappv1.RepoCreds {
	creds := &argoappv1.RepoCreds{
		URL:           e.Repo,
		Username:      e.Username,
		Password:      e.Password,
		SSHPrivateKey: e.SSHPrivateKey,
	}
	if isOCIReference(e.Repo) {
		creds.Type = "oci"
		creds.EnableOCI = true
	}
	return creds
}

func normalizeForMatch(repo string) string {
	return git.NormalizeGitURLAllowInvalid(strings.TrimPrefix(strings.TrimSpace(repo), ociPrefix))
}

func isOCIReference(repo string) bool {
	return strings.HasPrefix(strings.TrimSpace(repo), ociPrefix)
}

func copyMissingFromRepository(dst *argoappv1.Repository, src *argoappv1.Repository) bool {
	if dst == nil || src == nil {
		return false
	}

	var applied bool
	applied = copyString(&dst.Username, src.Username) || applied
	applied = copyString(&dst.Password, src.Password) || applied
	applied = copyString(&dst.SSHPrivateKey, src.SSHPrivateKey) || applied
	applied = copyBool(&dst.EnableOCI, src.EnableOCI) || applied
	return applied
}

func copyMissingFromRepoCreds(dst *argoappv1.Repository, src *argoappv1.RepoCreds) bool {
	if dst == nil || src == nil {
		return false
	}

	var applied bool
	applied = copyString(&dst.Username, src.Username) || applied
	applied = copyString(&dst.Password, src.Password) || applied
	applied = copyString(&dst.SSHPrivateKey, src.SSHPrivateKey) || applied
	applied = copyBool(&dst.EnableOCI, src.EnableOCI) || applied
	return applied
}

func copyString(dst *string, src string) bool {
	if *dst != "" || src == "" {
		return false
	}
	*dst = src
	return true
}

func copyBool(dst *bool, src bool) bool {
	if *dst || !src {
		return false
	}
	*dst = true
	return true
}

func cloneRepository(repo *argoappv1.Repository) *argoappv1.Repository {
	if repo == nil {
		return nil
	}
	cloned := *repo
	return &cloned
}

func cloneRepoCreds(creds *argoappv1.RepoCreds) *argoappv1.RepoCreds {
	if creds == nil {
		return nil
	}
	cloned := *creds
	return &cloned
}

type helmRepoCredEntry struct {
	match       string
	rawRepo     string
	specificity string
	credentials *argoappv1.RepoCreds
}
