package reposerver

import (
	"context"
	"errors"
	"io"
	"testing"

	repoapiclient "github.com/argoproj/argo-cd/v3/reposerver/apiclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestConnectWithFactoryFallsBackOnTransportMismatch(t *testing.T) {
	t.Parallel()

	type attempt struct {
		disableTLS bool
	}

	var attempts []attempt
	probeCalls := 0

	conn, _, err := connectWithFactory(
		context.Background(),
		"repo-server.example:8081",
		repoapiclient.TLSConfiguration{DisableTLS: true},
		func(_ string, tlsConfig repoapiclient.TLSConfiguration) (io.Closer, repoapiclient.RepoServerServiceClient, error) {
			attempts = append(attempts, attempt{disableTLS: tlsConfig.DisableTLS})
			return noopCloser{}, nil, nil
		},
		func(context.Context, io.Closer) error {
			probeCalls++
			if probeCalls == 1 {
				return status.Error(codes.Unavailable, `connection error: desc = "error reading server preface: EOF"`)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("connectWithFactory returned error: %v", err)
	}
	t.Cleanup(func() {
		closeQuietly(conn)
	})

	if len(attempts) != 2 {
		t.Fatalf("expected 2 connection attempts, got %d", len(attempts))
	}
	if !attempts[0].disableTLS {
		t.Fatalf("expected first attempt to use plaintext")
	}
	if attempts[1].disableTLS {
		t.Fatalf("expected fallback attempt to use TLS")
	}
}

func TestConnectWithFactoryReturnsNonRetryableError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("boom")

	_, _, err := connectWithFactory(
		context.Background(),
		"repo-server.example:8081",
		repoapiclient.TLSConfiguration{DisableTLS: true},
		func(_ string, _ repoapiclient.TLSConfiguration) (io.Closer, repoapiclient.RepoServerServiceClient, error) {
			return noopCloser{}, nil, nil
		},
		func(context.Context, io.Closer) error {
			return expectedErr
		},
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestShouldRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		err   error
		retry bool
	}{
		{
			name:  "server preface EOF",
			err:   status.Error(codes.Unavailable, `connection error: desc = "error reading server preface: EOF"`),
			retry: true,
		},
		{
			name:  "tls handshake mismatch",
			err:   status.Error(codes.Unavailable, "transport: authentication handshake failed: tls: first record does not look like a TLS handshake"),
			retry: true,
		},
		{
			name:  "other unavailable error",
			err:   status.Error(codes.Unavailable, "name resolver error"),
			retry: false,
		},
		{
			name:  "non unavailable error",
			err:   status.Error(codes.Internal, "boom"),
			retry: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldRetry(tt.err); got != tt.retry {
				t.Fatalf("expected retry=%v, got %v", tt.retry, got)
			}
		})
	}
}

type noopCloser struct{}

func (noopCloser) Close() error {
	return nil
}
