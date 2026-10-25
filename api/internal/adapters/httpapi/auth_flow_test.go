package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestDecodeOAuthFlow(t *testing.T) {
	now := time.Unix(1700000000, 0)
	valid := oauthFlow{
		State: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), Verifier: oauth2.GenerateVerifier(),
		ReturnTo: "/cuenta/?tab=profile", ExpiresAt: now.Add(oauthFlowTTL).Unix(),
	}
	encode := func(flow oauthFlow) string {
		data, err := json.Marshal(flow)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	for _, returnTo := range []string{valid.ReturnTo, "/discounts/100%25"} {
		flow := valid
		flow.ReturnTo = returnTo
		if got, err := decodeOAuthFlow(encode(flow), now); err != nil || got != flow {
			t.Fatalf("valid flow with return_to %q rejected", returnTo)
		}
	}
	for name, change := range map[string]func(*oauthFlow){
		"expired":             func(flow *oauthFlow) { flow.ExpiresAt = now.Unix() - 1 },
		"exact expiry":        func(flow *oauthFlow) { flow.ExpiresAt = now.Unix() },
		"far future":          func(flow *oauthFlow) { flow.ExpiresAt++ },
		"missing state":       func(flow *oauthFlow) { flow.State = "" },
		"invalid nonce":       func(flow *oauthFlow) { flow.Nonce = "invalid" },
		"invalid verifier":    func(flow *oauthFlow) { flow.Verifier = strings.Repeat("x", 43) },
		"unsafe return path":  func(flow *oauthFlow) { flow.ReturnTo = "//evil.example" },
		"encoded return path": func(flow *oauthFlow) { flow.ReturnTo = "/%252fevil.example" },
	} {
		t.Run(name, func(t *testing.T) {
			flow := valid
			change(&flow)
			if _, err := decodeOAuthFlow(encode(flow), now); err == nil {
				t.Fatal("invalid flow accepted")
			}
		})
	}
	for _, value := range []string{
		"", "%%%", strings.Repeat("x", maxOAuthCookieLength+1),
		base64.RawURLEncoding.EncodeToString([]byte(`{"unknown":true}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`null`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"state":`)),
		base64.RawURLEncoding.EncodeToString(append([]byte(`{}`), []byte(`{}`)...)),
	} {
		if _, err := decodeOAuthFlow(value, now); err == nil {
			t.Fatal("malformed flow accepted")
		}
	}
	if _, err := decodeOAuthFlow(encode(valid), time.Unix(valid.ExpiresAt, 0)); err == nil {
		t.Fatal("flow accepted at exact expiration boundary")
	}
}
