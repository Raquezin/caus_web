package httpapi

import "net/http"

func (b *Backend) adminListCompetitions(w http.ResponseWriter, r *http.Request) {
	b.listCompetitions(w, r)
}
