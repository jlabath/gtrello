package gtrello

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// checkMAC returns true if messageMAC is a valid HMAC tag for message.
func checkMAC(message, messageMAC, key []byte) bool {
	mac := hmac.New(sha1.New, key)
	mac.Write(message)
	expectedMAC := mac.Sum(nil)
	return hmac.Equal(messageMAC, expectedMAC)
}

//parseSignature parses signature such as sha1=0566101a3c64b9aad1f0352f10082df504d0d442
//and returns an error if there was a problem
func parseSignature(raw string) ([]byte, error) {
	if !isValidStr(raw) {
		return nil, fmt.Errorf("empty signature")
	}
	sary := strings.Split(raw, "=")
	if len(sary) != 2 {
		return nil, fmt.Errorf("malformated signature \"%s\"", raw)
	}
	if sary[0] != "sha1" {
		return nil, fmt.Errorf("only sha1 supported got \"%s\"", sary[0])
	}
	return hex.DecodeString(sary[1])
}

//return true on non empty nonnil strings
func isValidStr(s string) bool {
	if &s != nil {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

//getCardId parses the card id out of url
//we can be reckless here since cmgparser should have this sorted out
func getCardId(url string) string {
	sary := strings.Split(url, "/")
	idx := len(sary) - 1
	if idx < 1 {
		return ""
	}
	return sary[idx]
}
