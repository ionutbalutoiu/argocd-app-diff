package cli

import (
	"fmt"

	"argocd-app-diff/internal/appdiff"
	"argocd-app-diff/internal/appspec"
	"argocd-app-diff/internal/repocreds"

	"github.com/spf13/cobra"

	argocdclient "github.com/argoproj/argo-cd/v3/pkg/apiclient"
)

const (
	applicationFileFlag = "application-file"
)

type diffCommand struct {
	clientOpts      *argocdclient.ClientOptions
	applicationPath string
	repoServerURL   string
	sourceFlags     []string
	namespace       string
	exitCode        bool
	diffExitCode    int
	refresh         bool
	hardRefresh     bool
}

func configureDiffCommand(cmd *cobra.Command, clientOpts *argocdclient.ClientOptions) {
	diff := &diffCommand{
		clientOpts:   clientOpts,
		exitCode:     true,
		diffExitCode: 1,
	}

	cmd.Short = "Display the diff between a local Argo CD Application file and live managed resources"
	cmd.Long = "Display the diff between a local Argo CD Application file and live managed resources.\n" +
		"Uses diff output compatible with Argo CD and respects KUBECTL_EXTERNAL_DIFF when set.\n" +
		"Repository credentials may be supplied via " + repocreds.EnvVarJSONName + " or " + repocreds.EnvVarJSONPathName + "."
	cmd.Args = cobra.NoArgs
	cmd.RunE = diff.run

	diff.addFlags(cmd)
}

func (d *diffCommand) addFlags(cmd *cobra.Command) {
	flags := cmd.Flags()

	flags.StringVar(&d.applicationPath, applicationFileFlag, "", "Path to a single Argo CD Application manifest")
	flags.StringVar(&d.repoServerURL, "repo-server-url", "", "Direct Argo CD repo-server URL. Falls back to ARGOCD_REPO_SERVER_URL")
	flags.StringArrayVar(&d.sourceFlags, sourceRevisionOverrideFlag, nil, "Override a source target revision using <REPO_URL>|<NEW_REVISION>; may be repeated")
	flags.StringVarP(&d.namespace, "namespace", "N", "", "Namespace of the live Application resource")
	flags.BoolVar(&d.exitCode, "exit-code", d.exitCode, "Return non-zero exit code when a diff is found")
	flags.IntVar(&d.diffExitCode, "diff-exit-code", d.diffExitCode, "Return this exit code when a diff is found")
	flags.BoolVar(&d.refresh, "refresh", false, "Refresh application data before reading live resources")
	flags.BoolVar(&d.hardRefresh, "hard-refresh", false, "Refresh application data and bypass repo-server manifest cache")
}

func (d *diffCommand) run(cmd *cobra.Command, _ []string) error {
	if d.applicationPath == "" {
		return fmt.Errorf("--%s is required", applicationFileFlag)
	}

	app, err := appspec.Load(d.applicationPath)
	if err != nil {
		return err
	}

	sourceOverrides, err := parseSourceOverrides(d.sourceFlags)
	if err != nil {
		return err
	}
	if err := appspec.ApplySourceRevisionOverrides(app, sourceOverrides); err != nil {
		return err
	}

	repoCredentials, err := repocreds.LoadFromEnv()
	if err != nil {
		return err
	}

	liveNamespace := d.namespace
	if liveNamespace == "" {
		liveNamespace = app.Namespace
	}

	apiClient, err := argocdclient.NewClient(d.clientOpts)
	if err != nil {
		return fmt.Errorf("create Argo CD API client: %w", err)
	}

	result, err := appdiff.Run(cmd.Context(), apiClient, appdiff.Request{
		Application:     app,
		LiveNamespace:   liveNamespace,
		RepoServerURL:   d.repoServerURL,
		RepoCredentials: repoCredentials,
		Refresh:         d.refresh,
		HardRefresh:     d.hardRefresh,
	})
	if err != nil {
		return err
	}

	if result.FoundDiffs && d.exitCode {
		return &exitError{code: d.diffExitCode}
	}

	return nil
}
