package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) exportAccount(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	export, err := b.service.ExportAccount(ctx, account.ID)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="caus-account.json"`)
	writeJSON(w, http.StatusOK, exportResponse(export))
}
