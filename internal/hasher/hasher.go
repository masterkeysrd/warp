package hasher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// DirHash computes a stable SHA-256 hash of a directory tree.
// It hashes file contents and their relative paths, ensuring that identical
// directory structures always produce the same hash.
func DirHash(dirPath string) (string, error) {
	h := sha256.New()

	var paths []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip hidden directories like .git, but allow .agents
			if info.Name() != "." && info.Name() != ".agents" && info.Name()[0] == '.' {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip hidden files, but allow files inside .agents (which is handled by the check above)
		if info.Name() != ".agents" && info.Name()[0] == '.' {
			return nil
		}

		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		paths = append(paths, relPath)
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %w", err)
	}

	// Sort paths to ensure deterministic hashing
	sort.Strings(paths)

	for _, relPath := range paths {
		absPath := filepath.Join(dirPath, relPath)
		f, err := os.Open(absPath)
		if err != nil {
			return "", err
		}

		// Write the relative path to the hash
		h.Write([]byte(relPath))

		// Write the file content to the hash
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
	}

	return "h1:" + hex.EncodeToString(h.Sum(nil)), nil
}

// FileHash computes a stable SHA-256 hash of a single file.
func FileHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return "h1:" + hex.EncodeToString(h.Sum(nil)), nil
}
