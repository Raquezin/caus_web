package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGoogleCallbackRedirectsToExactReturnToE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	for _, returnTo := range []string{
		"/cuenta/?from=login",
		"/a/../b",
		"/a//b",
		"/en/#seccion/../x",
		"/a/./b/",
		"/%2e%2e/x",
		"/a?next=//evil.example/..",
		"/a;b=c/../d",
		"/charlas/2027/#speaker//ada",
	} {
		f.OIDC.mu.Lock()
		f.OIDC.nextClaims = authTestClaims{}
		f.OIDC.mu.Unlock()
		login, err := f.Client.Get(f.Server.URL + "/api/auth/google/login?return_to=" + url.QueryEscape(returnTo))
		if err != nil {
			t.Fatal(err)
		}
		_ = login.Body.Close()
		if login.StatusCode != http.StatusFound {
			t.Fatalf("return_to %q rejected at login with status %d", returnTo, login.StatusCode)
		}
		authorize, err := f.Client.Get(login.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		_ = authorize.Body.Close()
		if authorize.StatusCode != http.StatusFound {
			t.Fatalf("return_to %q: authorization status %d", returnTo, authorize.StatusCode)
		}
		callback, err := f.Client.Get(authorize.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		_ = callback.Body.Close()
		if callback.StatusCode != http.StatusFound {
			t.Fatalf("return_to %q: callback status %d", returnTo, callback.StatusCode)
		}
		location := callback.Header.Get("Location")
		if strings.HasPrefix(location, "//") || strings.Contains(location, "://") {
			t.Fatalf("return_to %q produced an off-site redirect %q", returnTo, location)
		}
		if location != returnTo {
			t.Fatalf("return_to %q accepted at login but redirected to %q", returnTo, location)
		}
	}
}
