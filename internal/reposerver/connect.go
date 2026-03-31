package reposerver

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	repoapiclient "github.com/argoproj/argo-cd/v3/reposerver/apiclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// Connect opens a repo-server client connection, retrying with the opposite TLS mode
// when the initial transport handshake indicates a protocol mismatch.
func Connect(ctx context.Context, address string, tlsConfig repoapiclient.TLSConfiguration) (io.Closer, repoapiclient.RepoServerServiceClient, error) {
	return connectWithFactory(ctx, address, tlsConfig, newClient, probe)
}

type clientFactory func(address string, tlsConfig repoapiclient.TLSConfiguration) (io.Closer, repoapiclient.RepoServerServiceClient, error)

type prober func(ctx context.Context, conn io.Closer) error

func connectWithFactory(
	ctx context.Context,
	address string,
	tlsConfig repoapiclient.TLSConfiguration,
	newClient clientFactory,
	probeConn prober,
) (io.Closer, repoapiclient.RepoServerServiceClient, error) {
	conn, client, err := connectAndProbe(ctx, address, tlsConfig, newClient, probeConn)
	if err == nil {
		return conn, client, nil
	}
	if !shouldRetry(err) {
		return nil, nil, err
	}

	fallbackConn, fallbackClient, fallbackErr := connectAndProbe(ctx, address, oppositeTLSConfig(tlsConfig), newClient, probeConn)
	if fallbackErr != nil {
		return nil, nil, fallbackConnectionError(err, fallbackErr)
	}

	return fallbackConn, fallbackClient, nil
}

func connectAndProbe(
	ctx context.Context,
	address string,
	tlsConfig repoapiclient.TLSConfiguration,
	newClient clientFactory,
	probeConn prober,
) (io.Closer, repoapiclient.RepoServerServiceClient, error) {
	conn, client, err := newClient(address, tlsConfig)
	if err != nil {
		return nil, nil, err
	}

	if err := probeConn(ctx, conn); err != nil {
		closeQuietly(conn)
		return nil, nil, err
	}

	return conn, client, nil
}

func oppositeTLSConfig(tlsConfig repoapiclient.TLSConfiguration) repoapiclient.TLSConfiguration {
	fallback := tlsConfig
	fallback.DisableTLS = !tlsConfig.DisableTLS
	return fallback
}

func fallbackConnectionError(initialErr error, fallbackErr error) error {
	return fmt.Errorf("initial repo server connection failed: %w; fallback connection failed: %w", initialErr, fallbackErr)
}

func newClient(address string, tlsConfig repoapiclient.TLSConfiguration) (io.Closer, repoapiclient.RepoServerServiceClient, error) {
	repoClientset := repoapiclient.NewRepoServerClientset(address, 60, tlsConfig)
	return repoClientset.NewRepoServerClient()
}

func probe(ctx context.Context, conn io.Closer) error {
	connInterface, ok := conn.(grpc.ClientConnInterface)
	if !ok {
		return fmt.Errorf("repo server connection does not support gRPC health checks")
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	healthClient := grpc_health_v1.NewHealthClient(connInterface)
	resp, err := healthClient.Check(probeCtx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("repo server health check returned %s", resp.GetStatus())
	}
	return nil
}

func shouldRetry(err error) bool {
	if status.Code(err) != codes.Unavailable {
		return false
	}

	message := err.Error()
	return strings.Contains(message, "error reading server preface") ||
		strings.Contains(message, "tls: first record does not look like a TLS handshake") ||
		strings.Contains(message, "authentication handshake failed") ||
		strings.Contains(message, "connection closed before server preface received")
}

func closeQuietly(closer io.Closer) {
	if closer != nil {
		_ = closer.Close()
	}
}
