package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostConfirmation_OK(t *testing.T) {
	var got confirmationBody
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/confirmations" || r.Method != http.MethodPost {
			t.Fatalf("path=%s method=%s", r.URL.Path, r.Method)
		}
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"planning_id":11737097,"status":"confirmed"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.PostConfirmation(context.Background(), 11737097, "confirmed", 1587578); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer secret-token" {
		t.Fatalf("auth=%q", auth)
	}
	if got.PlanningID != 11737097 || got.PatientID != 1587578 || got.Status != "confirmed" || got.Source != "max" {
		t.Fatalf("body=%+v", got)
	}
}

func TestPostConfirmation_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"success":false,"error_details":{"code":"PATIENT_MISMATCH"}}`)
	}))
	defer srv.Close()

	err := New(srv.URL, "t").PostConfirmation(context.Background(), 1, "confirmed", 2)
	he, ok := err.(*HTTPError)
	if !ok || !he.IsForbidden() || he.Code != "PATIENT_MISMATCH" {
		t.Fatalf("err=%v", err)
	}
}

func TestPostConfirmation_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"success":false,"error_details":{"code":"APPOINTMENT_NOT_FOUND"}}`)
	}))
	defer srv.Close()

	err := New(srv.URL, "t").PostConfirmation(context.Background(), 1, "declined", 2)
	he, ok := err.(*HTTPError)
	if !ok || !he.IsNotFound() {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(he.Error(), "404") {
		t.Fatalf("error=%v", he)
	}
}
