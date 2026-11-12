package maxclient

import (
	"strings"
	"testing"
)

func TestDescribeAttachments_NamesAndTypesOnly(t *testing.T) {
	raw := []byte(`{"vcf_info":"TEL:+79991234567","max_info":{"user_id":42,"name":"Иван"}}`)
	got := DescribeAttachments([]Attachment{{Type: "contact", Payload: raw}})
	if strings.Contains(got, "79991234567") || strings.Contains(got, "Иван") || strings.Contains(got, "TEL") {
		t.Fatalf("value leaked: %s", got)
	}
	for _, want := range []string{"type=contact", "vcf_info=string", "max_info=object", "user_id=number", "name=string"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
}
