package config

import (
	"crypto/rand"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("sk-or-v1-test-api-key-12345")
	ciphertext, nonce, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if len(ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}
	if string(ciphertext) == string(plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	got, err := enc.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(plaintext) {
		t.Fatalf("got %q, want %q", got, plaintext)
	}
}

func TestEncryptorInvalidKeyLength(t *testing.T) {
	_, err := NewEncryptor([]byte("too-short"))
	if err == nil {
		t.Fatal("expected error for invalid key length")
	}
}

func TestDecryptTamperedData(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := enc.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with ciphertext.
	ciphertext[0] ^= 0xff

	_, err = enc.Decrypt(ciphertext, nonce)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestEncryptEmptyPlaintext(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := enc.Encrypt([]byte(""))
	if err != nil {
		t.Fatal(err)
	}

	got, err := enc.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}
