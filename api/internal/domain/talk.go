package domain

import (
	"math"
	"time"

	"github.com/google/uuid"
)

const MaxTalkDurationSeconds = int64(math.MaxInt64) / int64(time.Second)

const (
	TalkPeriodAll      = "all"
	TalkPeriodUpcoming = "upcoming"
	TalkPeriodPast     = "past"
)

type Talk struct {
	ID          uuid.UUID
	Title       string
	Description string
	Location    string
	Language    string
	Date        time.Time
	Duration    int64
	StreamURL   *string
	PosterURL   *string
	Published   bool
	Speakers    []Person
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TalkInput struct {
	Title       string
	Description string
	Location    string
	Language    string
	Date        time.Time
	Duration    int64
	StreamURL   *string
	PosterURL   *string
	Published   bool
	SpeakerIDs  []uuid.UUID
}

type NullableText struct {
	Value *string
}

type TalkPatch struct {
	Title       *string
	Description *string
	Location    *string
	Language    *string
	Date        *time.Time
	Duration    *int64
	StreamURL   *NullableText
	PosterURL   *NullableText
	Published   *bool
	SpeakerIDs  *[]uuid.UUID
}

type TalkFilter struct {
	Period             string
	IncludeUnpublished bool
	Now                time.Time
}

type TalkPage struct {
	Talks   []Talk
	HasMore bool
}
