package handlers

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/linkallow"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

type fakeMax struct {
	messages []string
}

func (f *fakeMax) SendMessage(_ context.Context, _ int64, text string) error {
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeMax) AnswerCallback(context.Context, string, string) error { return nil }

func (f *fakeMax) SendMessageWithKeyboard(context.Context, int64, bool, string, [][]maxclient.CallbackButton) error {
	return nil
}

type fakeGW struct {
	patients   []gateway.Patient
	labCalls   int
	labPanels  []gateway.LabPanel
	searchErr  error
}

func (f *fakeGW) SearchByPhone(context.Context, string) ([]gateway.Patient, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.patients, nil
}

func (f *fakeGW) GetLabPanels(context.Context, int, int) ([]gateway.LabPanel, error) {
	f.labCalls++
	return f.labPanels, nil
}

func (f *fakeGW) PostConfirmation(context.Context, int64, string, int64) error {
	return nil
}

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
}

func TestLabPanels_LinkedOutsideAllowlistBlocked(t *testing.T) {
	gw := &fakeGW{labPanels: []gateway.LabPanel{{PanelName: "CBC", TestsCount: 3}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := st.Link(55, 999, "79990000000", "Cached Name"); err != nil {
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
	gw := &fakeGW{labPanels: []gateway.LabPanel{{PanelName: "CBC", TestsCount: 1, ReadyAt: "2026-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := st.Link(55, 100, "79990000000", "Allowed"); err != nil {
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
