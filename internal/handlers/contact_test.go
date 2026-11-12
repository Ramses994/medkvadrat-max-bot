package handlers

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/reminders"
)

func contactUpdate(sender int64, payload string) *maxclient.Update {
	return &maxclient.Update{Message: &maxclient.Message{
		Sender:    &maxclient.User{UserID: sender},
		Recipient: &maxclient.Recipient{ChatID: 1},
		Body: &maxclient.MessageBody{Attachments: []maxclient.Attachment{{
			Type:    "contact",
			Payload: []byte(payload),
		}}},
	}}
}

func TestContact_OwnContactLinksAndOtherIsRefused(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	h.ContactDebug = true
	own := `{"vcf_info":"TEL;TYPE=CELL:+7 (999) 111-22-33","max_info":{"user_id":42}}`
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, own)); err != nil {
		t.Fatal(err)
	}
	link, err := st.GetByUserID(42)
	if err != nil || !link.Verified() || link.PatientID != 100 {
		t.Fatalf("link=%+v err=%v", link, err)
	}
	if strings.Contains(logs.String(), "79991112233") || strings.Contains(logs.String(), "Иванов") {
		t.Fatalf("log leaked %s", logs.String())
	}

	other := `{"vcf_info":"TEL:+79990000000","max_info":{"user_id":99}}`
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, other)); err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	if !strings.Contains(fm.messages[len(fm.messages)-1], "свой контакт") {
		t.Fatalf("msg=%q", fm.messages[len(fm.messages)-1])
	}
	if link, _ := st.GetByUserID(42); link.PatientID != 100 {
		t.Fatal("other contact must not relink")
	}
}

func TestContact_MissingMaxInfoAndTwoNumbersRefuse(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, `{"vcf_info":"TEL:+79991112233"}`)); err != nil {
		t.Fatal(err)
	}
	two := `{"vcf_info":"TEL:+79991112233\nTEL:+79990000000","max_info":{"user_id":42}}`
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, two)); err != nil {
		t.Fatal(err)
	}
	if link, _ := st.GetByUserID(42); link != nil && link.Verified() {
		t.Fatal("refused contact was linked")
	}
	fm := h.max.(*fakeMax)
	if len(fm.messages) != 2 || !strings.Contains(fm.messages[0], "свой контакт") {
		t.Fatalf("messages=%v", fm.messages)
	}
}

func TestTypedPhoneAsksForTheButton(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 7}, Recipient: &maxclient.Recipient{ChatID: 1},
		Body: &maxclient.MessageBody{Text: "+79991112233"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if link, _ := st.GetByUserID(7); link != nil {
		t.Fatal("typed phone must not link")
	}
	fm := h.max.(*fakeMax)
	if len(fm.messages) != 1 || !strings.Contains(fm.messages[0], "Поделиться контактом") {
		t.Fatalf("messages=%v", fm.messages)
	}
}

func TestMinorIsHidden(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Ребёнок Ребёнок", BirthDate: "2015-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	h.Now = func() time.Time { return time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC) }
	if err := h.handleAuthentication(context.Background(), 1, 42, "79991112233"); err != nil {
		t.Fatal(err)
	}
	if link, _ := st.GetByUserID(42); link != nil && link.Verified() {
		t.Fatal("minor was offered")
	}
	fm := h.max.(*fakeMax)
	if !strings.Contains(fm.messages[0], "не нашли пациента") || strings.Contains(fm.messages[0], "Ребёнок") {
		t.Fatalf("messages=%v", fm.messages)
	}
}

func TestUnverifiedLinkGetsNeitherLabsNorReminders(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}, labPanels: []gateway.LabPanel{{PanelName: "CBC"}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := st.Link(7, 100, "79991112233", "Иванов Иван"); err != nil {
		t.Fatal(err)
	}
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 7}, Recipient: &maxclient.Recipient{ChatID: 1},
		Body: &maxclient.MessageBody{Text: "Мои анализы"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if gw.labCalls != 0 {
		t.Fatal("labs were shown")
	}
	ids, err := st.DistinctPatientIDs()
	if err != nil || len(ids) != 0 {
		t.Fatalf("audience=%v err=%v", ids, err)
	}
	fm := h.max.(*fakeMax)
	if strings.Contains(strings.Join(fm.messages, "\n"), "Иванов") {
		t.Fatalf("name leaked %v", fm.messages)
	}
}

type scriptConsent struct {
	view  gateway.ConsentView
	posts []string
}

func (s *scriptConsent) GetConsent(context.Context, int64) (gateway.ConsentView, error) {
	return s.view, nil
}

func (s *scriptConsent) PostConsent(_ context.Context, _, _ int64, kind, version, action string) error {
	s.posts = append(s.posts, kind+":"+version+":"+action)
	if action == "accept" && kind == "service" {
		s.view.Service = "active"
		s.view.ServiceVersion = version
	}
	if action == "revoke" && kind == "service" {
		s.view.Service = "revoked"
	}
	return nil
}

func TestConsentScreen_OnlyWhenMissingAndAcceptIsOnce(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}}
	h, _ := testHandler(t, []int64{100}, gw)
	cons := &scriptConsent{view: gateway.ConsentView{Service: "none", Ads: "none"}}
	h.Consent = cons
	h.ServiceVersion = "service-v1"
	if err := h.handleAuthentication(context.Background(), 1, 42, "79991112233"); err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	if len(fm.messages) != 1 {
		t.Fatalf("flag off must not show the screen: %d", len(fm.messages))
	}
	h.ConsentEnabled = true
	one := 1
	cons.view.MessengerNotify = &one
	if err := h.maybeConsent(context.Background(), 1, 42, 100); err != nil {
		t.Fatal(err)
	}
	if len(fm.messages) != 1 {
		t.Fatal("questionnaire 1 must not show the screen")
	}
	cons.view.MessengerNotify = nil
	cons.view.Service = "active"
	if err := h.maybeConsent(context.Background(), 1, 42, 100); err != nil {
		t.Fatal(err)
	}
	if len(fm.messages) != 1 {
		t.Fatal("active consent must not show the screen")
	}
	cons.view.Service = "none"
	if err := h.maybeConsent(context.Background(), 1, 42, 100); err != nil {
		t.Fatal(err)
	}
	if len(fm.messages) != 2 || !strings.Contains(fm.messages[1], "Уведомления о записях в MAX") {
		t.Fatalf("screen=%v", fm.messages)
	}
}

func TestConsentAccept_PostsVersionOnceAndRevokeBeatsQuestionnaire(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	cons := &scriptConsent{view: gateway.ConsentView{Service: "none"}}
	h.Consent = cons
	h.ConsentEnabled = true
	h.ServiceVersion = "service-v1"
	press := func() {
		t.Helper()
		err := h.OnMessageCallback(context.Background(), &maxclient.Update{Callback: &maxclient.Callback{
			CallbackID: "cb", Payload: "consent:service:accept", User: &maxclient.User{UserID: 42},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	press()
	press()
	if len(cons.posts) != 1 || cons.posts[0] != "service:service-v1:accept" {
		t.Fatalf("posts=%v", cons.posts)
	}
	one := 1
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1},
		Body: &maxclient.MessageBody{Text: "Отключить уведомления"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cons.posts) != 2 || cons.posts[1] != "service:service-v1:revoke" {
		t.Fatalf("posts=%v", cons.posts)
	}
	if reminders.CanNotify(&one, cons.view.Service, false, false) {
		t.Fatal("revocation must beat the questionnaire")
	}
}
