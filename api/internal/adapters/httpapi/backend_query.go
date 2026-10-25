package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"caus-platform/api/internal/domain"
)

func parseTalkFilter(r *http.Request, includeUnpublished bool) (domain.TalkFilter, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return domain.TalkFilter{}, false
	}
	filter := domain.TalkFilter{
		Period:             domain.TalkPeriodAll,
		IncludeUnpublished: includeUnpublished,
		Now:                naiveMadridNow(time.Now()),
	}
	values, present := query["period"]
	if !present {
		return filter, true
	}
	if len(values) != 1 {
		return domain.TalkFilter{}, false
	}
	switch values[0] {
	case domain.TalkPeriodAll, domain.TalkPeriodUpcoming, domain.TalkPeriodPast:
		filter.Period = values[0]
		return filter, true
	default:
		return domain.TalkFilter{}, false
	}
}

func parseUserQuery(r *http.Request) (string, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", false
	}
	values, present := query["q"]
	if !present {
		return "", true
	}
	if len(values) != 1 || !domain.ValidText(values[0]) {
		return "", false
	}
	search := strings.TrimSpace(values[0])
	if len(search) > domain.MaxUserSearchLength {
		return "", false
	}
	return search, true
}
