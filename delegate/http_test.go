package delegate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wings-software/dlite/client"
)

// TestDelegateTokenHashHeader verifies that the delegateTokenHash header is
// sent (or omitted) correctly for each client construction mode.
//
// The secret-based expectations below are golden values produced by the
// manager's HashUtils.calculateSha256 (Java BigInteger hex encoding, which
// strips leading zero nibbles) — deliberately NOT recomputed here, so the
// test catches any encoding divergence from what the manager stores.
func TestDelegateTokenHashHeader(t *testing.T) {
	// valid 16-byte hex secrets, usable as AES keys
	secret := "98d8fedb04c574e68939164bf463e73d" // gitleaks:allow — fake test fixture, not a credential
	// calculateSha256("98d8fedb04c574e68939164bf463e73d")
	secretHash := "9d5c66162adf75e9258f8e30054d3d507c4e1f65c9511d6104c37d62a76bb04f" // gitleaks:allow — golden SHA-256 test vector
	// SHA-256 of this secret begins with the zero byte 0x00; the manager's
	// BigInteger encoding strips it, yielding a 62-char hash. Strict hex
	// encoders (Go's hex.EncodeToString) would emit "00..." and miss the
	// manager's indexed lookup.
	zeroSecret := "ab00000000000000000000000000012c"
	// calculateSha256("ab00000000000000000000000000012c")
	zeroSecretHash := "427e1637b3abbdf434a0034e6a55d5c809e30fc6d1ac11e701a537a346ca7f" // gitleaks:allow — golden SHA-256 test vector
	explicitHash := strings.Repeat("ab", 32) // 64-char hex, stands in for a real hash

	tests := []struct {
		name      string
		newClient func(endpoint string) *HTTPClient
		wantHash  string
	}{
		{
			name: "secret-based client derives hash",
			newClient: func(endpoint string) *HTTPClient {
				return New(endpoint, "acct", secret, false, "")
			},
			wantHash: secretHash,
		},
		{
			name: "secret-based client matches manager encoding when digest has leading zero byte",
			newClient: func(endpoint string) *HTTPClient {
				return New(endpoint, "acct", zeroSecret, false, "")
			},
			wantHash: zeroSecretHash,
		},
		{
			// Docker delegates receive the base64-encoded UI token undecoded; the
			// manager stores calculateSha256 of the decoded hex token.
			// base64("98d8fedb04c574e68939164bf463e73d") — hardcoded so the test
			// pins the normalization contract, not Go's encoder output.
			name: "base64-configured secret is normalized before hashing (Docker delegate form)",
			newClient: func(endpoint string) *HTTPClient {
				return New(endpoint, "acct", "OThkOGZlZGIwNGM1NzRlNjg5MzkxNjRiZjQ2M2U3M2Q=", false, "")
			},
			wantHash: secretHash,
		},
		{
			name: "token-based client with hash sends provided hash",
			newClient: func(endpoint string) *HTTPClient {
				return NewFromTokenWithHash(endpoint, "acct", "jwe-token", explicitHash, false, "")
			},
			wantHash: explicitHash,
		},
		{
			name: "token-based client without hash omits header",
			newClient: func(endpoint string) *HTTPClient {
				return NewFromToken(endpoint, "acct", "jwe-token", false, "")
			},
			wantHash: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotHash, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHash = r.Header.Get(DelegateTokenHashHeader)
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := tc.newClient(srv.URL)
			if err := c.Heartbeat(context.Background(), &client.RegisterRequest{}); err != nil {
				t.Fatalf("heartbeat: %v", err)
			}
			if gotHash != tc.wantHash {
				t.Errorf("%s header = %q, want %q", DelegateTokenHashHeader, gotHash, tc.wantHash)
			}
			if !strings.HasPrefix(gotAuth, "Delegate ") {
				t.Errorf("Authorization header = %q, want Delegate prefix", gotAuth)
			}
		})
	}
}
