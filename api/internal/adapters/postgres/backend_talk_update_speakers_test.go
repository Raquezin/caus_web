package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointTalkUpdateKeepsSpeakerWithDeletedAccount(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	deleted := insertEndpointTalkUpdatePerson(t, tx, true)
	member := insertEndpointTalkUpdatePerson(t, tx, true)
	talkID := insertEndpointTalkUpdateTalk(t, tx, "Legacy talk "+uuid.NewString())
	attachEndpointTalkUpdateSpeakers(t, tx, talkID, deleted)
	if err := repo.DeleteAccount(ctx, deleted); err != nil {
		t.Fatal(err)
	}

	kept := []uuid.UUID{deleted}
	updated, err := repo.UpdateTalk(ctx, talkID, domain.TalkPatch{SpeakerIDs: &kept})
	if err != nil {
		t.Fatalf("keep deleted speaker: %v", err)
	}
	speakers := endpointTalkUpdateSpeakerSet(t, updated)
	if len(speakers) != 1 || !speakers[deleted] {
		t.Fatalf("kept speakers %+v", updated.Speakers)
	}

	withMember := []uuid.UUID{deleted, member}
	updated, err = repo.UpdateTalk(ctx, talkID, domain.TalkPatch{SpeakerIDs: &withMember})
	if err != nil {
		t.Fatalf("add registered speaker next to deleted one: %v", err)
	}
	speakers = endpointTalkUpdateSpeakerSet(t, updated)
	if len(speakers) != 2 || !speakers[deleted] || !speakers[member] {
		t.Fatalf("speakers after adding a registered co-speaker: %+v", updated.Speakers)
	}
}

func TestEndpointTalkUpdateRequiresRegisteredAddedSpeakers(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	deleted := insertEndpointTalkUpdatePerson(t, tx, true)
	unregistered := insertEndpointTalkUpdatePerson(t, tx, false)
	registered := insertEndpointTalkUpdatePerson(t, tx, true)
	legacyTalk := insertEndpointTalkUpdateTalk(t, tx, "Legacy talk "+uuid.NewString())
	attachEndpointTalkUpdateSpeakers(t, tx, legacyTalk, deleted)
	registeredTalk := insertEndpointTalkUpdateTalk(t, tx, "Registered talk "+uuid.NewString())
	attachEndpointTalkUpdateSpeakers(t, tx, registeredTalk, registered)
	if err := repo.DeleteAccount(ctx, deleted); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		talk uuid.UUID
		ids  []uuid.UUID
	}{
		"unregistered next to deleted": {legacyTalk, []uuid.UUID{deleted, unregistered}},
		"deleted added to registered":  {registeredTalk, []uuid.UUID{registered, deleted}},
	}
	for name, testCase := range cases {
		ids := testCase.ids
		_, err := repo.UpdateTalk(ctx, testCase.talk, domain.TalkPatch{SpeakerIDs: &ids})
		var validation domain.ValidationError
		if !errors.As(err, &validation) ||
			validation.Fields["speaker_ids"] != "speaker_ids must reference registered people" {
			t.Fatalf("%s: error %v", name, err)
		}
	}
}
