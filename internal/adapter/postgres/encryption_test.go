package postgres

import (
	"bytes"
	"testing"
)

func TestRetainedResponseEncryptedAndAuthenticated(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	s := NewWithResponseKey(nil, key)
	original := []byte(`{"token":"private-agent-token"}`)
	sealed, err := s.sealResponse(original, "alice:POST/tokens:key1")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("private-agent-token")) {
		t.Fatal("token stored in plaintext")
	}
	restarted := NewWithResponseKey(nil, key)
	plain, err := restarted.openResponse(sealed, "alice:POST/tokens:key1")
	if err != nil || !bytes.Equal(plain, original) {
		t.Fatalf("replay after restart: %s %v", plain, err)
	}
	if _, err = restarted.openResponse(sealed, "bob:POST/tokens:key1"); err == nil {
		t.Fatal("accepted ciphertext in different scope")
	}
	sealed[len(sealed)/2] ^= 1
	if _, err = restarted.openResponse(sealed, "alice:POST/tokens:key1"); err == nil {
		t.Fatal("accepted tampered response")
	}
}
