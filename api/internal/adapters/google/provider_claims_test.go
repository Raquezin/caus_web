package google

import (
	"context"
	"testing"
)

func TestProviderAcceptsAuthorizedParty(t *testing.T) {
	cases := map[string]func(map[string]any){
		"single audience no azp":    nil,
		"matching authorized party": func(claims map[string]any) { claims["azp"] = "test-client" },
		"multi audience with azp": func(claims map[string]any) {
			claims["aud"] = []string{"test-client", "other-client"}
			claims["azp"] = "test-client"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			provider, _, _ := mockProvider(t, oidcTestCase{mutate: mutate})
			identity, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce")
			if err != nil || identity.Subject != "verified-subject" {
				t.Fatalf("valid identity rejected: %+v error %v", identity, err)
			}
		})
	}
}

func TestProviderEmailAuthority(t *testing.T) {
	cases := map[string]struct {
		mutate        func(map[string]any)
		authoritative bool
	}{
		"gmail":                        {mutate: func(claims map[string]any) { claims["email"] = "ada@gmail.com" }, authoritative: true},
		"uppercase gmail":              {mutate: func(claims map[string]any) { claims["email"] = " ADA@GMAIL.COM " }, authoritative: true},
		"third party no hosted domain": {authoritative: false},
		"hosted domain":                {mutate: func(claims map[string]any) { claims["hd"] = "example.com" }, authoritative: true},
		"blank hosted domain":          {mutate: func(claims map[string]any) { claims["hd"] = "  " }, authoritative: false},
		"empty hosted domain":          {mutate: func(claims map[string]any) { claims["hd"] = "" }, authoritative: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider, _, _ := mockProvider(t, oidcTestCase{mutate: tc.mutate})
			identity, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce")
			if err != nil {
				t.Fatal(err)
			}
			if identity.EmailAuthoritative != tc.authoritative {
				t.Fatalf("email authority %v, want %v", identity.EmailAuthoritative, tc.authoritative)
			}
		})
	}
}
