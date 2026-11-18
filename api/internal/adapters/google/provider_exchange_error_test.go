package google

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"caus-platform/api/internal/domain"
)

func TestProviderExchangeErrorClassification(t *testing.T) {
	cases := []struct {
		name           string
		status         int
		code           string
		invalidGranted bool
	}{
		{name: "invalid grant", status: http.StatusBadRequest, code: "invalid_grant", invalidGranted: true},
		{name: "invalid client", status: http.StatusUnauthorized, code: "invalid_client"},
		{name: "server error", status: http.StatusInternalServerError},
		{name: "empty error body", status: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, _, _ := mockProvider(t, oidcTestCase{tokenStatus: tc.status, tokenCode: tc.code})
			identity, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce")
			if err == nil || identity != (domain.GoogleIdentity{}) {
				t.Fatalf("token endpoint failure accepted: %+v error %v", identity, err)
			}
			if errors.Is(err, domain.ErrInvalidIdentity) != tc.invalidGranted {
				t.Fatalf("status %d code %q: invalid identity %v, error %v", tc.status, tc.code, !tc.invalidGranted, err)
			}
		})
	}
}
