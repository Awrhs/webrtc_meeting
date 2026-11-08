package repository_test

import (
	"testing"
	"time"

	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"conference-sfu/backend/internal/testutil"
)

func TestPublicKeyUpsert(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "repository")
	repo := repository.NewChatRepository(gdb)
	u := seedUser(t, gdb, "keyuser")

	if err := repo.SavePublicKey(&models.E2EEPublicKey{UserID: u.ID, PublicKey: "k1", KeyType: "ecdh_p256"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePublicKey(&models.E2EEPublicKey{UserID: u.ID, PublicKey: `{"v":2,"ecdh":"a","ecdsa":"b"}`, KeyType: "bundle_v2"}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetPublicKey(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.KeyType != "bundle_v2" || got.PublicKey[0] != '{' {
		t.Fatalf("upsert did not replace key: %+v", got)
	}
	var count int64
	gdb.Model(&models.E2EEPublicKey{}).Where("user_id = ?", u.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected a single row per user, got %d", count)
	}
	if err := repo.DeletePublicKey(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetPublicKey(u.ID); err == nil {
		t.Fatal("key should be gone after delete")
	}
}

func TestMessagesPublicAndPrivateSeparation(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "repository")
	chat := repository.NewChatRepository(gdb)
	rooms := repository.NewRoomRepository(gdb)
	owner := seedUser(t, gdb, "owner")
	alice := seedUser(t, gdb, "alice")
	bob := seedUser(t, gdb, "bob")
	room := seedRoom(t, rooms, owner, "100010", 10)

	base := time.Now().Add(-time.Hour)
	mk := func(sender uint, receiver *uint, content string, offset time.Duration) {
		t.Helper()
		if err := chat.CreateMessage(&models.Message{RoomID: room.ID, SenderID: sender, ReceiverID: receiver, Type: models.MessageTypeText, Content: content, CreatedAt: base.Add(offset)}); err != nil {
			t.Fatal(err)
		}
	}
	mk(alice.ID, nil, "pub-1", 1*time.Minute)
	mk(bob.ID, nil, "pub-2", 2*time.Minute)
	mk(alice.ID, &bob.ID, "priv-a-to-b", 3*time.Minute)
	mk(bob.ID, &alice.ID, "priv-b-to-a", 4*time.Minute)
	mk(bob.ID, &owner.ID, "priv-b-to-owner", 5*time.Minute)

	public, err := chat.GetMessages(room.ID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 2 || public[0].Content != "pub-1" || public[1].Content != "pub-2" {
		t.Fatalf("public messages wrong (want chronological, no private): %+v", contents(public))
	}

	private, err := chat.GetPrivateMessages(room.ID, alice.ID, bob.ID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(private) != 2 || private[0].Content != "priv-a-to-b" || private[1].Content != "priv-b-to-a" {
		t.Fatalf("private conversation wrong (both directions, chronological, no third party): %+v", contents(private))
	}
}

func contents(msgs []*models.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}
