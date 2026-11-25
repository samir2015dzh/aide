// Package acpregistry fetches, caches, verifies, and installs agents from the
// official Agent Client Protocol Registry (agentclientprotocol/registry, Apache-2.0).
//
// Binary archive installs verify SHA-256 digests and extract into machine-isolated
// subdirectories under <dataDir>/acp-registry/installed/<agent-name>/<version>/.
// Dedicated built-in providers such as antigravity are explicitly protected from
// being reinstalled through the generic registry path (工单 #110 / #111).
package acpregistry

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// DefaultRegistryURL is the official ACP Registry CDN index (Apache-2.0).
const DefaultRegistryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

const (
	defaultCacheTTL   = 24 * time.Hour
	maxIndexBytes     = 8 << 20   // 8 MiB
	maxArchiveBytes   = 256 << 20 // 256 MiB
	maxExtractedBytes = 512 << 20 // 512 MiB
	maxExtractedFiles = 4096
)

//go:embed snapshot.json
var embeddedSnapshot []byte

// Index matches the official agentclientprotocol/registry JSON schema.
type Index struct {
	Version string  `json:"version"`
	Agents  []Entry `json:"agents"`
}

// Entry is one agent manifest in the official ACP Registry.
type Entry struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Description  string       `json:"description,omitempty"`
	Repository   string       `json:"repository,omitempty"`
	License      string       `json:"license,omitempty"`
	Authors      []string     `json:"authors,omitempty"`
	Icon         string       `json:"icon,omitempty"`
	Distribution Distribution `json:"distribution"`
}

// Distribution describes how an ACP registry agent is distributed.
type Distribution struct {
	Binary map[string]BinaryTarget `json:"binary,omitempty"`
	NPX    *PackageTarget          `json:"npx,omitempty"`
	UVX    *PackageTarget          `json:"uvx,omitempty"`
}

// BinaryTarget is a platform-specific binary archive distribution.
type BinaryTarget struct {
	Archive string            `json:"archive"`
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	SHA256  string            `json:"sha256,omitempty"`
}

