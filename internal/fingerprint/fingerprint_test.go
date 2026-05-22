package fingerprint_test

import (
	"testing"

	"envfuse/internal/fingerprint"
)

// TestFingerprint_DeterministicSameInputSameHash verifies that identical logical
// env and file content always produces the same fingerprint regardless of map iteration
// order (D-07: canonical sort before hashing).
func TestFingerprint_DeterministicSameInputSameHash(t *testing.T) {
	t.Parallel()

	// Two maps with same logical content but constructed in different insertion orders.
	envA := map[string]string{
		"DB_PASSWORD": "secret",
		"APP_API_KEY": "keyval",
		"LOG_LEVEL":   "info",
	}
	envB := map[string]string{
		"LOG_LEVEL":   "info",
		"APP_API_KEY": "keyval",
		"DB_PASSWORD": "secret",
	}

	filesA := map[string][]byte{
		"/etc/app/config.conf": []byte("host=localhost\nport=5432\n"),
		"/etc/app/tls.crt":    []byte("-----BEGIN CERTIFICATE-----\n"),
	}
	filesB := map[string][]byte{
		"/etc/app/tls.crt":    []byte("-----BEGIN CERTIFICATE-----\n"),
		"/etc/app/config.conf": []byte("host=localhost\nport=5432\n"),
	}

	hashA := fingerprint.Compute(envA, filesA)
	hashB := fingerprint.Compute(envB, filesB)

	if hashA != hashB {
		t.Fatalf("expected identical fingerprint for same logical content regardless of map order; got %q vs %q", hashA, hashB)
	}
}

// TestFingerprint_DifferentEnvProducesDifferentHash verifies that changing even one
// env key-value pair results in a different fingerprint (sensitivity to effective env).
func TestFingerprint_DifferentEnvProducesDifferentHash(t *testing.T) {
	t.Parallel()

	envBefore := map[string]string{
		"APP_API_KEY": "old-key",
		"DB_PASSWORD": "secret",
	}
	envAfter := map[string]string{
		"APP_API_KEY": "new-key",
		"DB_PASSWORD": "secret",
	}
	files := map[string][]byte{
		"/etc/app/config.conf": []byte("unchanged content"),
	}

	hashBefore := fingerprint.Compute(envBefore, files)
	hashAfter := fingerprint.Compute(envAfter, files)

	if hashBefore == hashAfter {
		t.Fatalf("expected different fingerprints for different env; got %q for both", hashBefore)
	}
}

// TestFingerprint_DifferentFileBytesProducesDifferentHash verifies that changing
// rendered file content results in a different fingerprint (D-05: file bytes included).
func TestFingerprint_DifferentFileBytesProducesDifferentHash(t *testing.T) {
	t.Parallel()

	env := map[string]string{"APP_API_KEY": "stable"}

	filesBefore := map[string][]byte{
		"/etc/app/config.conf": []byte("version=1"),
	}
	filesAfter := map[string][]byte{
		"/etc/app/config.conf": []byte("version=2"),
	}

	hashBefore := fingerprint.Compute(env, filesBefore)
	hashAfter := fingerprint.Compute(env, filesAfter)

	if hashBefore == hashAfter {
		t.Fatalf("expected different fingerprints for different file bytes; got %q for both", hashBefore)
	}
}

// TestFingerprint_EmptyInputsStable verifies that two empty inputs produce the same
// fingerprint (degenerate determinism case).
func TestFingerprint_EmptyInputsStable(t *testing.T) {
	t.Parallel()

	hash1 := fingerprint.Compute(map[string]string{}, map[string][]byte{})
	hash2 := fingerprint.Compute(map[string]string{}, map[string][]byte{})

	if hash1 != hash2 {
		t.Fatalf("expected identical fingerprint for empty inputs; got %q vs %q", hash1, hash2)
	}
}

// TestFingerprint_MetadataNotIncluded verifies that runtime metadata fields that
// are NOT part of the candidate env/files do not affect the fingerprint (D-08).
// This is structural: the Compute function only accepts env+files, so timestamps
// and latency cannot be passed in — we verify the signature enforces this.
func TestFingerprint_MetadataNotIncluded(t *testing.T) {
	t.Parallel()

	env := map[string]string{"KEY": "value"}
	files := map[string][]byte{"/path": []byte("content")}

	// Compute twice with the same payload. If timestamps were included internally,
	// calls at different instants would differ; they must not.
	hash1 := fingerprint.Compute(env, files)
	hash2 := fingerprint.Compute(env, files)

	if hash1 != hash2 {
		t.Fatalf("fingerprint must be stable across repeated calls (no embedded metadata); got %q vs %q", hash1, hash2)
	}
}

// TestFingerprint_OutputIsNonEmpty verifies that Compute returns a non-empty string
// (basic sanity check that SHA-256 output is returned).
func TestFingerprint_OutputIsNonEmpty(t *testing.T) {
	t.Parallel()

	h := fingerprint.Compute(
		map[string]string{"KEY": "value"},
		map[string][]byte{"/path": []byte("content")},
	)

	if h == "" {
		t.Fatal("expected non-empty fingerprint hash, got empty string")
	}
}
