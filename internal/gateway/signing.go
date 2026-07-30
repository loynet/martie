package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func signature(secret, timestamp, method, path string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write([]byte(method))
	mac.Write([]byte("."))
	mac.Write([]byte(path))
	if body != nil {
		mac.Write([]byte("."))
		mac.Write(body)
	}
	return "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func validSignature(got, want string) bool {
	gotHex, ok := strings.CutPrefix(got, "hmac-sha256=")
	if !ok {
		return false
	}
	gotBytes, err := hex.DecodeString(gotHex)
	if err != nil {
		return false
	}
	wantHex, _ := strings.CutPrefix(want, "hmac-sha256=")
	wantBytes, err := hex.DecodeString(wantHex)
	if err != nil {
		return false
	}
	return hmac.Equal(gotBytes, wantBytes)
}
