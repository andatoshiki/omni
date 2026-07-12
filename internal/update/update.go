// Package update implements a minimal self-update mechanism for the omni binary.
// It fetches the latest release from GitHub, downloads the matching asset,
// verifies its checksum, and atomically replaces the running binary.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/andatoshiki/omni/internal/version"
)

const timeout = 5 * time.Minute

// executablePath returns the resolved path to the currently running binary.
// Override in tests to point at a temp file.
var executablePath = func() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// Run executes the update flow using the provided HTTP client.
// If client is nil, a default with timeout duration is used.
func Run(ctx context.Context, client *http.Client) error {
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	executable, err := executablePath()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}

	current := version.Version
	fmt.Printf("Current version: %s\n", current)

	release, err := fetchLatestRelease(ctx, client)
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}

	latest := release.TagName
	fmt.Printf("Latest version:  %s\n", latest)

	if !isNewer(current, latest) {
		fmt.Printf("Already up to date (%s).\n", latest)
		return nil
	}

	expectedAsset := assetName(runtime.GOOS, runtime.GOARCH, latest)
	if expectedAsset == "" {
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	checksums, err := fetchAndParseChecksums(ctx, client, release.Assets)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}

	expectedHash := checksums[expectedAsset]
	if expectedHash == "" {
		return fmt.Errorf("no checksum found for %s in checksums.txt", expectedAsset)
	}

	var assetURL string
	for _, a := range release.Assets {
		if a.Name == expectedAsset {
			assetURL = a.URL
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("asset %s not found in release %s", expectedAsset, latest)
	}

	fmt.Printf("Downloading %s...\n", expectedAsset)
	archiveData, err := downloadAsset(ctx, client, assetURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	fmt.Print("Verifying checksum... ")
	if err := verifySHA256(archiveData, expectedHash); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	fmt.Println("ok")

	fmt.Print("Extracting... ")
	tmpDir, err := os.MkdirTemp("", "omni-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	newBinary := filepath.Join(tmpDir, "omni")
	if err := extractBinary(archiveData, newBinary); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	fmt.Println("ok")

	fmt.Print("Installing... ")
	if err := replaceBinary(newBinary, executable); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	fmt.Println("ok")

	fmt.Printf("\nUpdated to %s.\n", latest)
	return nil
}

// --- asset naming ---

func assetName(goos, goarch, tag string) string {
	osName, ok := map[string]string{
		"darwin": "darwin",
		"linux":  "linux",
	}[goos]
	if !ok {
		return ""
	}
	archName, ok := map[string]string{
		"amd64": "x86-64",
		"arm64": "aarch64",
	}[goarch]
	if !ok {
		return ""
	}
	return fmt.Sprintf("omni-%s-%s-%s.tar.gz", osName, archName, tag)
}

// --- version comparison ---

// isNewer returns true if latest is a semantically newer version than current.
// Both strings may have an optional "v" prefix. If either version cannot be
// parsed, false is returned (no upgrade is safer than a wrong one).
func isNewer(current, latest string) bool {
	// dev builds are always considered older, unless comparing against dev.
	if current == "dev" {
		return latest != "dev"
	}
	cur, curOK := parseSemver(current)
	lat, latOK := parseSemver(latest)
	if !curOK || !latOK {
		return false
	}
	for i := 0; i < 3; i++ {
		if lat[i] > cur[i] {
			return true
		}
		if lat[i] < cur[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		nums[i] = n
	}
	return nums, true
}

// --- checksum verification ---

func fetchAndParseChecksums(ctx context.Context, client *http.Client, assets []githubAsset) (map[string]string, error) {
	var url string
	for _, a := range assets {
		if a.Name == "checksums.txt" {
			url = a.URL
			break
		}
	}
	if url == "" {
		return nil, errors.New("checksums.txt not found in release assets")
	}

	data, err := downloadAsset(ctx, client, url)
	if err != nil {
		return nil, err
	}
	return parseChecksums(string(data)), nil
}

// parseChecksums parses a sha256sum-format checksum file.
// Format: <hex-hash>  <filename>
func parseChecksums(content string) map[string]string {
	out := make(map[string]string)
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out[fields[1]] = fields[0]
	}
	return out
}

func verifySHA256(data []byte, expected string) error {
	h := sha256.Sum256(data)
	got := hex.EncodeToString(h[:])
	if got != expected {
		return fmt.Errorf("expected %s, got %s", expected[:16], got[:16])
	}
	return nil
}

// --- archive extraction ---

func extractBinary(data []byte, dest string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar reader: %w", err)
		}
		// Only extract the omni binary, skip everything else.
		base := filepath.Base(hdr.Name)
		if base != "omni" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("create %s: %w", dest, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return fmt.Errorf("write binary: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close binary: %w", err)
		}
		return nil
	}
	return errors.New("omni binary not found in archive")
}

// --- binary replacement ---

func replaceBinary(newPath, currentPath string) error {
	if err := os.Chmod(newPath, 0755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}

	backupPath := currentPath + ".old"

	if err := os.Rename(currentPath, backupPath); err != nil {
		// Cross-filesystem rename: copy to a temp file adjacent to the target instead.
		if !errors.Is(err, os.ErrNotExist) { // ErrNotExist shouldn't happen, but be safe
			return fmt.Errorf("backup current binary: %w", err)
		}
	}

	if err := os.Rename(newPath, currentPath); err != nil {
		// If the final rename fails (e.g. cross-filesystem), copy instead.
		if err := copyFile(newPath, currentPath); err != nil {
			// Try to restore backup.
			os.Rename(backupPath, currentPath)
			return fmt.Errorf("replace binary: %w", err)
		}
	}

	// Clean up backup.
	os.Remove(backupPath)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// PrintHelp prints usage information for the update command.
func PrintHelp() {
	l := func(url, text string) string {
		return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
	}

	fmt.Printf(`Omni — a self-hosted Telegram AI assistant with persistent memory,
model switching, and token tracking across chat sessions.

Tech: Go · Telegram Bot API · multi-provider AI · SQLite / MySQL / PostgreSQL

Usage:
  omni                  start the bot
  omni update           self-update to the latest release
  omni --help           show this overview
  omni --version        show version and build info

Built with ❤️ by %s at %s,
released under %s license.
`,
		l("https://www.toshiki.dev", "Anda Toshiki"),
		l("https://t.me/toshikidev", "Toshiki's Devpedia"),
		l("https://github.com/andatoshiki/omni/blob/master/license", "GPLv3"),
	)
}

