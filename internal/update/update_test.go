package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- assetName tests ---

func TestAssetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos, goarch, tag, want string
	}{
		{"darwin", "amd64", "v1.2.3", "omni-darwin-x86-64-v1.2.3.tar.gz"},
		{"darwin", "arm64", "v1.2.3", "omni-darwin-aarch64-v1.2.3.tar.gz"},
		{"linux", "amd64", "v1.2.3", "omni-linux-x86-64-v1.2.3.tar.gz"},
		{"linux", "arm64", "v1.2.3", "omni-linux-aarch64-v1.2.3.tar.gz"},
		// unsupported — no Windows (ZIP extraction not implemented)
		{"windows", "amd64", "v1.2.3", ""},
		{"windows", "arm64", "v1.2.3", ""},
		{"freebsd", "amd64", "v1.2.3", ""},
		{"linux", "riscv64", "v1.2.3", ""},
	}

	for _, tt := range tests {
		got := assetName(tt.goos, tt.goarch, tt.tag)
		if got != tt.want {
			t.Errorf("assetName(%s, %s, %s) = %q, want %q", tt.goos, tt.goarch, tt.tag, got, tt.want)
		}
	}
}

// --- version comparison tests ---

func TestIsNewer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.10", "v1.2.9", false},
		{"v2.0.0", "v1.9.9", false},
		{"dev", "v1.0.0", true},
		{"dev", "dev", false},
		{"v1.2.3", "v1.2.10", true},
		{"v1.9.0", "v1.10.0", true},
		{"v3.5.9", "v3.5.10", true},
		// no v prefix
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "2.0.0", true},
		// malformed versions — not newer
		{"garbage", "v1.0.0", false},
		{"v1.0.0", "garbage", false},
		{"v1.0", "v1.0.1", false},
	}

	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"v1.2.3", [3]int{1, 2, 3}, true},
		{"v0.0.0", [3]int{0, 0, 0}, true},
		{"v10.20.30", [3]int{10, 20, 30}, true},
		{"1.2.3", [3]int{1, 2, 3}, true},
		{"v1.2.3-rc1", [3]int{0, 0, 0}, false},
		{"garbage", [3]int{0, 0, 0}, false},
		{"v1.2", [3]int{0, 0, 0}, false},
	}

	for _, tt := range tests {
		nums, ok := parseSemver(tt.in)
		if ok != tt.ok {
			t.Errorf("parseSemver(%q) ok = %v, want %v", tt.in, ok, tt.ok)
		}
		if ok && nums != tt.want {
			t.Errorf("parseSemver(%q) = %v, want %v", tt.in, nums, tt.want)
		}
	}
}

// --- checksum tests ---

func TestParseChecksums(t *testing.T) {
	t.Parallel()

	input := `abc123def4567890123456789012345678901234  omni-darwin-aarch64-v1.2.3.tar.gz
def456abc7890123456789012345678901234567  omni-linux-x86-64-v1.2.3.tar.gz
`
	got := parseChecksums(input)
	if got["omni-darwin-aarch64-v1.2.3.tar.gz"] != "abc123def4567890123456789012345678901234" {
		t.Errorf("wrong hash for darwin asset: %q", got["omni-darwin-aarch64-v1.2.3.tar.gz"])
	}
	if got["omni-linux-x86-64-v1.2.3.tar.gz"] != "def456abc7890123456789012345678901234567" {
		t.Errorf("wrong hash for linux asset: %q", got["omni-linux-x86-64-v1.2.3.tar.gz"])
	}
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
}

