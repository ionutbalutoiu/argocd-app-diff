package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	argocdclient "github.com/argoproj/argo-cd/v3/pkg/apiclient"
	"github.com/argoproj/argo-cd/v3/util/localconfig"
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// Execute runs the CLI and returns the desired process exit code.
func Execute() int {
	if err := newRootCommand().Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.err != nil && ee.err.Error() != "" {
				_, _ = fmt.Fprintln(os.Stderr, ee.err)
			}
			return ee.code
		}

		_, _ = fmt.Fprintln(os.Stderr, err)
		return 2
	}

	return 0
}

func newRootCommand() *cobra.Command {
	clientOpts := argocdclient.ClientOptions{
		ConfigPath: defaultConfigPath(),
	}

	cmd := &cobra.Command{
		Use:           "argocd-app-diff",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	flags := cmd.PersistentFlags()
	flags.StringVar(&clientOpts.ConfigPath, "config", clientOpts.ConfigPath, "Path to Argo CD config")
	flags.StringVar(&clientOpts.ServerAddr, "server", "", "Argo CD server address")
	flags.BoolVar(&clientOpts.PlainText, "plaintext", false, "Disable TLS for Argo CD API requests")
	flags.BoolVar(&clientOpts.Insecure, "insecure", false, "Skip Argo CD server certificate and domain verification")
	flags.StringVar(&clientOpts.AuthToken, "auth-token", "", "Authentication token")
	flags.BoolVar(&clientOpts.GRPCWeb, "grpc-web", false, "Enable gRPC-web protocol")
	flags.StringVar(&clientOpts.GRPCWebRootPath, "grpc-web-root-path", "", "Set the gRPC-web root path")
	flags.StringSliceVarP(&clientOpts.Headers, "header", "H", nil, "Set additional header for all Argo CD API requests")
	flags.StringVar(&clientOpts.Context, "argocd-context", "", "The name of the Argo CD server context to use")

	configureDiffCommand(cmd, &clientOpts)
	return cmd
}

func defaultConfigPath() string {
	path, err := localconfig.DefaultLocalConfigPath()
	if err != nil {
		return ""
	}

	return path
}
