package fileio

import (
	"fmt"
	"os"
	"path/filepath"
)

type FilePayload struct {
	Path    string
	Content []byte
}

func WriteAtomic(files []FilePayload) error {
	tmpFiles := make([]struct {
		tmp    string
		target string
	}, 0, len(files))
	for _, file := range files {
		dir := filepath.Dir(file.Path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("ensure target dir %s: %w", dir, err)
		}

		tmp, err := os.CreateTemp(dir, ".envfuse-tmp-*")
		if err != nil {
			return fmt.Errorf("create temp file for %s: %w", file.Path, err)
		}

		tmpPath := tmp.Name()

		if _, err := tmp.Write(file.Content); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("write temp file for %s: %w", file.Path, err)
		}
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("sync temp file for %s: %w", file.Path, err)
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("close temp file for %s: %w", file.Path, err)
		}

		tmpFiles = append(tmpFiles, struct {
			tmp    string
			target string
		}{tmp: tmpPath, target: file.Path})
	}

	for i, pair := range tmpFiles {
		if err := os.Rename(pair.tmp, pair.target); err != nil {
			for j := i; j < len(tmpFiles); j++ {
				_ = os.Remove(tmpFiles[j].tmp)
			}
			return fmt.Errorf("rename temp file to %s: %w", pair.target, err)
		}
	}

	return nil
}
