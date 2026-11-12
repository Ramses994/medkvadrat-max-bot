package handlers

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/linkallow"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

type fakeMax struct {
	messages     []string
	answers      []string
	replaced     string
	notification string
	keyboards    []keyboardSend
}

type keyboardSend struct {
	recipient int64
	byUser    bool
	text      string
	rows      [][]maxclient.CallbackButton
}

func (f *fakeMax) SendMessage(_ context.Context, _ int64, text string) error {
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeMax) AnswerCallback(_ context.Context, _, text string) error {
	f.answers = append(f.answers, text)
	return nil
}

func (f *fakeMax) AnswerCallbackReplace(_ context.Context, _, notification, text string) error {
	f.notification = notification
	f.replaced = text
	return nil
}

func (f *fakeMax) SendMessageWithKeyboard(_ context.Context, recipient int64, byUser bool, text string, rows [][]maxclient.CallbackButton) error {
	f.messages = append(f.messages, text)
	f.keyboards = append(f.keyboards, keyboardSend{recipient: recipient, byUser: byUser, text: text, rows: rows})
	return nil
}

func (f *fakeMax) SendContactRequest(_ context.Context, _ int64, text, _ string) error {
	f.messages = append(f.messages, text)
	return nil
}

type fakeGW struct {
	patients    []gateway.Patient
	labCalls    int
	searchCalls int
	labPanels   []gateway.LabPanel
	searchErr   error
}

func (f *fakeGW) SearchByPhone(context.Context, string) ([]gateway.Patient, error) {
	f.searchCalls++
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.patients, nil
}

func (f *fakeGW) GetLabPanels(context.Context, int, int) ([]gateway.LabPanel, error) {
	f.labCalls++
	return f.labPanels, nil
}

func (f *fakeGW) PostConfirmation(context.Context, int64, string, int64, string) (string, bool, bool, error) {
	return "confirmed", false, false, nil
}

type seqGW struct {
	lists [][]gateway.Patient
	n     int
}

func (s *seqGW) SearchByPhone(context.Context, string) ([]gateway.Patient, error) {
	i := s.n
	if i >= len(s.lists) {
		i = len(s.lists) - 1
	}
	s.n++
	return s.lists[i], nil
}

func (s *seqGW) GetLabPanels(context.Context, int, int) ([]gateway.LabPanel, error) {
	return nil, nil
}

func (s *seqGW) PostConfirmation(context.Context, int64, string, int64, string) (string, bool, bool, error) {
	return "", false, false, nil
}

