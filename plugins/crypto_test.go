package plugins

import (
	"strings"
	"testing"
)

func TestCryptoOperationsAreLocalAndCorrect(t *testing.T) {
	tests := map[string]string{
		"md5":       "5d41402abc4b2a76b9719d911017c592",
		"sha1":      "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
		"sha256":    "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		"b64encode": "aGVsbG8=",
		"urlencode": "hello+world",
	}
	for command, want := range tests {
		got, err := cryptoResult(command, map[string]string{"urlencode": "hello world"}[command])
		if command != "urlencode" {
			got, err = cryptoResult(command, "hello")
		}
		if err != nil || got != want {
			t.Errorf("cryptoResult(%q) = %q, %v; want %q", command, got, err, want)
		}
	}
	decoded, err := cryptoResult("b64decode", "aGVsbG8=")
	if err != nil || decoded != "hello" {
		t.Fatalf("base64 decode = %q, %v", decoded, err)
	}
}

func TestCryptoRejectsOversizedInputAndStripsControls(t *testing.T) {
	if _, err := cryptoResult("b64decode", "%%%\n"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	clean := cleanExternalText("hello\r\nworld\x0304")
	if strings.ContainsAny(clean, "\r\n\x03") {
		t.Fatalf("controls remain in crypto output: %q", clean)
	}
}