// PackageTarget is a package-runner (npx / uvx) distribution.
type PackageTarget struct {
	Package string            `json:"package"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ListResult holds the loaded registry index and sync provenance.
type ListResult struct {
	Entries   []Entry
	Source    string // "remote", "cache", or "embedded"
	SyncedAt  time.Time
	LastError string
	Platform  string
}

// InstallResult describes a freshly installed registry agent ready to be saved
// into the machine's custom ACP agents config.
type InstallResult struct {
	Name            string
	RegistryID      string
	RegistryVersion string
	Distribution    string // "binary", "npx", or "uvx"
	Command         string
	Args            []string
	Env             map[string]string
	Dir             string
	InstallDir      string
	SHA256          string
}

// Options configures a Registry instance (zero values use production defaults).
type Options struct {
	DataDir    string
	URL        string
	HTTPClient *http.Client
	CacheTTL   time.Duration
	Platform   string // e.g. "windows-x86_64"; defaults to CurrentPlatform()
	PathEnv    string
}

// Registry manages loading, caching, and installing official ACP Registry agents.
type Registry struct {
	dataDir  string
	url      string
	client   *http.Client
	ttl      time.Duration
	platform string
	pathEnv  string

	mu        sync.Mutex
	cached    *Index
	source    string
	syncedAt  time.Time
	lastError string
}

type cacheEnvelope struct {
	SyncedAt time.Time `json:"synced_at"`
	Index    Index     `json:"index"`
}

// New creates a Registry rooted at opts.DataDir.
func New(opts Options) *Registry {
	u := strings.TrimSpace(opts.URL)
	if u == "" {
		u = strings.TrimSpace(os.Getenv("AIDE_ACP_REGISTRY_URL"))
	}
	if u == "" {
		u = DefaultRegistryURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	plat := opts.Platform
	if plat == "" {
		plat = CurrentPlatform()
	}
	return &Registry{
		dataDir:  opts.DataDir,
		url:      u,
		client:   client,
		ttl:      ttl,
		platform: plat,
		pathEnv:  opts.PathEnv,
	}
}

// CurrentPlatform returns the ACP Registry target key ("os-arch") for this machine.
func CurrentPlatform() string {
	return PlatformFor(runtime.GOOS, runtime.GOARCH)
}

// PlatformFor maps Go GOOS/GOARCH pairs to official ACP Registry target keys.
func PlatformFor(goos, goarch string) string {
	arch := goarch
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	return goos + "-" + arch
}

// Platform returns this registry instance's target platform key.
func (r *Registry) Platform() string { return r.platform }

func (r *Registry) cachePath() string {
	if r.dataDir == "" {
		return ""
	}
	return filepath.Join(r.dataDir, "acp-registry", "index-cache.json")
}

func (r *Registry) installedRoot() string {
	return filepath.Join(r.dataDir, "acp-registry", "installed")
}

// EmbeddedIndex parses the bundled official registry snapshot.
func EmbeddedIndex() (Index, error) {
	var idx Index
	if err := json.Unmarshal(embeddedSnapshot, &idx); err != nil {
		return Index{}, fmt.Errorf("parse embedded acp registry snapshot: %w", err)
	}
	return idx, nil
}

// List returns the registry catalog, using memory/disk cache when fresh and
// transparently falling back to cache or the embedded snapshot on network failure.
func (r *Registry) List(ctx context.Context, forceRefresh bool) ListResult {
	r.mu.Lock()
	if !forceRefresh && r.cached != nil && time.Since(r.syncedAt) < r.ttl {
		res := ListResult{
			Entries:   slices.Clone(r.cached.Agents),
			Source:    r.source,
			SyncedAt:  r.syncedAt,
			LastError: r.lastError,
			Platform:  r.platform,
		}
		r.mu.Unlock()
		return res
	}
	r.mu.Unlock()

	// Try disk cache before hitting network when not forced.
	var disk *cacheEnvelope
	if env, err := r.readDiskCache(); err == nil && len(env.Index.Agents) > 0 {
		disk = &env
		if !forceRefresh && time.Since(env.SyncedAt) < r.ttl {
			r.mu.Lock()
			r.cached = &env.Index
			r.source = "cache"
			r.syncedAt = env.SyncedAt
			r.lastError = ""
			r.mu.Unlock()
			return ListResult{
				Entries:  slices.Clone(env.Index.Agents),
				Source:   "cache",
				SyncedAt: env.SyncedAt,
				Platform: r.platform,
			}
		}
	}

	// Attempt remote refresh.
	if idx, err := r.fetchRemote(ctx); err == nil && len(idx.Agents) > 0 {
		now := time.Now().UTC()
		r.writeDiskCache(cacheEnvelope{SyncedAt: now, Index: idx})
		r.mu.Lock()
		r.cached = &idx
		r.source = "remote"
		r.syncedAt = now
		r.lastError = ""
		r.mu.Unlock()
		return ListResult{
			Entries:  slices.Clone(idx.Agents),
			Source:   "remote",
			SyncedAt: now,
			Platform: r.platform,
		}
	} else if err != nil {
		// Fallback gracefully: stale disk cache first, then embedded snapshot.
		if disk != nil {
			r.mu.Lock()
			r.cached = &disk.Index
			r.source = "cache"
			r.syncedAt = disk.SyncedAt
			r.lastError = err.Error()
			r.mu.Unlock()
			return ListResult{
				Entries:   slices.Clone(disk.Index.Agents),
				Source:    "cache",
				SyncedAt:  disk.SyncedAt,
				LastError: err.Error(),
				Platform:  r.platform,
			}
		}
		emb, _ := EmbeddedIndex()
		r.mu.Lock()
		r.cached = &emb
		r.source = "embedded"
		r.syncedAt = time.Time{}
		r.lastError = err.Error()
		r.mu.Unlock()
		return ListResult{
			Entries:   slices.Clone(emb.Agents),
			Source:    "embedded",
			LastError: err.Error(),
			Platform:  r.platform,
		}
	}

	emb, _ := EmbeddedIndex()
	return ListResult{
		Entries:  slices.Clone(emb.Agents),
		Source:   "embedded",
		Platform: r.platform,
	}
}

func (r *Registry) fetchRemote(ctx context.Context) (Index, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return Index{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aide-acp-registry/1.0")
	resp, err := r.client.Do(req)
	if err != nil {
		return Index{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Index{}, fmt.Errorf("registry HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes))
	if err != nil {
		return Index{}, err
	}
	var idx Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return Index{}, fmt.Errorf("decode registry index: %w", err)
	}
	if len(idx.Agents) == 0 {
		return Index{}, errors.New("registry index contains no agents")
	}
	return idx, nil
}

func (r *Registry) readDiskCache() (cacheEnvelope, error) {
	p := r.cachePath()
	if p == "" {
		return cacheEnvelope{}, os.ErrNotExist
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cacheEnvelope{}, err
	}
	var env cacheEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return cacheEnvelope{}, err
	}
	return env, nil
}

func (r *Registry) writeDiskCache(env cacheEnvelope) {
	p := r.cachePath()
	if p == "" {
		return
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, p)
	}
}

// BestDistribution chooses the preferred installable distribution for platform:
// binary (platform-matched) > npx > uvx.
func BestDistribution(e Entry, platform string) (kind string, sha256Declared bool, reason string) {
	if strings.EqualFold(e.ID, "antigravity") {
		return "", false, "antigravity is a dedicated built-in provider in Aide and is not installed via the generic ACP registry"
	}
	if t, ok := e.Distribution.Binary[platform]; ok && strings.TrimSpace(t.Archive) != "" && strings.TrimSpace(t.Cmd) != "" {
		return "binary", strings.TrimSpace(t.SHA256) != "", ""
	}
	if e.Distribution.NPX != nil && strings.TrimSpace(e.Distribution.NPX.Package) != "" {
		return "npx", false, ""
	}
	if e.Distribution.UVX != nil && strings.TrimSpace(e.Distribution.UVX.Package) != "" {
		return "uvx", false, ""
	}
	return "", false, fmt.Sprintf("no distribution available for platform %s", platform)
}

var nonSlugRE = regexp.MustCompile(`[^a-z0-9-]+`)

// NormalizeAgentName turns a registry id into a valid Aide custom agent name
// (`^[a-z][a-z0-9-]{0,31}$`). If reserved returns true, a "-acp" suffix is appended.
func NormalizeAgentName(registryID string, reserved func(string) bool) string {
	s := strings.ToLower(strings.TrimSpace(registryID))
	s = strings.ReplaceAll(s, "_", "-")
	s = nonSlugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		s = "acp-" + s
	}
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	if reserved != nil && reserved(s) {
		cand := s
		if !strings.HasSuffix(cand, "-acp") {
			if len(cand) > 28 {
				cand = strings.TrimRight(cand[:28], "-")
			}
			cand += "-acp"
		}
		if !reserved(cand) {
			return cand
		}
		for i := 2; i <= 99; i++ {
			suffix := fmt.Sprintf("-%d", i)
			base := s
			if len(base)+len(suffix) > 32 {
				base = strings.TrimRight(base[:32-len(suffix)], "-")
			}
			if !reserved(base + suffix) {
				return base + suffix
			}
		}
	}
	return s
}

// Install downloads (verifying SHA-256 for binary archives) and prepares an
// official ACP Registry agent in a machine-isolated directory.
func (r *Registry) Install(ctx context.Context, registryID, desiredName string, reserved func(string) bool) (InstallResult, error) {
	registryID = strings.TrimSpace(registryID)
	if registryID == "" {
		return InstallResult{}, errors.New("registry_id is required")
	}
	if strings.EqualFold(registryID, "antigravity") {
		return InstallResult{}, errors.New("antigravity is a dedicated built-in provider in Aide and must not be installed via the generic ACP registry")
	}

	listed := r.List(ctx, false)
	var entry *Entry
	for i := range listed.Entries {
		if listed.Entries[i].ID == registryID {
			entry = &listed.Entries[i]
			break
		}
	}
	if entry == nil {
		// One fresh retry in case the registry was updated upstream.
		listed = r.List(ctx, true)
		for i := range listed.Entries {
			if listed.Entries[i].ID == registryID {
				entry = &listed.Entries[i]
				break
			}
		}
	}
	if entry == nil {
		return InstallResult{}, fmt.Errorf("ACP registry agent %q not found", registryID)
	}

	kind, _, reason := BestDistribution(*entry, r.platform)
	if kind == "" {
		return InstallResult{}, errors.New(reason)
	}

	name := strings.TrimSpace(desiredName)
	if name == "" {
		name = NormalizeAgentName(entry.ID, reserved)
	}

	switch kind {
	case "binary":
		return r.installBinary(ctx, *entry, entry.Distribution.Binary[r.platform], name)
	case "npx":
		return r.installRunner(*entry, "npx", entry.Distribution.NPX, name)
	case "uvx":
		return r.installRunner(*entry, "uvx", entry.Distribution.UVX, name)
	default:
		return InstallResult{}, fmt.Errorf("unsupported distribution %q", kind)
	}
}

func (r *Registry) installRunner(entry Entry, runner string, target *PackageTarget, name string) (InstallResult, error) {
	bin := provider.LookPath(runner, provider.SearchPath(r.pathEnv))
	if bin == "" {
		return InstallResult{}, fmt.Errorf("%s is required to install %s (%s) but was not found on PATH", runner, entry.Name, target.Package)
	}
	var args []string
	if runner == "npx" {
		args = append(args, "-y", target.Package)
	} else {
		args = append(args, target.Package)
	}
	args = append(args, target.Args...)
	return InstallResult{
		Name:            name,
		RegistryID:      entry.ID,
		RegistryVersion: entry.Version,
		Distribution:    runner,
		Command:         bin,
		Args:            args,
		Env:             cloneMap(target.Env),
	}, nil
}

func (r *Registry) installBinary(ctx context.Context, entry Entry, target BinaryTarget, name string) (InstallResult, error) {
	if r.dataDir == "" {
		return InstallResult{}, errors.New("machine data directory is not configured")
	}
	u, err := url.Parse(target.Archive)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return InstallResult{}, fmt.Errorf("invalid archive URL %q", target.Archive)
	}

	tmpDir, err := os.MkdirTemp("", "aide-acp-install-*")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "archive")
	sum, err := r.downloadAndHash(ctx, target.Archive, archivePath)
	if err != nil {
		return InstallResult{}, fmt.Errorf("download %s: %w", entry.ID, err)
	}

	if want := strings.ToLower(strings.TrimSpace(target.SHA256)); want != "" {
		want = strings.TrimPrefix(want, "sha256:")
		if sum != want {
			return InstallResult{}, fmt.Errorf("SHA-256 verification failed for %s: expected %s, got %s", entry.ID, want, sum)
		}
	}

	extractDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(extractDir, 0o700); err != nil {
		return InstallResult{}, err
	}
	if err := extractArchive(archivePath, target.Archive, extractDir); err != nil {
		return InstallResult{}, fmt.Errorf("extract %s: %w", entry.ID, err)
	}

	verDir := sanitizeSegment(entry.Version)
	if verDir == "" {
		verDir = "latest"
	}
	agentRoot := filepath.Join(r.installedRoot(), sanitizeSegment(name))
	finalDir := filepath.Join(agentRoot, verDir)
	if err := os.MkdirAll(agentRoot, 0o700); err != nil {
		return InstallResult{}, err
	}
	_ = os.RemoveAll(finalDir)
	if err := moveDir(extractDir, finalDir); err != nil {
		return InstallResult{}, fmt.Errorf("place installed files: %w", err)
	}

	cmdPath, err := resolveInstalledCommand(finalDir, target.Cmd)
	if err != nil {
		_ = os.RemoveAll(finalDir)
		return InstallResult{}, err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(cmdPath, 0o755)
	}

	return InstallResult{
		Name:            name,
		RegistryID:      entry.ID,
		RegistryVersion: entry.Version,
		Distribution:    "binary",
		Command:         cmdPath,
		Args:            slices.Clone(target.Args),
		Env:             cloneMap(target.Env),
		InstallDir:      finalDir,
		SHA256:          sum,
	}, nil
}

// Uninstall removes any machine-isolated binary installation for name.
func (r *Registry) Uninstall(name string) error {
	if r.dataDir == "" || strings.TrimSpace(name) == "" {
		return nil
	}
	root := filepath.Clean(r.installedRoot())
	target := filepath.Clean(filepath.Join(root, sanitizeSegment(name)))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("refusing to remove path outside installed root: %q", name)
	}
	return os.RemoveAll(target)
}

func (r *Registry) downloadAndHash(ctx context.Context, rawURL, dst string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aide-acp-registry/1.0")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return "", err
	}
	if n > maxArchiveBytes {
		return "", fmt.Errorf("archive exceeds %d byte limit", maxArchiveBytes)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractArchive(archivePath, origURL, dst string) error {
	lower := strings.ToLower(origURL)
	if idx := strings.IndexByte(lower, '?'); idx >= 0 {
		lower = lower[:idx]
	}
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, dst)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, dst)
	default:
		// Sniff magic bytes: ZIP starts with PK\x03\x04, gzip with \x1f\x8b.
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		var hdr [4]byte
		_, _ = io.ReadFull(f, hdr[:])
		_ = f.Close()
		if hdr[0] == 'P' && hdr[1] == 'K' {
			return extractZip(archivePath, dst)
		}
		if hdr[0] == 0x1f && hdr[1] == 0x8b {
			return extractTarGz(archivePath, dst)
		}
		return fmt.Errorf("unsupported archive format for %q (expected .zip or .tar.gz)", origURL)
	}
}

func safeExtractPath(root, entryName string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(entryName))
	if cleaned == "." || cleaned == "" {
		return root, nil
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, `..`+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes target directory", entryName)
	}
	target := filepath.Join(root, cleaned)
	rel, err := filepath.Rel(root, target)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("archive entry %q escapes target directory", entryName)
	}
	return target, nil
}

func extractZip(archivePath, dst string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > maxExtractedFiles {
		return fmt.Errorf("archive contains too many entries (%d)", len(zr.File))
	}
	var total int64
	for _, f := range zr.File {
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive contains disallowed symlink %q", f.Name)
		}
		target, err := safeExtractPath(dst, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			_ = rc.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(rc, maxExtractedBytes-total+1))
		_ = out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return copyErr
		}
		total += n
		if total > maxExtractedBytes {
			return fmt.Errorf("extracted content exceeds %d byte limit", maxExtractedBytes)
		}
	}
	return nil
}

func extractTarGz(archivePath, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	count := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		count++
		if count > maxExtractedFiles {
			return fmt.Errorf("archive contains too many entries (>%d)", maxExtractedFiles)
		}
		target, err := safeExtractPath(dst, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(out, io.LimitReader(tr, maxExtractedBytes-total+1))
			_ = out.Close()
			if copyErr != nil {
				return copyErr
			}
			total += n
			if total > maxExtractedBytes {
				return fmt.Errorf("extracted content exceeds %d byte limit", maxExtractedBytes)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive contains disallowed link %q", hdr.Name)
		}
	}
	return nil
}

func resolveInstalledCommand(installDir, cmd string) (string, error) {
	cleaned := strings.TrimSpace(cmd)
	if cleaned == "" {
		return "", errors.New("registry binary target has empty cmd")
	}
	target, err := safeExtractPath(installDir, cleaned)
	if err != nil {
		return "", err
	}
	candidates := []string{target}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(target), ".exe") {
		candidates = append(candidates, target+".exe")
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("installed command %q not found inside extracted archive", cmd)
}

func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device fallback (e.g. %TEMP% on C: and DataDir on D:).
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func sanitizeSegment(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, `\`, "-")
	s = strings.ReplaceAll(s, "..", "-")
	return strings.Trim(s, "-.")
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
