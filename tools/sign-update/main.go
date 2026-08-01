package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	keyPath := flag.String("key", "", "path to a base64-encoded Ed25519 seed or private key")
	inputPath := flag.String("input", "", "release artifact to sign")
	outputPath := flag.String("output", "", "signature sidecar to create")
	printPublicKey := flag.Bool("print-public-key", false, "print the base64-encoded public key")
	flag.Parse()
	if err := run(*keyPath, *inputPath, *outputPath, *printPublicKey); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(keyPath, inputPath, outputPath string, printPublicKey bool) error {
	privateKey, err := readPrivateKey(keyPath)
	if err != nil {
		return err
	}
	if printPublicKey {
		publicKey := privateKey.Public().(ed25519.PublicKey)
		fmt.Println(base64.StdEncoding.EncodeToString(publicKey))
	}
	if inputPath == "" && outputPath == "" {
		if printPublicKey {
			return nil
		}
		return errors.New("-input and -output are required when not printing the public key")
	}
	if inputPath == "" || outputPath == "" {
		return errors.New("-input and -output must be provided together")
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open update artifact: %w", err)
	}
	defer input.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return fmt.Errorf("hash update artifact: %w", err)
	}
	signature := ed25519.Sign(privateKey, hash.Sum(nil))
	encoded := base64.StdEncoding.EncodeToString(signature) + "\n"
	if err := os.WriteFile(outputPath, []byte(encoded), 0o644); err != nil {
		return fmt.Errorf("write update signature: %w", err)
	}
	return nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("-key is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	encoded := strings.TrimSpace(string(data))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, errors.New("signing key is not valid base64")
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	case ed25519.PrivateKeySize:
		privateKey := ed25519.NewKeyFromSeed(decoded[:ed25519.SeedSize])
		derived := privateKey.Public().(ed25519.PublicKey)
		if !derived.Equal(ed25519.PublicKey(decoded[ed25519.SeedSize:])) {
			return nil, errors.New("Ed25519 private key contains an inconsistent public key")
		}
		return privateKey, nil
	default:
		return nil, fmt.Errorf("Ed25519 signing key is %d bytes; expected 32-byte seed or 64-byte private key", len(decoded))
	}
}