func TestParseChecksumsEmpty(t *testing.T) {
	t.Parallel()

	got := parseChecksums("")
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestVerifySHA256(t *testing.T) {
	t.Parallel()

	data := []byte("hello, omni")
	h := sha256.Sum256(data)
	expected := hex.EncodeToString(h[:])

	if err := verifySHA256(data, expected); err != nil {
		t.Errorf("verifySHA256 should pass: %v", err)
	}

	if err := verifySHA256(data, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("verifySHA256 should fail for wrong hash")
	}
}

// --- extraction tests ---

func TestExtractBinary(t *testing.T) {
	// Create a tar.gz containing a fake "omni" binary.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("fake omni binary content")
	hdr := &tar.Header{
		Name:     "omni",
		Size:     int64(len(content)),
		Mode:     0755,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	// Also include some extra files like the real archive does.
	readmeHdr := &tar.Header{
		Name:     "readme.md",
		Size:     6,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(readmeHdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("# Omni")); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "omni")
	if err := extractBinary(buf.Bytes(), dest); err != nil {
		t.Fatalf("extractBinary: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}

	// Check executable permissions.
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0111 == 0 {
		t.Error("extracted binary is not executable")
	}
}

func TestExtractBinaryNotFound(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:     "readme.md",
		Size:     6,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("# Omni")); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "omni")
	err := extractBinary(buf.Bytes(), dest)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// --- binary replacement tests ---

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()

	// Create a "current" binary.
	currentPath := filepath.Join(dir, "omni")
	if err := os.WriteFile(currentPath, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a "new" binary.
	newPath := filepath.Join(dir, "omni-new")
	if err := os.WriteFile(newPath, []byte("new binary"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(newPath, currentPath); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}

	// Verify the current path has the new content.
	got, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Errorf("after replace: %q, want %q", got, "new binary")
	}

	// Verify the .old backup was removed.
	if _, err := os.Stat(currentPath + ".old"); !os.IsNotExist(err) {
		t.Error(".old backup should have been removed")
	}
}

// --- GitHub API tests ---

func TestFetchLatestReleaseJSONParsing(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/andatoshiki/omni/releases/latest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rel := githubRelease{
			TagName: "v1.2.3",
			Assets: []githubAsset{
				{Name: "omni-darwin-aarch64-v1.2.3.tar.gz", URL: srv.URL + "/download/asset.tar.gz", Size: 1024},
				{Name: "checksums.txt", URL: srv.URL + "/download/checksums.txt", Size: 256},
			},
		}
		json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	// Make apiBase configurable for the test.
	origBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = origBase }()

	client := srv.Client()
	rel, err := fetchLatestRelease(context.Background(), client)
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("TagName = %q, want %q", rel.TagName, "v1.2.3")
	}
	if len(rel.Assets) != 2 {
		t.Errorf("got %d assets, want 2", len(rel.Assets))
	}
	if rel.Assets[0].Name != "omni-darwin-aarch64-v1.2.3.tar.gz" {
		t.Errorf("asset[0].Name = %q", rel.Assets[0].Name)
	}
}

func TestDownloadAsset(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download/asset.tar.gz" {
			w.Write([]byte("fake binary data"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	data, err := downloadAsset(context.Background(), client, srv.URL+"/download/asset.tar.gz")
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	if string(data) != "fake binary data" {
		t.Errorf("downloaded data = %q, want %q", data, "fake binary data")
	}
}

func TestDownloadAssetError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	_, err := downloadAsset(context.Background(), client, srv.URL+"/nonexistent")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// --- full integration test with real temp dir ---

func TestRunEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode")
	}

	dir := t.TempDir()

	// Create a fake "current" binary.
	currentPath := filepath.Join(dir, "omni")
	if err := os.WriteFile(currentPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	// Build a fake release with a tar.gz asset containing a "new" fake binary.
	var archiveBuf bytes.Buffer
	gw := gzip.NewWriter(&archiveBuf)
	tw := tar.NewWriter(gw)

	newContent := []byte("new fake binary")
	hdr := &tar.Header{
		Name:     "omni",
		Size:     int64(len(newContent)),
		Mode:     0755,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(newContent); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	archiveData := archiveBuf.Bytes()

	// Compute checksums.
	expAsset := assetName(runtime.GOOS, runtime.GOARCH, "v99.0.0")
	if expAsset == "" {
		t.Skip("unsupported platform for end-to-end test")
	}
	archiveHash := sha256.Sum256(archiveData)
	checksumsContent := fmt.Sprintf("%s  %s\n",
		hex.EncodeToString(archiveHash[:]), expAsset)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/andatoshiki/omni/releases/latest":
			json.NewEncoder(w).Encode(githubRelease{
				TagName: "v99.0.0",
				Assets: []githubAsset{
					{Name: expAsset, URL: fmt.Sprintf("%s/asset", srv.URL), Size: int64(len(archiveData))},
					{Name: "checksums.txt", URL: fmt.Sprintf("%s/checksums", srv.URL), Size: int64(len(checksumsContent))},
				},
			})
		case "/asset":
			w.Write(archiveData)
		case "/checksums":
			w.Write([]byte(checksumsContent))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Override apiBase and executablePath for end-to-end test.
	origBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = origBase }()

	origExecPath := executablePath
	executablePath = func() (string, error) { return currentPath, nil }
	defer func() { executablePath = origExecPath }()

	client := srv.Client()
	err := Run(context.Background(), client)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify that the "current binary" has been replaced.
	got, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("read current binary after update: %v", err)
	}
	if string(got) != string(newContent) {
		t.Errorf("binary content after update = %q, want %q", got, newContent)
	}
}

// --- resolveExecutable test ---

func TestExecutablePath(t *testing.T) {
	t.Parallel()

	p, err := executablePath()
	if err != nil {
		t.Fatalf("executablePath: %v", err)
	}
	if p == "" {
		t.Error("executable path should not be empty")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("executable should exist at %s: %v", p, err)
	}
}

// --- apiBase override test ---

func TestDownloadTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("too late"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	_, err := downloadAsset(ctx, client, srv.URL)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
