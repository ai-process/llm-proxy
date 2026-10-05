package crypto

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func key(b byte) string {
	raw := make([]byte, keySize)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestParseKeyringActiveIsHighestVersion(t *testing.T) {
	kr, err := ParseKeyring("2:" + key(2) + ",1:" + key(1))
	if err != nil {
		t.Fatalf("ParseKeyring: %v", err)
	}
	if kr.ActiveVersion() != 2 {
		t.Fatalf("active = %d, want 2", kr.ActiveVersion())
	}
	if len(kr.Versions()) != 2 {
		t.Fatalf("versions = %v, want 2 entries", kr.Versions())
	}
}

func TestParseKeyringRejects(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too-short"))
	cases := map[string]string{
		"empty":            "",
		"no colon":         "1" + key(1),
		"zero version":     "0:" + key(1),
		"negative version": "-1:" + key(1),
		"bad base64":       "1:!!!not-base64!!!",
		"wrong key size":   "1:" + short,
		"duplicate":        "1:" + key(1) + ",1:" + key(2),
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseKeyring(spec); err == nil {
				t.Fatalf("ParseKeyring(%q) = nil error, want failure", spec)
			}
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	kr, err := ParseKeyring("1:" + key(7))
	if err != nil {
		t.Fatalf("ParseKeyring: %v", err)
	}
	const token = "8136363993:AAEnXXtchKN-not-a-real-token"

	blob, version, err := kr.Encrypt(token)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
	if strings.Contains(string(blob), token) {
		t.Fatal("ciphertext contains the plaintext")
	}

	got, err := kr.Decrypt(blob, version)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != token {
		t.Fatalf("Decrypt = %q, want %q", got, token)
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	kr, _ := ParseKeyring("1:" + key(9))
	a, _, err := kr.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	b, _, err := kr.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(a) == string(b) {
		t.Fatal("two encryptions of the same plaintext are identical — nonce reused")
	}
}

func TestDecryptOldVersionAfterRotation(t *testing.T) {
	old, _ := ParseKeyring("1:" + key(1))
	blob, version, err := old.Encrypt("legacy-token")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	rotated, err := ParseKeyring("1:" + key(1) + ",2:" + key(2))
	if err != nil {
		t.Fatalf("ParseKeyring: %v", err)
	}
	if rotated.ActiveVersion() != 2 {
		t.Fatalf("active = %d, want 2", rotated.ActiveVersion())
	}
	got, err := rotated.Decrypt(blob, version)
	if err != nil {
		t.Fatalf("Decrypt v1 blob with rotated keyring: %v", err)
	}
	if got != "legacy-token" {
		t.Fatalf("Decrypt = %q", got)
	}
}

func TestDecryptUnknownVersion(t *testing.T) {
	kr, _ := ParseKeyring("1:" + key(1))
	blob, _, _ := kr.Encrypt("x")
	if _, err := kr.Decrypt(blob, 99); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("err = %v, want ErrUnknownVersion", err)
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	kr, _ := ParseKeyring("1:" + key(3))
	blob, version, _ := kr.Encrypt("token")

	tampered := make([]byte, len(blob))
	copy(tampered, blob)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := kr.Decrypt(tampered, version); err == nil {
		t.Fatal("Decrypt accepted a tampered ciphertext")
	}

	if _, err := kr.Decrypt(blob[:4], version); err == nil {
		t.Fatal("Decrypt accepted a truncated ciphertext")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	a, _ := ParseKeyring("1:" + key(1))
	b, _ := ParseKeyring("1:" + key(2))
	blob, version, _ := a.Encrypt("token")
	if _, err := b.Decrypt(blob, version); err == nil {
		t.Fatal("Decrypt succeeded with the wrong key")
	}
}
