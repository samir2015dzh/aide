// Package acpregistry fetches, caches, verifies, and installs agents from the
// official Agent Client Protocol Registry (agentclientprotocol/registry, Apache-2.0).
//
// A binary distribution is installed only from an https URL whose Registry entry
// declares its SHA-256, which the download must match; one without falls back to
// the entry's npx/uvx distribution, or is not installable. Binaries extract into
// machine-isolated subdirectories under
// <dataDir>/acp-registry/installed/<agent-name>/<version>/.
// Dedicated built-in providers such as antigravity (Registry id antigravity-acp)
// are explicitly protected from being reinstalled through the generic registry
// path (工单 #110 / #111, 审计 D).
package acpregistry

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
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
	defaultCacheTTL = 24 * time.Hour
	// failureTTL: after a failed refresh, how long List serves what it fell
	// back to before trying the network again (unless forced).
	failureTTL    = 5 * time.Minute
	maxIndexBytes = 8 << 20 // 8 MiB
	// Real agent packages are large (some over 250 MB); still bounded.
	maxArchiveBytes   = 1 << 30 // 1 GiB
	maxExtractedBytes = 2 << 30 // 2 GiB
	maxExtractedFiles = 16384
	// A download may take as long as it needs while bytes keep arriving: it
	// fails when connecting, the response headers, or a pause in the body
	// take longer than these.
	connectTimeout        = 20 * time.Second
	responseHeaderTimeout = 30 * time.Second
	downloadIdleTimeout   = 60 * time.Second
)

// snapshot.json is the official Registry index (DefaultRegistryURL) as of when
// it was last regenerated, verbatim; regenerate it with
//
//	go test -tags acpregistry_snapshot -run TestRegenerateSnapshot ./internal/acpregistry
//
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
	HTTPClient *http.Client // index and downloads (tests); default: production clients
	CacheTTL   time.Duration
	Platform   string // e.g. "windows-x86_64"; defaults to CurrentPlatform()
	PathEnv    string
}

// Registry manages loading, caching, and installing official ACP Registry agents.
type Registry struct {
	dataDir  string
	url      string
	client   *http.Client // the index: small, a total timeout
	download *http.Client // archives: no total timeout (see downloadIdleTimeout)
	idle     time.Duration
	ttl      time.Duration
	platform string
	pathEnv  string

	mu        sync.Mutex
	cached    *Index
	source    string
	syncedAt  time.Time
	lastError string
	failedAt  time.Time // when the last refresh failed (zero: it did not)
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
	client, download := opts.HTTPClient, opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.DialContext = (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext
		t.TLSHandshakeTimeout = connectTimeout
		t.ResponseHeaderTimeout = responseHeaderTimeout
		download = &http.Client{Transport: t}
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
		download: download,
		idle:     downloadIdleTimeout,
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
	fresh := r.cached != nil && (time.Since(r.syncedAt) < r.ttl || !r.failedAt.IsZero() && time.Since(r.failedAt) < failureTTL)
	if !forceRefresh && fresh {
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
			r.failedAt = time.Time{}
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
		r.failedAt = time.Time{}
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
			r.failedAt = time.Now()
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
		r.failedAt = time.Now()
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

// DedicatedBuiltin is the Aide built-in provider that an entry is, when it
// is one ("antigravity" for the Registry's antigravity-acp, by id or name,
// however spelled); "" otherwise. Such entries are never installed through
// the generic path.
func DedicatedBuiltin(e Entry) string {
	for _, s := range []string{e.ID, e.Name} {
		n := NormalizeAgentName(s, nil)
		n = strings.TrimPrefix(n, "google-")
		n = strings.TrimSuffix(strings.TrimSuffix(n, "-acp"), "-agent")
		if n == "antigravity" {
			return "antigravity"
		}
	}
	return ""
}

// Builtin is the built-in Aide agent a registry id is the program of, or "":
// Google's Antigravity ACP server (registry id antigravity-acp) is what the
// built-in antigravity agent runs (工单 #133). It is installed where that
// agent finds it (InstallTo), never as a custom agent beside it.
func Builtin(registryID string) string { return DedicatedBuiltin(Entry{ID: registryID}) }

// BestDistribution chooses the preferred installable distribution for platform:
// binary (platform-matched, https, with a declared SHA-256, in a format Aide
// installs) > npx > uvx. sha256Verified: the install is checked against the
// Registry's SHA-256 (binary). reason says why there is none, or why the
// platform's binary is not used when a runner is.
func BestDistribution(e Entry, platform string) (kind string, sha256Verified bool, reason string) {
	if b := DedicatedBuiltin(e); b != "" {
		return "", false, b + " is a dedicated built-in provider in Aide and is not installed via the generic ACP registry"
	}
	binaryWhy := ""
	if t, ok := e.Distribution.Binary[platform]; ok && strings.TrimSpace(t.Archive) != "" && strings.TrimSpace(t.Cmd) != "" {
		if binaryWhy = binaryProblem(t); binaryWhy == "" {
			return "binary", true, ""
		}
	}
	if e.Distribution.NPX != nil && strings.TrimSpace(e.Distribution.NPX.Package) != "" {
		return "npx", false, binaryWhy
	}
	if e.Distribution.UVX != nil && strings.TrimSpace(e.Distribution.UVX.Package) != "" {
		return "uvx", false, binaryWhy
	}
	if binaryWhy != "" {
		return "", false, binaryWhy
	}
	return "", false, fmt.Sprintf("no distribution available for platform %s", platform)
}

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func declaredSHA256(t BinaryTarget) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(t.SHA256)), "sha256:")
}

