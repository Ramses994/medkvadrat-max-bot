package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

func revisionPair(t *testing.T) (*Handler, *scriptConsent, *fakeMax) {
	t.Helper()
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	h.Now = func() time.Time { return now }
	if err := st.LinkVerified(1, 100, "79991112233", "Аккаунт А", now); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkVerified(2, 100, "79991112233", "Аккаунт Б", now); err != nil {
		t.Fatal(err)
	}
	cons := &scriptConsent{view: gateway.ConsentView{Service: "none"}}
	h.Consent = cons
	h.ConsentEnabled = true
	h.ServiceVersion = "service-v1"
	st.FailConsentExpiry(errors.New("disk"))
	return h, cons, h.max.(*fakeMax)
}

func sayRevoke(t *testing.T, h *Handler, user int64) {
	t.Helper()
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: user}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
		Body: &maxclient.MessageBody{Text: "Отключить уведомления"},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsentRevision_SameSecondRevokeRejectsTheOtherButton(t *testing.T) {
	h, cons, fm := revisionPair(t)
	if err := h.sendServiceConsent(context.Background(), 1, 2, 100); err != nil {
		t.Fatal(err)
	}
	payload := fm.keyboards[0].rows[0][0].Payload
	sayRevoke(t, h, 1)
	before := len(cons.posts)
	if err := h.OnMessageCallback(context.Background(), dialogCallback(2, payload)); err != nil {
		t.Fatal(err)
	}
	if len(cons.posts) != before || cons.view.Service != "revoked" || fm.answers[len(fm.answers)-1] != consentStale {
		t.Fatalf("posts=%v state=%s answers=%v", cons.posts, cons.view.Service, fm.answers)
	}
}

func TestConsentRevision_RepeatRevokeRejectsTheButtonShownBetween(t *testing.T) {
	h, cons, fm := revisionPair(t)
	sayRevoke(t, h, 1)
	if err := h.sendServiceConsent(context.Background(), 1, 2, 100); err != nil {
		t.Fatal(err)
	}
	payload := fm.keyboards[0].rows[0][0].Payload
	sayRevoke(t, h, 1)
	before := len(cons.posts)
	if err := h.OnMessageCallback(context.Background(), dialogCallback(2, payload)); err != nil {
		t.Fatal(err)
	}
	accepts := 0
	for _, post := range cons.posts[before:] {
		if strings.HasSuffix(post, ":accept") {
			accepts++
		}
	}
	if accepts != 0 || cons.view.Service != "revoked" || fm.answers[len(fm.answers)-1] != consentStale {
		t.Fatalf("posts=%v state=%s answers=%v", cons.posts, cons.view.Service, fm.answers)
	}
}

func TestConsentRevision_ScreenAfterTheLastRevokeIsAccepted(t *testing.T) {
	h, cons, fm := revisionPair(t)
	sayRevoke(t, h, 1)
	if err := h.sendServiceConsent(context.Background(), 1, 2, 100); err != nil {
		t.Fatal(err)
	}
	payload := fm.keyboards[0].rows[0][0].Payload
	if err := h.OnMessageCallback(context.Background(), dialogCallback(2, payload)); err != nil {
		t.Fatal(err)
	}
	if cons.view.Service != "active" || len(cons.posts) == 0 || !strings.HasSuffix(cons.posts[len(cons.posts)-1], ":accept") {
		t.Fatalf("posts=%v state=%s", cons.posts, cons.view.Service)
	}
}

func TestConsentButton_WithoutRevisionIsStale(t *testing.T) {
	h, cons, fm := revisionPair(t)
	h.storage.OmitNextPromptRevision()
	if err := h.sendServiceConsent(context.Background(), 1, 2, 100); err != nil {
		t.Fatal(err)
	}
	payload := fm.keyboards[0].rows[0][0].Payload
	before := len(cons.posts)
	if err := h.OnMessageCallback(context.Background(), dialogCallback(2, payload)); err != nil {
		t.Fatal(err)
	}
	if len(cons.posts) != before || fm.answers[len(fm.answers)-1] != consentStale {
		t.Fatalf("posts=%v answers=%v", cons.posts, fm.answers)
	}
}
