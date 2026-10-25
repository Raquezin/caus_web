package httpapi

import (
	"net/http"
	"sort"
	"strings"
)

func originAllowed(origin, publicOrigin string) bool {
	return publicOrigin != "" && origin == publicOrigin
}

func cors(next http.Handler, publicOrigin string, mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resolved http.Handler
		if mux != nil {
			resolved, r.Pattern = mux.Handler(r)
		}
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !originAllowed(origin, publicOrigin) {
			writeError(w, http.StatusForbidden, "cors_origin_denied", "origin not allowed")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			method := r.Header.Get("Access-Control-Request-Method")
			allow := preflightAllow(resolved)
			if !containsMethod(allow, method) {
				w.Header().Set("Allow", strings.Join(allow, ", "))
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
				return
			}
			if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
				for _, header := range strings.Split(requested, ",") {
					header = strings.TrimSpace(header)
					if !strings.EqualFold(header, "Accept") && !strings.EqualFold(header, "Content-Type") {
						writeError(w, http.StatusBadRequest, "cors_headers_denied", "request headers not allowed")
						return
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(allow, ", "))
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After, X-Request-ID")
		next.ServeHTTP(w, r)
	})
}

func preflightAllow(handler http.Handler) []string {
	if dispatch, ok := handler.(*methodDispatch); ok {
		allow := make([]string, 0, len(dispatch.allow)+1)
		allow = append(allow, dispatch.allow...)
		allow = append(allow, http.MethodOptions)
		sort.Strings(allow)
		return allow
	}
	return []string{http.MethodGet, http.MethodHead, http.MethodOptions}
}

func containsMethod(methods []string, method string) bool {
	for _, candidate := range methods {
		if candidate == method {
			return true
		}
	}
	return false
}