// secureDownload reports whether u may be downloaded from: https, or plain
// http to this computer's own loopback address (a registry served by a test
// or a local mirror; what it serves is still checked against its SHA-256).
func secureDownload(u *url.URL) bool {
	if u == nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		if strings.EqualFold(h, "localhost") {
			return true
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	}
	return false
}

// binaryProblem is why Aide will not install a binary target ("" when it will).
func binaryProblem(t BinaryTarget) string {
	u, err := url.Parse(strings.TrimSpace(t.Archive))
	if err != nil || !secureDownload(u) {
		return "its binary download is not an https URL, so Aide will not install it"
	}
	if !sha256RE.MatchString(declaredSHA256(t)) {
		return "the Registry declares no SHA-256 for its binary, so Aide will not install and run it unverified"
	}
	if f := archiveFormat(u.Path); f == formatUnsupported {
		return fmt.Sprintf("its binary package (%s) is in a format Aide cannot unpack (it takes .zip, .tar.gz, .tar.bz2 or a bare executable)", path.Base(u.Path))
	}
	return ""
}

type format int

const (
	formatSniff format = iota // no archive extension: an archive by its first bytes, else a bare executable
	formatZip
	formatTarGz
	formatTarBz2
	formatUnsupported
)

// archiveFormat is the format a download's URL path names.
func archiveFormat(p string) format {
	lower := strings.ToLower(p)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return formatZip
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return formatTarGz
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"), strings.HasSuffix(lower, ".tbz"):
		return formatTarBz2
	}
	for _, ext := range []string{".tar.xz", ".txz", ".tar.zst", ".tzst", ".tar", ".7z", ".rar", ".xz", ".zst", ".gz", ".bz2", ".dmg", ".pkg", ".msi", ".deb", ".rpm"} {
		if strings.HasSuffix(lower, ext) {
			return formatUnsupported
		}
	}
	return formatSniff
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

var agentNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Install downloads (verifying SHA-256 for binary archives) and prepares an
// official ACP Registry agent in a machine-isolated directory. reserved
// reports names Aide may not use for it (built-in agents, and custom agents
// other than this entry's earlier install); an explicit desiredName that is
// reserved is refused, never overwritten.
func (r *Registry) Install(ctx context.Context, registryID, desiredName string, reserved func(string) bool) (InstallResult, error) {
	registryID = strings.TrimSpace(registryID)
	if registryID == "" {
		return InstallResult{}, errors.New("registry_id is required")
	}
	if b := DedicatedBuiltin(Entry{ID: registryID}); b != "" {
		return InstallResult{}, fmt.Errorf("%s is a dedicated built-in provider in Aide and must not be installed via the generic ACP registry", b)
	}
	name := strings.TrimSpace(desiredName)
	if name != "" {
		if !agentNameRE.MatchString(name) {
			return InstallResult{}, fmt.Errorf("the name %q is not one Aide can use: lower case letters, digits and -, starting with a letter, at most 32", name)
		}
		if reserved != nil && reserved(name) {
			return InstallResult{}, fmt.Errorf("the name %q is already used by another agent on this machine: choose another name, or remove that agent first", name)
		}
	}
	entry, err := r.entry(ctx, registryID)
	if err != nil {
		return InstallResult{}, err
	}

	kind, _, reason := BestDistribution(*entry, r.platform)
	if kind == "" {
		return InstallResult{}, fmt.Errorf("%s cannot be installed: %s", entry.ID, reason)
	}

	if name == "" {
		name = NormalizeAgentName(entry.ID, reserved)
	}

	switch kind {
	case "binary":
		return r.installBinary(ctx, *entry, entry.Distribution.Binary[r.platform], name, "")
	case "npx":
		return r.installRunner(*entry, "npx", entry.Distribution.NPX, name)
	case "uvx":
		return r.installRunner(*entry, "uvx", entry.Distribution.UVX, name)
	default:
		return InstallResult{}, fmt.Errorf("unsupported distribution %q", kind)
	}
}

// InstallTo installs a built-in agent's program (Builtin) from its registry
// entry's binary archive for this platform, its SHA-256 checked, into dir,
// where that agent finds it: what was there is replaced (工单 #133).
func (r *Registry) InstallTo(ctx context.Context, registryID, dir string) (InstallResult, error) {
	name := Builtin(registryID)
	if name == "" {
		return InstallResult{}, fmt.Errorf("%q is not a built-in agent's program", registryID)
	}
	// Read afresh (the cache falls back when the registry can't be read): the
	// owner installs now what the registry names now.
	var entry *Entry
	for _, e := range r.List(ctx, true).Entries {
		if e.ID == registryID {
			entry = &e
			break
		}
	}
	if entry == nil {
		return InstallResult{}, fmt.Errorf("ACP registry agent %q not found", registryID)
	}
	target, ok := entry.Distribution.Binary[r.platform]
	if !ok || strings.TrimSpace(target.Archive) == "" || strings.TrimSpace(target.Cmd) == "" {
		return InstallResult{}, fmt.Errorf("%s has no download for this computer (%s)", entry.Name, r.platform)
	}
	if strings.TrimSpace(target.SHA256) == "" {
		return InstallResult{}, fmt.Errorf("%s's download for %s names no SHA-256 to check it by", entry.Name, r.platform)
	}
	return r.installBinary(ctx, *entry, target, name, dir)
}

// entry is registryID's entry, the registry read again once if it is not
// in the cached one (it may have been updated upstream).
func (r *Registry) entry(ctx context.Context, registryID string) (*Entry, error) {
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
		return nil, fmt.Errorf("ACP registry agent %q not found", registryID)
	}
	return entry, nil
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

