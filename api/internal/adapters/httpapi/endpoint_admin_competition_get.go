package httpapi

import "net/http"

func (b *Backend) adminGetCompetition(w http.ResponseWriter, r *http.Request) {
	b.getCompetition(w, r)
}
