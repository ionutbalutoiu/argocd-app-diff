package reposerver

import "testing"

func TestParseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		address     string
		disableTLS  bool
		expectError bool
	}{
		{
			name:       "grpc",
			input:      "grpc://repo-server.argocd.svc:8081",
			address:    "repo-server.argocd.svc:8081",
			disableTLS: true,
		},
		{
			name:       "grpcs",
			input:      "grpcs://repo-server.argocd.svc:8081",
			address:    "repo-server.argocd.svc:8081",
			disableTLS: false,
		},
		{
			name:        "missing scheme",
			input:       "repo-server.argocd.svc:8081",
			expectError: true,
		},
		{name: "path is rejected", input: "grpcs://repo-server.argocd.svc:8081/path", expectError: true},
		{name: "query is rejected", input: "grpcs://repo-server.argocd.svc:8081?tls=false", expectError: true},
		{name: "fragment is rejected", input: "grpcs://repo-server.argocd.svc:8081#fragment", expectError: true},
		{name: "userinfo is rejected", input: "grpcs://user:pass@repo-server.argocd.svc:8081", expectError: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			address, tlsConfig, err := ParseURL(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseURL returned error: %v", err)
			}
			if address != tt.address {
				t.Fatalf("expected address %q, got %q", tt.address, address)
			}
			if tlsConfig.DisableTLS != tt.disableTLS {
				t.Fatalf("expected DisableTLS=%v, got %v", tt.disableTLS, tlsConfig.DisableTLS)
			}
		})
	}
}
