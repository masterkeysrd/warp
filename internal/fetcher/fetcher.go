package fetcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// GlobalCacheDir returns the absolute path to the WARP global cache directory.
func GlobalCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".warp", "pkg", "mod"), nil
}

// Fetch retrieves a plugin repository and stores it in the global cache.
// It returns the absolute path to the cached directory.
func Fetch(source, version string) (string, error) {
	// If it starts with file://, strip it
	if strings.HasPrefix(source, "file://") {
		source = strings.TrimPrefix(source, "file://")
	}

	// If it's a local absolute or relative path, resolve and return it directly without caching
	if filepath.IsAbs(source) || strings.HasPrefix(source, ".") {
		absPath, err := filepath.Abs(source)
		if err != nil {
			return "", fmt.Errorf("failed to resolve local path: %w", err)
		}
		if stat, err := os.Stat(absPath); err == nil && stat.IsDir() {
			return absPath, nil
		}
	}

	cacheDir, err := GlobalCacheDir()
	if err != nil {
		return "", err
	}

	// Strip http:// or https:// or git:// for the cache directory name
	safeSource := source
	if strings.HasPrefix(safeSource, "http://") {
		safeSource = strings.TrimPrefix(safeSource, "http://")
	} else if strings.HasPrefix(safeSource, "https://") {
		safeSource = strings.TrimPrefix(safeSource, "https://")
	} else if strings.HasPrefix(safeSource, "git://") {
		safeSource = strings.TrimPrefix(safeSource, "git://")
	}

	// Format: ~/.warp/pkg/mod/github.com/org/repo@v1.2.0
	targetDir := filepath.Join(cacheDir, fmt.Sprintf("%s@%s", safeSource, version))

	// If the directory already exists, assume it's cached.
	// In a production system, we'd also verify the hash here against warp.lock.
	if _, err := os.Stat(targetDir); err == nil {
		return targetDir, nil
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	cloneURL := source
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "file://") && !strings.HasPrefix(source, "git://") {
		cloneURL = "https://" + source
	}

	if version == "latest" {
		// Clone default branch (usually main/master)
		_, err = git.PlainClone(targetDir, false, &git.CloneOptions{
			URL:      cloneURL,
			Progress: os.Stdout,
		})
	} else {
		// Clone specific tag/branch
		_, err = git.PlainClone(targetDir, false, &git.CloneOptions{
			URL:           cloneURL,
			ReferenceName: plumbing.NewTagReferenceName(version),
			SingleBranch:  true,
			Depth:         1,
			Progress:      os.Stdout,
		})
	}

	if err != nil {
		// Cleanup partial clone on failure
		os.RemoveAll(targetDir)
		return "", fmt.Errorf("failed to clone repository %s: %w", cloneURL, err)
	}

	return targetDir, nil
}
