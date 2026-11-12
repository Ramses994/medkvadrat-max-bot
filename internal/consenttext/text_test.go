package consenttext

import "testing"

func TestCheck_DefaultFlagsAllowDraftText(t *testing.T) {
	if err := Check(false, false, ServiceVersion, AdsVersion); err != nil {
		t.Fatal(err)
	}
}

func TestCheck_EnabledTemplateRefusesToStart(t *testing.T) {
	if err := Check(true, false, ServiceVersion, AdsVersion); err == nil {
		t.Fatal("expected template error")
	}
	if err := Check(false, true, ServiceVersion, AdsVersion); err == nil {
		t.Fatal("expected ads template error")
	}
}
