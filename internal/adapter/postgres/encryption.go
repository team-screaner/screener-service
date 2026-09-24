package postgres

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// NewWithResponseKey uses a persistent 32-byte deployment secret, shared by replicas.
// Invalid length is a startup programming/configuration error.
func NewWithResponseKey(db *sql.DB, key []byte) *Store {
	if len(key) != 32 {
		panic("response encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &Store{db: db, responses: aead}
}
func (s *Store) sealResponse(plain []byte, binding string) ([]byte, error) {
	nonce := make([]byte, s.responses.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	encrypted := s.responses.Seal(nonce, nonce, plain, []byte(binding))
	return json.Marshal(map[string]string{"encrypted": base64.RawStdEncoding.EncodeToString(encrypted)})
}
func (s *Store) openResponse(data []byte, binding string) ([]byte, error) {
	var envelope struct {
		Encrypted string `json:"encrypted"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	b, err := base64.RawStdEncoding.DecodeString(envelope.Encrypted)
	if err != nil {
		return nil, err
	}
	n := s.responses.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid retained response")
	}
	return s.responses.Open(nil, b[:n], b[n:], []byte(binding))
}
