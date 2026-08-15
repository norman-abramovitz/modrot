package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func newTestNPMServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "xterm/5.3.0":
			_, _ = w.Write([]byte(`{
				"version": "5.3.0",
				"deprecated": "This package is now deprecated. Move to @xterm/xterm instead.",
				"repository": {"type": "git", "url": "git+https://github.com/xtermjs/xterm.js.git"}
			}`))
		case "circular-json/0.5.9":
			_, _ = w.Write([]byte(`{
				"version": "0.5.9",
				"deprecated": "CircularJSON is in maintenance only, flatted is its successor.",
				"repository": {"url": "git://github.com/WebReflection/circular-json.git"}
			}`))
		case "stringrepo/1.0.0":
			_, _ = w.Write([]byte(`{"version":"1.0.0","repository":"https://github.com/foo/bar"}`))
		case "shorthand/1.0.0":
			_, _ = w.Write([]byte(`{"version":"1.0.0","repository":"github:foo/bar"}`))
		case "sluggish/1.0.0":
			_, _ = w.Write([]byte(`{"version":"1.0.0","repository":"foo/bar"}`))
		case "norepo/1.0.0":
			_, _ = w.Write([]byte(`{"version":"1.0.0"}`))
		case "xterm/latest":
			_, _ = w.Write([]byte(`{
				"version": "5.3.0",
				"deprecated": "This package is now deprecated. Move to @xterm/xterm instead.",
				"repository": {"url": "git+https://github.com/xtermjs/xterm.js.git"}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRepositoryURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"object", `{"type":"git","url":"git+https://github.com/foo/bar.git"}`, "git+https://github.com/foo/bar.git"},
		{"string url", `"https://github.com/foo/bar"`, "https://github.com/foo/bar"},
		{"github shorthand", `"github:foo/bar"`, "https://github.com/foo/bar"},
		{"bare slug", `"foo/bar"`, "https://github.com/foo/bar"},
		{"empty", ``, ""},
		{"null", `null`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.raw != "" {
				raw = json.RawMessage(tt.raw)
			}
			if got := repositoryURL(raw); got != tt.want {
				t.Errorf("repositoryURL(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestResolveNPMPopulatesOwnerRepo(t *testing.T) {
	srv := newTestNPMServer(t)
	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{
		{Path: "xterm", Version: "5.3.0", Ecosystem: "npm"},
		{Path: "stringrepo", Version: "1.0.0", Ecosystem: "npm"},
		{Path: "shorthand", Version: "1.0.0", Ecosystem: "npm"},
		{Path: "sluggish", Version: "1.0.0", Ecosystem: "npm"},
		{Path: "norepo", Version: "1.0.0", Ecosystem: "npm"},
		{Path: "missing", Version: "9.9.9", Ecosystem: "npm"},
	}
	got, _ := resolveNPMWithClient(mods, c)
	if got != 4 {
		t.Errorf("resolved %d, want 4", got)
	}

	want := map[string][2]string{
		"xterm":      {"xtermjs", "xterm.js"},
		"stringrepo": {"foo", "bar"},
		"shorthand":  {"foo", "bar"},
		"sluggish":   {"foo", "bar"},
		"norepo":     {"", ""},
		"missing":    {"", ""},
	}
	for _, m := range mods {
		w := want[m.Path]
		if m.Owner != w[0] || m.Repo != w[1] {
			t.Errorf("%s: got %s/%s, want %s/%s", m.Path, m.Owner, m.Repo, w[0], w[1])
		}
	}
}

func TestCheckNPMDeprecations(t *testing.T) {
	srv := newTestNPMServer(t)
	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{
		{Path: "xterm", Version: "5.3.0", Ecosystem: "npm"},
		{Path: "circular-json", Version: "0.5.9", Ecosystem: "npm"},
		{Path: "norepo", Version: "1.0.0", Ecosystem: "npm"},
	}
	got, _ := checkNPMDeprecationsWithClient(mods, c)
	if got != 2 {
		t.Errorf("found %d deprecated, want 2", got)
	}
	if !strings.Contains(mods[0].Deprecated, "@xterm/xterm") {
		t.Errorf("xterm: Deprecated = %q", mods[0].Deprecated)
	}
	if !strings.Contains(mods[1].Deprecated, "flatted") {
		t.Errorf("circular-json: Deprecated = %q", mods[1].Deprecated)
	}
	if mods[2].Deprecated != "" {
		t.Errorf("norepo: Deprecated = %q, want empty", mods[2].Deprecated)
	}
}

// An unlocked unit has no version, so the client must fall back to
// dist-tags.latest and record which version it resolved to.
func TestResolveNPMUnlockedUsesLatest(t *testing.T) {
	srv := newTestNPMServer(t)
	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{{Path: "xterm", Version: "", Ecosystem: "npm"}}
	if got, _ := resolveNPMWithClient(mods, c); got != 1 {
		t.Fatalf("resolved %d, want 1", got)
	}
	if mods[0].Version != "5.3.0" {
		t.Errorf("Version = %q, want 5.3.0 from dist-tags.latest", mods[0].Version)
	}
	if mods[0].Owner != "xtermjs" {
		t.Errorf("Owner = %q, want xtermjs", mods[0].Owner)
	}
}

// Scoped names contain a slash and are the common case in modern npm trees.
// The registry accepts the slash unescaped, so the URL must be built without
// encoding it — encoding would turn a valid path into a 404, which the client
// would then treat as "package does not exist" and silently report nothing.
func TestNPMClientScopedNames(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RequestURI is the raw request line. r.URL.Path is Go's DECODED
		// form, in which a percent-encoded slash is indistinguishable from a
		// literal one, so asserting on it would pass even if the client
		// escaped the scope separator — a false guarantee.
		gotURI = r.RequestURI
		_, _ = w.Write([]byte(`{"version":"7.24.0","repository":{"url":"git+https://github.com/babel/babel.git"}}`))
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{{Path: "@babel/core", Version: "7.24.0", Ecosystem: "npm"}}
	if got, _ := resolveNPMWithClient(mods, c); got != 1 {
		t.Fatalf("resolved %d, want 1", got)
	}
	if gotURI != "/@babel/core/7.24.0" {
		t.Errorf("requested %q, want /@babel/core/7.24.0 with the scope slash unescaped", gotURI)
	}
	if mods[0].Owner != "babel" || mods[0].Repo != "babel" {
		t.Errorf("got %s/%s, want babel/babel", mods[0].Owner, mods[0].Repo)
	}
}

// A 404 is a definitive answer and gets cached; a transient failure is an
// absence of information and must not be, so a later phase retries it.
// Conflating them would let modrot report a clean scan while having silently
// skipped packages.
func TestNPMClientDistinguishes404FromFailure(t *testing.T) {
	var notFoundHits, errorHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "gone") {
			notFoundHits++
			w.WriteHeader(http.StatusNotFound)
			return
		}
		errorHits++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL

	// 404: definitive, cached, ok=true.
	for i := 0; i < 3; i++ {
		info, ok := c.fetch("gone", "1.0.0")
		if !ok {
			t.Fatalf("run %d: 404 reported as a failure, want a definitive answer", i)
		}
		if info != nil {
			t.Fatalf("run %d: got info for a 404", i)
		}
	}
	if notFoundHits != 1 {
		t.Errorf("404 fetched %d times, want 1 (should be cached)", notFoundHits)
	}

	// 429: transient, not cached, ok=false, so it is retried each time.
	for i := 0; i < 3; i++ {
		if _, ok := c.fetch("flaky", "1.0.0"); ok {
			t.Fatalf("run %d: 429 reported as a definitive answer", i)
		}
	}
	if errorHits != 3 {
		t.Errorf("transient failure fetched %d times, want 3 (must not be cached)", errorHits)
	}
}

// A package the registry cannot be reached about must be counted, so the user
// is told the scan is incomplete rather than shown a false all-clear.
func TestResolveNPMReportsUnreachablePackages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "good") {
			_, _ = w.Write([]byte(`{"version":"1.0.0","repository":"foo/bar"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{
		{Path: "good", Version: "1.0.0", Ecosystem: "npm"},
		{Path: "unreachable", Version: "1.0.0", Ecosystem: "npm"},
	}
	if got, _ := resolveNPMWithClient(mods, c); got != 1 {
		t.Errorf("resolved %d, want 1", got)
	}
	if mods[0].Owner != "foo" {
		t.Errorf("good: Owner = %q, want foo", mods[0].Owner)
	}
	// The unreachable one is left untouched rather than silently marked clean.
	if mods[1].Owner != "" {
		t.Errorf("unreachable: Owner = %q, want empty", mods[1].Owner)
	}
}

// The deprecation phase must report its own unreachable packages. For an
// unlocked unit the resolve phase fills in the version, so this phase keys on
// a version resolve never fetched — its failures are not a subset of
// resolve's, and dropping them would leave a package silently unchecked.
func TestCheckNPMDeprecationsReportsUnreachablePackages(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{{Path: "unreachable", Version: "1.0.0", Ecosystem: "npm"}}
	if got, _ := checkNPMDeprecationsWithClient(mods, c); got != 0 {
		t.Errorf("found %d deprecated, want 0", got)
	}
	if hits == 0 {
		t.Error("registry was never contacted")
	}
	// A transient failure must not be cached, so a retry re-requests it.
	if _, ok := c.fetch("unreachable", "1.0.0"); ok {
		t.Error("transient failure was cached as a definitive answer")
	}
	if mods[0].Deprecated != "" {
		t.Errorf("Deprecated = %q, want empty", mods[0].Deprecated)
	}
}

// The same package@version must be fetched once no matter how often it appears.
func TestNPMClientCaches(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"version":"1.0.0","repository":"foo/bar"}`))
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL
	for i := 0; i < 3; i++ {
		if info, _ := c.fetch("pkg", "1.0.0"); info == nil {
			t.Fatal("fetch returned nil")
		}
	}
	if hits != 1 {
		t.Errorf("server hit %d times, want 1", hits)
	}
}

// A version that came from dist-tags.latest is pinned nowhere in the repo, so
// it must be distinguishable from one the manifest or lockfile actually names.
// Without the mark the two render identically and a stale lockfile looks
// installed.
func TestResolveNPMMarksInferredVersion(t *testing.T) {
	srv := newTestNPMServer(t)
	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{
		{Path: "xterm", Version: "", Ecosystem: "npm"},
		{Path: "xterm", Version: "5.3.0", Ecosystem: "npm"},
	}
	if got, _ := resolveNPMWithClient(mods, c); got != 2 {
		t.Fatalf("resolved %d, want 2", got)
	}
	if !mods[0].VersionInferred {
		t.Error("unresolved dependency: VersionInferred = false, want true")
	}
	if mods[1].VersionInferred {
		t.Error("lockfile-pinned dependency: VersionInferred = true, want false")
	}
}

// captureStderr captures stderr output during fn execution.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	fn()

	_ = w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// A dependency declared as a workspace sibling or local path must never be
// asked about, because the registry's 404 would be meaningless — and because
// that meaningless 404 is what used to make every real 404 unreportable.
func TestNonRegistryDepsAreNeverRequested(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{
		{Path: "ui", Version: "", Ecosystem: "npm", NonRegistry: true},
		{Path: "shared", Version: "", Ecosystem: "npm", NonRegistry: true},
	}
	_ = captureStderr(t, func() { _, _ = resolveNPMWithClient(mods, c) })

	if len(requested) != 0 {
		t.Errorf("registry was asked about non-registry deps: %v", requested)
	}
}

// A package the registry definitively does not have, declared with an ordinary
// version range, is an anomaly: unpublished, renamed, or a typo. Dropping it
// silently is what a rot detector must never do.
func TestMissingRegistryPackagesAreReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "good") {
			_, _ = w.Write([]byte(`{"version":"1.0.0","repository":"foo/bar"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{
		{Path: "good", Version: "1.0.0", Ecosystem: "npm"},
		{Path: "typosquatted", Version: "1.0.0", Ecosystem: "npm"},
	}
	stderr := captureStderr(t, func() { _, _ = resolveNPMWithClient(mods, c) })

	if !strings.Contains(stderr, "typosquatted") {
		t.Errorf("missing package not named on stderr, got: %q", stderr)
	}
	if strings.Contains(stderr, "good") {
		t.Errorf("resolvable package should not be reported, got: %q", stderr)
	}
}

// Resolve and deprecation are two phases over the same client. A package the
// registry does not have is missing in both, but it is one problem and must be
// named once — two identical lists read as two different faults.
func TestMissingPackageReportedOncePerRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := newNPMClient()
	c.baseURL = srv.URL

	mods := []Module{{Path: "ghost-package", Version: "1.0.0", Ecosystem: "npm"}}
	stderr := captureStderr(t, func() {
		_, _ = resolveNPMWithClient(mods, c)
		_, _ = checkNPMDeprecationsWithClient(mods, c)
	})

	if got := strings.Count(stderr, "ghost-package"); got != 1 {
		t.Errorf("ghost-package named %d times, want 1; stderr:\n%s", got, stderr)
	}
}
