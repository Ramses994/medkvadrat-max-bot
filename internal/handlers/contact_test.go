package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/reminders"
)

func hmacHex(token, vcf string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(vcf))
	return hex.EncodeToString(mac.Sum(nil))
}

func contactJSON(vcf, hash string, userID int64) string {
	body := map[string]any{"vcf_info": vcf}
	if hash != "" {
		body["hash"] = hash
	}
	if userID != 0 {
		body["max_info"] = map[string]any{"user_id": userID}
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func dialogCallback(user int64, payload string) *maxclient.Update {
	return &maxclient.Update{
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog"}},
		Callback: &maxclient.Callback{CallbackID: "cb", Payload: payload, User: &maxclient.User{UserID: user}},
	}
}

func contactUpdate(sender int64, payload string) *maxclient.Update {
	return &maxclient.Update{Message: &maxclient.Message{
		Sender:    &maxclient.User{UserID: sender},
		Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
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
	ownVCF := "TEL;TYPE=CELL:+7 (999) 111-22-33"
	own := contactJSON(ownVCF, hmacHex(testBotToken, ownVCF), 42)
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
	if strings.Count(logs.String(), "contact shape") != 1 {
		t.Fatalf("shape log count=%d %s", strings.Count(logs.String(), "contact shape"), logs.String())
	}

	otherVCF := "TEL:+79990000000"
	other := contactJSON(otherVCF, hmacHex(testBotToken, otherVCF), 99)
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
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON("TEL:+79991112233", "", 0))); err != nil {
		t.Fatal(err)
	}
	twoVCF := "TEL:+79991112233\nTEL:+79990000000"
	two := contactJSON(twoVCF, hmacHex(testBotToken, twoVCF), 42)
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
		Sender: &maxclient.User{UserID: 7}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
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
		Sender: &maxclient.User{UserID: 7}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
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
	if action == "accept" && kind == "ads" {
		s.view.Ads = "active"
		s.view.AdsVersion = version
	}
	return nil
}

func (s *scriptConsent) DueReminders(context.Context, time.Time, time.Time, []int64) ([]gateway.DueReminder, error) {
	return []gateway.DueReminder{{
		PlanningID:        11,
		PatientID:         100,
		DoctorName:        "Смирнов",
		MessengerNotify:   s.view.MessengerNotify,
		BotConsentService: s.view.Service,
		BranchID:          106,
		DateConsultation:  "2026-06-24 10:30",
	}}, nil
}

func (s *scriptConsent) BookingByPlanningID(context.Context, int64) (gateway.BookingSnapshot, error) {
	rows, err := s.DueReminders(context.Background(), time.Time{}, time.Time{}, nil)
	if err != nil || len(rows) == 0 {
		return gateway.BookingSnapshot{}, err
	}
	return gateway.BookingSnapshot{NewBooking: gateway.NewBooking{DueReminder: rows[0]}}, nil
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
	if err := h.sendServiceConsent(context.Background(), 1, 42, 100); err != nil {
		t.Fatal(err)
	}
	payload := h.max.(*fakeMax).keyboards[0].rows[0][0].Payload
	press := func() {
		t.Helper()
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, payload)); err != nil {
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
		Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
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

func TestDisableNotifications_RecordsRevokeAndSkipsTheNextReminder(t *testing.T) {
	gw := &fakeGW{}
	h, st := testHandler(t, []int64{100}, gw)
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	one := 1
	cons := &scriptConsent{view: gateway.ConsentView{MessengerNotify: &one, Service: "none"}}
	h.Consent = cons
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
		Body: &maxclient.MessageBody{Text: "Отключить уведомления"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cons.posts) != 1 || cons.posts[0] != "service:service-v1:revoke" || cons.view.Service != "revoked" {
		t.Fatalf("recorded=%+v posts=%v", cons.view, cons.posts)
	}
	sent := &sentUsers{}
	runner := &reminders.Runner{
		Gateway: cons, Storage: st, Messenger: sent,
		Allowlist: []int64{100}, LinkAllowlist: []int64{100},
		Now: func() time.Time {
			return time.Date(2026, time.June, 23, 10, 30, 0, 0, reminders.MoscowLocation())
		},
	}
	runner.Tick(context.Background())
	if len(sent.ids) != 0 {
		t.Fatalf("revoked reminder was sent to %v", sent.ids)
	}
}

type sentUsers struct{ ids []int64 }

func (s *sentUsers) SendToUser(context.Context, int64, string, int64, string) error {
	s.ids = append(s.ids, 42)
	return nil
}

func TestEnableNotifications_FlagOffHidesTheDraft(t *testing.T) {
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	zero := 0
	cons := &scriptConsent{view: gateway.ConsentView{MessengerNotify: &zero, Service: "none"}}
	h.Consent = cons
	h.ConsentEnabled = false
	say := func(text string) {
		t.Helper()
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: text},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	say("Включить уведомления")
	fm := h.max.(*fakeMax)
	if !strings.Contains(fm.messages[len(fm.messages)-1], "обратитесь в регистратуру") {
		t.Fatalf("questionnaire 0: %q", fm.messages[len(fm.messages)-1])
	}
	cons.view.MessengerNotify = nil
	say("Включить уведомления")
	last := fm.messages[len(fm.messages)-1]
	if strings.Contains(last, "[") || !strings.Contains(last, "пока нельзя") {
		t.Fatalf("flag off: %q", last)
	}
	err := h.OnMessageCallback(context.Background(), dialogCallback(42, "consent:service:accept:100"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cons.posts) != 0 || len(fm.answers) == 0 || fm.answers[len(fm.answers)-1] != consentStale {
		t.Fatalf("posts=%v answers=%v", cons.posts, fm.answers)
	}
}

func TestAdsConsent_GoesToTheUser(t *testing.T) {
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	h.Consent = &scriptConsent{view: gateway.ConsentView{Service: "none"}}
	h.ConsentEnabled = true
	h.AdsEnabled = true
	h.ServiceVersion = "service-v1"
	if err := h.sendServiceConsent(context.Background(), 1, 42, 100); err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	err := h.OnMessageCallback(context.Background(), dialogCallback(42, fm.keyboards[0].rows[0][0].Payload))
	if err != nil {
		t.Fatal(err)
	}
	last := fm.keyboards[len(fm.keyboards)-1]
	if !last.byUser || last.recipient != 42 {
		t.Fatalf("keyboards=%+v", fm.keyboards)
	}
}

func TestConsentCallback_RefusesAnotherProfileAndOldPayload(t *testing.T) {
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	cons := &scriptConsent{view: gateway.ConsentView{Service: "none"}}
	h.Consent = cons
	h.ConsentEnabled = true
	press := func(payload string) {
		t.Helper()
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, payload)); err != nil {
			t.Fatal(err)
		}
	}
	press("consent:service:accept:999")
	press("consent:service:accept")
	fm := h.max.(*fakeMax)
	if len(cons.posts) != 0 || len(fm.answers) != 2 {
		t.Fatalf("posts=%v answers=%v", cons.posts, fm.answers)
	}
	if fm.answers[0] != consentStale || fm.answers[1] != consentStale {
		t.Fatalf("answers=%v", fm.answers)
	}
}

func TestDisableNotifications_WithoutAPI_DoesNotClaimSuccess(t *testing.T) {
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
		Body: &maxclient.MessageBody{Text: "Отключить уведомления"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	last := fm.messages[len(fm.messages)-1]
	if last != "Не удалось сохранить, попробуйте позже" || strings.Contains(last, "отключены") {
		t.Fatalf("msg=%q", last)
	}
}

func TestAdsAccept_RequiresTheSameVersion(t *testing.T) {
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	cons := &scriptConsent{view: gateway.ConsentView{Ads: "active", AdsVersion: "ads-v1"}}
	h.Consent = cons
	h.AdsEnabled = true
	h.AdsVersion = "ads-v1"
	if err := h.sendAdsConsent(context.Background(), 42, 100); err != nil {
		t.Fatal(err)
	}
	payload := h.max.(*fakeMax).keyboards[0].rows[0][0].Payload
	press := func(p string) {
		t.Helper()
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, p)); err != nil {
			t.Fatal(err)
		}
	}
	press(payload)
	if len(cons.posts) != 0 {
		t.Fatalf("same version posted %v", cons.posts)
	}
	h.AdsVersion = "ads-v2"
	press(payload)
	if len(cons.posts) != 0 {
		t.Fatalf("old button adopted the new version %v", cons.posts)
	}
}
