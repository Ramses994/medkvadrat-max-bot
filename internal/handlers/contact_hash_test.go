package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestContactHash_AcceptsOnlyHexOfTheOriginalVCard(t *testing.T) {
	const token = "bot-token"
	vcf := "BEGIN:VCARD\nTEL:+79991112233"
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(vcf))
	sum := mac.Sum(nil)
	ok, how, verified := checkContactHash(token, vcf, hex.EncodeToString(sum))
	if !ok || how != "ok" || verified != vcf {
		t.Fatalf("hex ok=%v %s verified=%q", ok, how, verified)
	}
	ok, how, _ = checkContactHash(token, vcf, stringsToUpper(hex.EncodeToString(sum)))
	if !ok || how != "ok" {
		t.Fatalf("upper ok=%v %s", ok, how)
	}
	for _, hash := range []string{
		base64.StdEncoding.EncodeToString(sum),
		base64.URLEncoding.EncodeToString(sum),
		base64.RawStdEncoding.EncodeToString(sum),
	} {
		ok, how, _ = checkContactHash(token, vcf, hash)
		if ok || how != "fail" {
			t.Fatalf("base64 accepted ok=%v %s", ok, how)
		}
	}
	literal := `BEGIN:VCARD\r\nTEL:+79991112233`
	ok, how, _ = checkContactHash(token, literal, hex.EncodeToString(sum))
	if ok || how != "fail" {
		t.Fatalf("rewritten card ok=%v %s", ok, how)
	}
	ok, how, _ = checkContactHash(token, vcf, "")
	if ok || how != "missing" {
		t.Fatalf("empty ok=%v %s", ok, how)
	}
}

func stringsToUpper(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'f' {
			out[i] = c - 32
		}
	}
	return string(out)
}
