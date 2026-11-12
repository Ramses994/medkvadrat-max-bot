package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// checkContactHash verifies payload.hash as the hex HMAC-SHA256 of the bot token
// and the vCard text exactly as MAX sent it. The live check matched that form.
// The label is ok, fail, or missing: it never includes the token, hash, or vCard.
func checkContactHash(token, vcf, hash string) (bool, string, string) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return false, "missing", ""
	}
	if token == "" || vcf == "" {
		return false, "fail", ""
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(vcf))
	sum := mac.Sum(nil)
	got, err := hex.DecodeString(hash)
	if err != nil || !hmac.Equal(got, sum) {
		return false, "fail", ""
	}
	return true, "ok", vcf
}
