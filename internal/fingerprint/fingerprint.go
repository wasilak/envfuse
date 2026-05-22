// Package fingerprint computes a deterministic SHA-256 digest over the effective
// env payload and rendered file bytes of a sync cycle candidate.
//
// Design decisions enforced here:
//   - D-05: fingerprint input includes both effective env payload and rendered file bytes only.
//   - D-06: fingerprint is computed from the pre-commit candidate state.
//   - D-07: env keys and output paths are canonically sorted before hashing.
//   - D-08: runtime metadata (timestamps, latency, fetch order) is excluded.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Compute returns a deterministic hex-encoded SHA-256 digest over the provided
// candidate env payload and rendered file bytes.
//
// The digest is stable across map iteration orders because env keys and file
// paths are sorted before contributing to the hash (D-07). Only the effective
// env payload and rendered file bytes are included (D-05). Runtime metadata such
// as timestamps, latency, or fetch order must not be passed in (D-08).
func Compute(env map[string]string, files map[string][]byte) string {
	h := sha256.New()

	// Hash env: sort keys for deterministic order (D-07).
	envKeys := make([]string, 0, len(env))
	for k := range env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)

	for _, k := range envKeys {
		// Separate key and value with a null byte to prevent ambiguous encoding
		// (e.g., "AB"+"C=D" vs "A"+"BC=D"). Use a record separator between entries.
		fmt.Fprintf(h, "env\x00%s\x00%s\x01", k, env[k])
	}

	// Hash files: sort paths for deterministic order (D-07).
	filePaths := make([]string, 0, len(files))
	for p := range files {
		filePaths = append(filePaths, p)
	}
	sort.Strings(filePaths)

	for _, p := range filePaths {
		// Write path as header then raw bytes as body.
		fmt.Fprintf(h, "file\x00%s\x00", p)
		h.Write(files[p])
		h.Write([]byte{0x01}) // record separator
	}

	return hex.EncodeToString(h.Sum(nil))
}
