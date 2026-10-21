package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostConfirmation_OK(t *testing.T) {
	var got confirmationBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"planning_id":11737097,"status":"confirmed"}}`))
	}))
	defer srv.Close()

	stored, already, stale, err := New(srv.URL, "secret-token").PostConfirmation(context.Background(), 11737097, "confirmed", 1587578, "ver1")
	if err != nil || already || stale || stored != "confirmed" {
		t.Fatalf("stored=%s already=%v stale=%v err=%v", stored, already, stale, err)
	}
	if got.ApptVersion != "ver1" {
		t.Fatalf("body=%+v", got)
	}
}

func TestPostConfirmation_Stale(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"success":false,"error_details":{"code":"STALE_REMINDER"}}`)
	}))
	defer srv.Close()
	_, already, stale, err := New(srv.URL, "t").PostConfirmation(context.Background(), 1, "confirmed", 2, "old")
	if err != nil || already || !stale {
		t.Fatalf("already=%v stale=%v err=%v", already, stale, err)
	}
}

func TestPostConfirmation_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"success":false,"error_details":{"code":"PATIENT_MISMATCH"}}`)
	}))
	defer srv.Close()
	_, _, _, err := New(srv.URL, "t").PostConfirmation(context.Background(), 1, "confirmed", 2, "v")
	ge, ok := err.(*Error)
	if !ok || ge.Status != 403 || ge.Code != "PATIENT_MISMATCH" {
		t.Fatalf("err=%v", err)
	}
}
