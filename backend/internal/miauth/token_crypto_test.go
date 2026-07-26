package miauth

import "testing"

func TestTokenCipherRoundTrip(t *testing.T) {
	c, err := NewTokenCipher("test-key")
	if err != nil {
		t.Fatalf("NewTokenCipher: %v", err)
	}
	const token = "abcdef0123456789"
	sealed, err := c.Seal(token)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if string(sealed) == token {
		t.Error("ciphertext must not equal plaintext")
	}
	got, err := c.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != token {
		t.Errorf("Open = %q, want %q", got, token)
	}
	// 鍵が違えば復号できないこと。
	other, _ := NewTokenCipher("another-key")
	if _, err := other.Open(sealed); err == nil {
		t.Error("decrypting with a different key must fail")
	}
	// 改竄を検知すること(GCMの認証)。
	sealed[len(sealed)-1] ^= 0xff
	if _, err := c.Open(sealed); err == nil {
		t.Error("tampered ciphertext must fail")
	}
}

func TestNewTokenCipherRequiresKey(t *testing.T) {
	if _, err := NewTokenCipher(""); err == nil {
		t.Error("empty key must be rejected")
	}
}

func TestNewSessionIDFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID: %v", err)
		}
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("bad uuid shape: %q", id)
		}
		if id[14] != '4' {
			t.Errorf("version nibble = %c, want 4", id[14])
		}
		if seen[id] {
			t.Fatal("duplicate session id")
		}
		seen[id] = true
	}
}
