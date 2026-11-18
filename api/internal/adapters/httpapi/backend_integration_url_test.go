package httpapi

import (
	"net/http"
	"testing"
)

func TestBackendIntegrationDatabaseURLChecksNameTheirField(t *testing.T) {
	f := newIntegrationFixture(t)

	talk := validIntegrationTalkBody(f, "Underscore host")
	talk["stream_url"] = "http://my_host/x"
	status, _, body := f.admin(http.MethodPost, "/api/admin/talks", talk)
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, body, "stream_url")

	competitionID := f.createCompetitionRow("URL cup", 3)
	status, _, body = f.admin(http.MethodPost, "/api/admin/competitions/"+competitionID.String()+"/teams",
		map[string]any{"photo_url": "https://ñandú.es/x", "member_ids": []string{f.memberID.String()}})
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, body, "photo_url")
}
