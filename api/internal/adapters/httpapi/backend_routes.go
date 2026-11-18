package httpapi

import "net/http"

func (b *Backend) registerRoutes(mux *http.ServeMux, auth *Auth) {
	adminRead := func(handler http.Handler) http.Handler { return auth.requireAdmin(handler) }
	adminWrite := func(handler http.Handler) http.Handler {
		return auth.requireAdmin(auth.requireSameOrigin(handler))
	}
	userWrite := func(handler http.Handler) http.Handler {
		return auth.requireUser(auth.requireSameOrigin(handler))
	}
	mux.Handle("/api/talks", getOnly(http.HandlerFunc(b.listTalks)))
	mux.Handle("/api/talks/{id}", getOnly(http.HandlerFunc(b.getTalk)))
	mux.Handle("/api/talks.ics", getOnly(http.HandlerFunc(b.talksCalendar)))
	mux.Handle("/api/competitions", getOnly(http.HandlerFunc(b.listCompetitions)))
	mux.Handle("/api/competitions/{id}", getOnly(http.HandlerFunc(b.getCompetition)))
	mux.Handle("/api/me", endpointMethods(map[string]http.Handler{
		http.MethodGet:    auth.requireUser(http.HandlerFunc(b.me)),
		http.MethodPatch:  userWrite(http.HandlerFunc(b.updateAccount)),
		http.MethodDelete: userWrite(http.HandlerFunc(b.deleteAccount)),
	}))
	mux.Handle("/api/me/export", getOnly(auth.requireUser(http.HandlerFunc(b.exportAccount))))
	mux.Handle("/api/auth/logout", endpointMethods(map[string]http.Handler{
		http.MethodPost: userWrite(http.HandlerFunc(b.logout)),
	}))
	mux.Handle("/api/admin/talks", endpointMethods(map[string]http.Handler{
		http.MethodGet:  adminRead(http.HandlerFunc(b.adminListTalks)),
		http.MethodPost: adminWrite(http.HandlerFunc(b.createTalk)),
	}))
	mux.Handle("/api/admin/talks/{id}", endpointMethods(map[string]http.Handler{
		http.MethodGet:    adminRead(http.HandlerFunc(b.adminGetTalk)),
		http.MethodPatch:  adminWrite(http.HandlerFunc(b.updateTalk)),
		http.MethodDelete: adminWrite(http.HandlerFunc(b.deleteTalk)),
	}))
	mux.Handle("/api/admin/competitions", endpointMethods(map[string]http.Handler{
		http.MethodGet:  adminRead(http.HandlerFunc(b.adminListCompetitions)),
		http.MethodPost: adminWrite(http.HandlerFunc(b.createCompetition)),
	}))
	mux.Handle("/api/admin/competitions/{id}", endpointMethods(map[string]http.Handler{
		http.MethodGet:    adminRead(http.HandlerFunc(b.adminGetCompetition)),
		http.MethodPatch:  adminWrite(http.HandlerFunc(b.updateCompetition)),
		http.MethodDelete: adminWrite(http.HandlerFunc(b.deleteCompetition)),
	}))
	mux.Handle("/api/admin/competitions/{id}/teams", endpointMethods(map[string]http.Handler{
		http.MethodGet:  adminRead(http.HandlerFunc(b.listTeams)),
		http.MethodPost: adminWrite(http.HandlerFunc(b.createTeam)),
	}))
	mux.Handle("/api/admin/teams/{id}", endpointMethods(map[string]http.Handler{
		http.MethodGet:    adminRead(http.HandlerFunc(b.getTeam)),
		http.MethodPatch:  adminWrite(http.HandlerFunc(b.updateTeam)),
		http.MethodDelete: adminWrite(http.HandlerFunc(b.deleteTeam)),
	}))
	mux.Handle("/api/admin/users", getOnly(adminRead(http.HandlerFunc(b.listUsers))))
	mux.Handle("/api/admin/users/{id}", endpointMethods(map[string]http.Handler{
		http.MethodPatch: adminWrite(http.HandlerFunc(b.updateUserRole)),
	}))
}
