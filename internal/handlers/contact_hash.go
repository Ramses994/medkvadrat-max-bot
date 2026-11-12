package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// checkContactHash verifies payload.hash as HMAC-SHA256(bot token, vcf_info).
// maxo compares the hex digest; maxigo also accepts base64 of the same digest.
// The vCard is tried as JSON delivered it and with a literal \r\n turned into a newline.
// The returned label is safe to log: it names the match and never the token, hash, or vCard.
func checkContactHash(token, vcf, hash string) (bool, string) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return false, "missing"
	}
	if token == "" || vcf == "" {
		return false, "fail"
	}
	messages := []struct{ name, vcf string }{{"raw", vcf}}
	if replaced := strings.ReplaceAll(vcf, `\r\n`, "\n"); replaced != vcf {
		messages = append(messages, struct{ name, vcf string }{"lf", replaced})
	}
	for _, msg := range messages {
		mac := hmac.New(sha256.New, []byte(token))
		mac.Write([]byte(msg.vcf))
		sum := mac.Sum(nil)
		if got, err := hex.DecodeString(hash); err == nil && hmac.Equal(got, sum) {
			return true, "hex " + msg.name
		}
		for _, enc := range []struct {
			name string
			enc  *base64.Encoding
		}{
			{"b64", base64.StdEncoding},
			{"b64url", base64.URLEncoding},
			{"b64raw", base64.RawStdEncoding},
			{"b64urlraw", base64.RawURLEncoding},
		} {
			got, err := enc.enc.DecodeString(hash)
			if err == nil && hmac.Equal(got, sum) {
				return true, enc.name + " " + msg.name
			}
		}
	}
	return false, "fail"
}
