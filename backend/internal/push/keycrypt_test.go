package push

import (
	"strings"
	"testing"

	"github.com/shiroha-a/town/internal/miauth"
)

func TestSealOpenRoundTrip(t *testing.T) {
	c, err := miauth.NewTokenCipher("test-key")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{cipher: c}

	sealed, err := s.seal("secret-vapid-key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, encPrefix) {
		t.Fatalf("封をした印が付いていない: %q", sealed)
	}
	if strings.Contains(sealed, "secret-vapid-key") {
		t.Error("平文がそのまま残っている")
	}
	got, err := s.open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret-vapid-key" {
		t.Errorf("open = %q, want %q", got, "secret-vapid-key")
	}
}

// 鍵の無い環境が既存の平文を読めること(暗号化を入れる前からある行)。
func TestOpenPlaintextWithoutCipher(t *testing.T) {
	s := &Service{}
	got, err := s.open("plain-old-key")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain-old-key" {
		t.Errorf("open = %q", got)
	}
	sealed, err := s.seal("plain-old-key")
	if err != nil {
		t.Fatal(err)
	}
	if sealed != "plain-old-key" {
		t.Errorf("鍵が無いのに封をしている: %q", sealed)
	}
}

// 封をした鍵を、鍵の無いプロセスが読もうとしたら止まること。ここで黙って
// 新しい鍵を作ると、全員の通知購読が無効になる。web と worker で
// TOWN_TOKEN_KEY が食い違うと起きる。
func TestOpenSealedWithoutCipherFails(t *testing.T) {
	c, err := miauth.NewTokenCipher("test-key")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := (&Service{cipher: c}).seal("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Service{}).open(sealed); err == nil {
		t.Error("鍵が無いのに開けてしまった")
	}
}
