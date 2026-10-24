package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const allowedOrigin = "https://clubalgoritmiaus.es"

func originAllowed(origin string) bool {
	if origin == allowedOrigin {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	return origin == u.Scheme+"://localhost:"+u.Port()
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !originAllowed(origin) {
			writeError(w, http.StatusForbidden, "cors_origin_denied", "origin not allowed")
			return
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			method := r.Header.Get("Access-Control-Request-Method")
			if method != http.MethodGet && method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD, OPTIONS")
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
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After")
		next.ServeHTTP(w, r)
	})
}
