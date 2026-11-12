package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestContactHash_AcceptsHexAndBase64OfTheSameDigest(t *testing.T) {
	const token = "bot-token"
	vcf := "BEGIN:VCARD\nTEL:+79991112233"
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(vcf))
	sum := mac.Sum(nil)
	for _, hash := range []string{
		hex.EncodeToString(sum),
		stringsToUpper(hex.EncodeToString(sum)),
		base64.StdEncoding.EncodeToString(sum),
		base64.URLEncoding.EncodeToString(sum),
	} {
		ok, how, _ := checkContactHash(token, vcf, hash)
		if !ok || how == "fail" || how == "missing" {
			t.Fatalf("hash %q → ok=%v %s", hash, ok, how)
		}
	}
	literal := `BEGIN:VCARD\r\nTEL:+79991112233`
	mac = hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("BEGIN:VCARD\nTEL:+79991112233"))
	ok, how, _ := checkContactHash(token, literal, hex.EncodeToString(mac.Sum(nil)))
	if !ok || how != "hex lf" {
		t.Fatalf("literal newline ok=%v %s", ok, how)
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