// installBinary downloads and checks entry's archive and places it in finalDir,
// by default <data>/acp-registry/installed/<name>/<version>.
func (r *Registry) installBinary(ctx context.Context, entry Entry, target BinaryTarget, name, finalDir string) (InstallResult, error) {
	if r.dataDir == "" {
		return InstallResult{}, errors.New("machine data directory is not configured")
	}
	if why := binaryProblem(target); why != "" {
		return InstallResult{}, fmt.Errorf("%s cannot be installed: %s", entry.ID, why)
	}
	u, _ := url.Parse(strings.TrimSpace(target.Archive))

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

	if want := declaredSHA256(target); sum != want {
		return InstallResult{}, fmt.Errorf("SHA-256 verification failed for %s: expected %s, got %s", entry.ID, want, sum)
	}

	extractDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(extractDir, 0o700); err != nil {
		return InstallResult{}, err
	}
	if err := extractArchive(archivePath, u.Path, target.Cmd, extractDir); err != nil {
		return InstallResult{}, fmt.Errorf("extract %s: %w", entry.ID, err)
	}

	if finalDir == "" {
		verDir := sanitizeSegment(entry.Version)
		if verDir == "" {
			verDir = "latest"
		}
		finalDir = freshDir(filepath.Join(r.installedRoot(), sanitizeSegment(name), verDir))
	}
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o700); err != nil {
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

// freshDir is dir, or when something is already there (the same version
// installed before, its agent maybe still running) the first free dir-2,
// dir-3, …: what is there is left alone, since Windows removes neither a
// running program's file nor its folder; Uninstall removes them all.
func freshDir(dir string) string {
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return dir
	}
	for i := 2; ; i++ {
		d := fmt.Sprintf("%s-%d", dir, i)
		if _, err := os.Lstat(d); os.IsNotExist(err) {
			return d
		}
	}
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

// errStalled: a download's body made no progress for the idle timeout.
var errStalled = errors.New("the download stalled")

func (r *Registry) downloadAndHash(ctx context.Context, rawURL, dst string) (string, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aide-acp-registry/1.0")
	c := *r.download
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !secureDownload(req.URL) {
			return fmt.Errorf("refusing a redirect to a non-https URL (%s)", req.URL.Redacted())
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	resp, err := c.Do(req)
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

	// No total limit: a large package on a slow line takes as long as it
	// takes, but one that stops sending fails after r.idle.
	idle := time.AfterFunc(r.idle, func() { cancel(errStalled) })
	defer idle.Stop()
	body := &progressReader{r: io.LimitReader(resp.Body, maxArchiveBytes+1), progress: func() { idle.Reset(r.idle) }}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), body)
	idle.Stop()
	// A cancelled request's body can also end in a clean EOF: what
	// cancelled it decides.
	if errors.Is(context.Cause(ctx), errStalled) {
		return "", fmt.Errorf("%w: no data for %s", errStalled, r.idle)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return "", err
	}
	if n > maxArchiveBytes {
		return "", fmt.Errorf("archive exceeds %d byte limit", maxArchiveBytes)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressReader struct {
	r        io.Reader
	progress func()
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.progress()
	}
	return n, err
}

// extractArchive unpacks the download at archivePath (urlPath: its URL's
// path) into dst. A download whose URL names no archive format and whose
// first bytes are no archive Aide knows is a bare executable: it is placed
// as the target's cmd.
func extractArchive(archivePath, urlPath, cmd, dst string) error {
	switch archiveFormat(urlPath) {
	case formatZip:
		return extractZip(archivePath, dst)
	case formatTarGz:
		return extractTar(archivePath, dst, gunzip)
	case formatTarBz2:
		return extractTar(archivePath, dst, bunzip2)
	case formatUnsupported:
		return fmt.Errorf("unsupported archive format for %q", path.Base(urlPath))
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	var hdr [6]byte
	n, _ := io.ReadFull(f, hdr[:])
	_ = f.Close()
	switch b := hdr[:n]; {
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		return extractZip(archivePath, dst)
	case bytes.HasPrefix(b, []byte{0x1f, 0x8b}):
		return extractTar(archivePath, dst, gunzip)
	case bytes.HasPrefix(b, []byte("BZh")):
		return extractTar(archivePath, dst, bunzip2)
	case bytes.HasPrefix(b, []byte{0xfd, '7', 'z', 'X', 'Z', 0}), bytes.HasPrefix(b, []byte{0x28, 0xb5, 0x2f, 0xfd}), bytes.HasPrefix(b, []byte("7z\xbc\xaf")):
		return fmt.Errorf("unsupported archive format for %q (xz, zstd or 7z)", path.Base(urlPath))
	}
	return placeExecutable(archivePath, cmd, dst)
}

// placeExecutable puts a bare executable download where cmd names it.
func placeExecutable(file, cmd, dst string) error {
	target, err := safeExtractPath(dst, strings.TrimSpace(cmd))
	if err != nil {
		return err
	}
	if target == dst {
		return errors.New("registry binary target has empty cmd")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(file)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func gunzip(r io.Reader) (io.Reader, error)  { return gzip.NewReader(r) }
func bunzip2(r io.Reader) (io.Reader, error) { return bzip2.NewReader(r), nil }

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

func extractTar(archivePath, dst string, decompress func(io.Reader) (io.Reader, error)) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	dr, err := decompress(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(dr)
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
