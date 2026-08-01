package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSignsArtifactWithSupportedKeyFormats(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte("signed update artifact")
	digest := sha256.Sum256(artifact)

	for _, test := range []struct {
		name string
		key  []byte
	}{
		{name: "seed", key: privateKey.Seed()},
		{name: "private key", key: privateKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			keyPath := filepath.Join(dir, "update.key")
			artifactPath := filepath.Join(dir, "vision-relay.exe")
			signaturePath := artifactPath + ".sig"
			if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(test.key)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(artifactPath, artifact, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := run(keyPath, artifactPath, signaturePath, false); err != nil {
				t.Fatal(err)
			}
			encoded, err := os.ReadFile(signaturePath)
			if err != nil {
				t.Fatal(err)
			}
			signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			if !ed25519.Verify(publicKey, digest[:], signature) {
				t.Fatal("generated signature does not verify against the artifact digest")
			}
		})
	}
}

func TestReadPrivateKeyRejectsInconsistentPublicKey(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := append([]byte(nil), privateKey...)
	corrupted[len(corrupted)-1] ^= 0xff
	path := filepath.Join(t.TempDir(), "inconsistent.key")
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(corrupted)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = readPrivateKey(path)
	if err == nil || !strings.Contains(err.Error(), "inconsistent public key") {
		t.Fatalf("inconsistent key error = %v", err)
	}
}

func TestReadPrivateKeyRejectsInvalidLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.key")
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString([]byte("too short"))), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := readPrivateKey(path)
	if err == nil || !strings.Contains(err.Error(), "expected 32-byte seed or 64-byte private key") {
		t.Fatalf("invalid key error = %v", err)
	}
}