func TestPatientSelection_UsesShownOrderWhenSearchReverses(t *testing.T) {
	shown := []gateway.Patient{{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"}, {PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}}
	gw := &seqGW{lists: [][]gateway.Patient{shown, {{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}, {PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"}}}}
	h, st := testHandler(t, []int64{10, 20}, &fakeGW{})
	h.gateway = gw
	fm := h.max.(*fakeMax)

	if err := h.handleAuthentication(context.Background(), 1, 42, "+79991112233"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.messages[0], "1. Т. Т., 1985") || !strings.Contains(fm.messages[0], "2. М. Д., 1980") || strings.Contains(fm.messages[0], "Тест Тестовый") || strings.Contains(fm.messages[0], "Мартынов Дмитрий") {
		t.Fatalf("list=%q", fm.messages[0])
	}
	_, ids, _, _, ok, err := st.PendingChoice(42, time.Now())
	if err != nil || !ok || len(ids) != 2 || ids[0] != 10 || ids[1] != 20 {
		t.Fatalf("saved ids=%v ok=%v err=%v", ids, ok, err)
	}
	if err := h.handlePatientSelection(context.Background(), 1, 42, "79991112233", "1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.messages[len(fm.messages)-1], "ДД.ММ.ГГГГ") {
		t.Fatalf("ask=%q", fm.messages[len(fm.messages)-1])
	}
	if err := h.handlePatientSelection(context.Background(), 1, 42, "79991112233", "01.01.1985"); err != nil {
		t.Fatal(err)
	}
	link, err := st.GetByUserID(42)
	if err != nil || link == nil || link.PatientID != 10 || link.FullName != "Тест Тестовый" {
		t.Fatalf("link=%+v err=%v", link, err)
	}
	if !strings.Contains(fm.messages[len(fm.messages)-1], "Тест Тестовый") || strings.Contains(fm.messages[len(fm.messages)-1], "Мартынов Дмитрий") {
		t.Fatalf("confirm=%q", fm.messages[len(fm.messages)-1])
	}
	if _, _, _, _, still, _ := st.PendingChoice(42, time.Now()); still {
		t.Fatal("pending choice must be deleted after bind")
	}
}

func TestPatientSelection_ExpiredRefuses(t *testing.T) {
	h, st := testHandler(t, []int64{10, 20}, &fakeGW{})
	h.gateway = &seqGW{lists: [][]gateway.Patient{{{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"}, {PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}}}}
	if err := h.handleAuthentication(context.Background(), 1, 42, "+79991112233"); err != nil {
		t.Fatal(err)
	}
	if err := st.SavePendingChoice(42, "79991112233", []int64{10, 20}, time.Now().Add(-16*time.Minute)); err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	if err := h.handlePatientSelection(context.Background(), 1, 42, "79991112233", "1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.messages[len(fm.messages)-1], "Список устарел") {
		t.Fatalf("msg=%q", fm.messages[len(fm.messages)-1])
	}
	link, _ := st.GetByUserID(42)
	if link != nil && link.PatientID != 0 {
		t.Fatalf("bound %d", link.PatientID)
	}
}

func TestPatientSelection_MissingIDRefuses(t *testing.T) {
	gw := &seqGW{lists: [][]gateway.Patient{
		{{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}, {PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"}},
		{{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}},
	}}
	h, st := testHandler(t, []int64{10, 20}, &fakeGW{})
	h.gateway = gw
	if err := h.handleAuthentication(context.Background(), 1, 42, "+79991112233"); err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	if err := h.handlePatientSelection(context.Background(), 1, 42, "79991112233", "1"); err != nil {
		t.Fatal(err)
	}
	if err := h.handlePatientSelection(context.Background(), 1, 42, "79991112233", "01.01.1985"); err != nil {
		t.Fatal(err)
	}
	link, _ := st.GetByUserID(42)
	if link == nil || link.PatientID != 0 {
		t.Fatalf("must stay unbound, link=%+v", link)
	}
	if strings.Contains(fm.messages[len(fm.messages)-1], "успешно") {
		t.Fatalf("msg=%q", fm.messages[len(fm.messages)-1])
	}
}

func TestPatientSelection_OutOfRangeRefuses(t *testing.T) {
	h, st := testHandler(t, []int64{10, 20}, &fakeGW{})
	h.gateway = &seqGW{lists: [][]gateway.Patient{{{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"}, {PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}}}}
	if err := h.handleAuthentication(context.Background(), 1, 42, "+79991112233"); err != nil {
		t.Fatal(err)
	}
	if err := h.handlePatientSelection(context.Background(), 1, 42, "79991112233", "3"); err != nil {
		t.Fatal(err)
	}
	link, _ := st.GetByUserID(42)
	if link == nil || link.PatientID != 0 {
		t.Fatalf("must stay unbound, link=%+v", link)
	}
}

func TestChangeNumber_DeletesPendingChoice(t *testing.T) {
	h, st := testHandler(t, []int64{10, 20}, &fakeGW{})
	h.gateway = &seqGW{lists: [][]gateway.Patient{{{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"}, {PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}}}}
	if err := h.handleAuthentication(context.Background(), 1, 42, "+79991112233"); err != nil {
		t.Fatal(err)
	}
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender:    &maxclient.User{UserID: 42},
		Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
		Body:      &maxclient.MessageBody{Text: "Сменить номер"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, ok, _ := st.PendingChoice(42, time.Now()); ok {
		t.Fatal("pending choice remains")
	}
	if link, _ := st.GetByUserID(42); link != nil {
		t.Fatal("link remains")
	}
}

const testBotToken = "test-token"

func testHandler(t *testing.T, allow []int64, gw *fakeGW) (*Handler, *storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	st, err := storage.New(filepath.Join(dir, "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := &Handler{
		max:       &fakeMax{},
		gateway:   gw,
		storage:   st,
		LinkAllow: linkallow.New(allow),
		BotToken:  testBotToken,
	}
	return h, st
}

func TestAuthentication_PatientOutsideAllowlistNotLinked(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 999, FullName: "Test Patient", Phone: "79990000000"}}}
	h, st := testHandler(t, []int64{100}, gw)
	fm := h.max.(*fakeMax)

	err := h.handleAuthentication(context.Background(), 1, 42, "+79990000000")
	if err != nil {
		t.Fatal(err)
	}
	link, err := st.GetByUserID(42)
	if err != nil {
		t.Fatal(err)
	}
	if link != nil {
		t.Fatalf("expected no link, got patient_id=%d", link.PatientID)
	}
	if len(fm.messages) != 1 || !strings.Contains(fm.messages[0], botTestModeText) {
		t.Fatalf("messages=%v", fm.messages)
	}
}

func TestAuthentication_UnknownPhoneWithAllowlist_SameTestMode(t *testing.T) {
	gw := &fakeGW{patients: nil}
	h, st := testHandler(t, []int64{100}, gw)
	fm := h.max.(*fakeMax)

	if err := h.handleAuthentication(context.Background(), 1, 7, "+79991112233"); err != nil {
		t.Fatal(err)
	}
	if link, _ := st.GetByUserID(7); link != nil {
		t.Fatal("must not link")
	}
	if len(fm.messages) != 1 || fm.messages[0] != contactNotFound {
		t.Fatalf("messages=%v", fm.messages)
	}
}

func TestSendWelcome_OutsideAllowlist_NoFullName(t *testing.T) {
	h, _ := testHandler(t, []int64{100}, &fakeGW{})
	st, _ := storage.New(filepath.Join(t.TempDir(), "b.db"))
	defer st.Close()
	_ = st.LinkVerified(1, 999, "79990000000", "Secret Name", time.Now())
	link, _ := st.GetByUserID(1)
	h.storage = st
	fm := h.max.(*fakeMax)
	if err := h.sendWelcome(context.Background(), 1, link); err != nil {
		t.Fatal(err)
	}
	if len(fm.messages) != 1 || fm.messages[0] != botTestModeText {
		t.Fatalf("messages=%q", fm.messages)
	}
}

func TestAuthentication_EmptyAllowlistDeniesLink(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 1, FullName: "One"}}}
	h, st := testHandler(t, nil, gw)

	if err := h.handleAuthentication(context.Background(), 1, 7, "+79991112233"); err != nil {
		t.Fatal(err)
	}
	link, _ := st.GetByUserID(7)
	if link != nil {
		t.Fatal("must not link when allowlist empty")
	}
	fm := h.max.(*fakeMax)
	if len(fm.messages) != 1 || fm.messages[0] != botTestModeText {
		t.Fatalf("messages=%v", fm.messages)
	}
}

func TestLabPanels_LinkedOutsideAllowlistBlocked(t *testing.T) {
	gw := &fakeGW{labPanels: []gateway.LabPanel{{PanelName: "CBC", TestsCount: 3}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := st.LinkVerified(55, 999, "79990000000", "Cached Name", time.Now()); err != nil {
		t.Fatal(err)
	}
	link, err := st.GetByUserID(55)
	if err != nil || link == nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)

	err = h.sendLabPanels(context.Background(), 1, link)
	if err != nil {
		t.Fatal(err)
	}
	if gw.labCalls != 0 {
		t.Fatalf("GetLabPanels called %d times", gw.labCalls)
	}
	if len(fm.messages) != 1 || !strings.Contains(fm.messages[0], botTestModeText) {
		t.Fatalf("messages=%v", fm.messages)
	}
}

func TestLabPanels_AllowedPatientGetsLabs(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Allowed", BirthDate: "1985-01-01"}}, labPanels: []gateway.LabPanel{{PanelName: "CBC", TestsCount: 1, ReadyAt: "2026-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := st.LinkVerified(55, 100, "79990000000", "Allowed", time.Now()); err != nil {
		t.Fatal(err)
	}
	link, _ := st.GetByUserID(55)
	fm := h.max.(*fakeMax)

	if err := h.sendLabPanels(context.Background(), 1, link); err != nil {
		t.Fatal(err)
	}
	if gw.labCalls != 1 {
		t.Fatal("expected lab fetch")
	}
	if len(fm.messages) != 1 || !strings.Contains(fm.messages[0], "анализ") {
		t.Fatalf("messages=%v", fm.messages)
	}
}
