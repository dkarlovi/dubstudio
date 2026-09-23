package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidSessionID(t *testing.T) {
	id, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID error: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("newSessionID() = %q, want 32 hex chars", id)
	}
	if !validSessionID(id) {
		t.Errorf("validSessionID rejected newSessionID's own output %q", id)
	}

	other, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID error: %v", err)
	}
	if other == id {
		t.Errorf("two newSessionID calls returned the same id %q", id)
	}

	// The cookie value becomes a directory name, so anything but exactly
	// what newSessionID produces has to be refused -- see validSessionID.
	for _, bad := range []string{
		"",
		"../../../../etc/passwd",
		"..",
		"0123456789abcdef0123456789abcde",   // 31, too short
		"0123456789abcdef0123456789abcdef0", // 33, too long
		"0123456789ABCDEF0123456789abcdef",  // uppercase hex
		"0123456789abcdef0123456789abcdeg",  // non-hex letter
		"0123456789abcdef0123456789abcd/f",  // path separator
		"0123456789abcdef0123456789abcd.f",
	} {
		if validSessionID(bad) {
			t.Errorf("validSessionID(%q) = true, want false", bad)
		}
	}
}

// tenantTestServer is an httpServer over a fresh work-dir root, with no
// Service behind it -- enough for every tenant-resolution path, since
// those never reach the Service.
func tenantTestServer(t *testing.T) (*httpServer, string) {
	t.Helper()
	root := t.TempDir()
	return newHTTPServer(localTenantFunc(ServiceConfig{WorkDir: root}), "", 0, 0), root
}

func resolveWithCookie(t *testing.T, h *httpServer, value string) (*tenant, *httptest.ResponseRecorder) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/session", nil)
	if value != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
	}
	w := httptest.NewRecorder()
	tn, ok := h.resolveTenant(w, r)
	if !ok {
		t.Fatalf("resolveTenant failed: %d %s", w.Code, w.Body.String())
	}
	return tn, w
}

func tenantWorkDir(t *testing.T, tn *tenant) string {
	t.Helper()
	ls, ok := tn.service.(*LocalService)
	if !ok {
		t.Fatalf("tenant service is %T, want *LocalService", tn.service)
	}
	return ls.cfg.WorkDir
}

func tenantCacheDir(t *testing.T, tn *tenant) string {
	t.Helper()
	ls, ok := tn.service.(*LocalService)
	if !ok {
		t.Fatalf("tenant service is %T, want *LocalService", tn.service)
	}
	return ls.cfg.CacheDir
}

func TestResolveTenant(t *testing.T) {
	t.Run("issues an HttpOnly cookie on first contact", func(t *testing.T) {
		h, _ := tenantTestServer(t)
		_, w := resolveWithCookie(t, h, "")

		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("got %d cookies, want 1", len(cookies))
		}
		c := cookies[0]
		if c.Name != sessionCookieName {
			t.Errorf("cookie name = %q, want %q", c.Name, sessionCookieName)
		}
		if !validSessionID(c.Value) {
			t.Errorf("issued cookie value %q is not a valid session id", c.Value)
		}
		if !c.HttpOnly {
			t.Error("cookie is not HttpOnly")
		}
		if c.Path != "/" {
			t.Errorf("cookie path = %q, want /", c.Path)
		}
	})

	t.Run("reuses one tenant for the same cookie, and issues no second cookie", func(t *testing.T) {
		h, _ := tenantTestServer(t)
		first, w := resolveWithCookie(t, h, "")
		id := w.Result().Cookies()[0].Value

		again, w2 := resolveWithCookie(t, h, id)
		if again != first {
			t.Errorf("same cookie built a second tenant (%p vs %p)", again, first)
		}
		if got := len(w2.Result().Cookies()); got != 0 {
			t.Errorf("re-issued %d cookies for a known session id, want 0", got)
		}
		if got := len(h.tenants); got != 1 {
			t.Errorf("server holds %d tenants, want 1", got)
		}
	})

	t.Run("different cookies get isolated sessions, stores and work dirs", func(t *testing.T) {
		h, root := tenantTestServer(t)
		a, wa := resolveWithCookie(t, h, "")
		b, wb := resolveWithCookie(t, h, "")

		idA := wa.Result().Cookies()[0].Value
		idB := wb.Result().Cookies()[0].Value
		if idA == idB {
			t.Fatal("two first-contact requests got the same session id")
		}
		if a == b {
			t.Fatal("two different cookies share one tenant")
		}

		dirA, dirB := tenantWorkDir(t, a), tenantWorkDir(t, b)
		if dirA == dirB {
			t.Fatalf("both tenants share work dir %s", dirA)
		}
		if want := filepath.Join(root, "sessions", idA); dirA != want {
			t.Errorf("work dir = %s, want %s", dirA, want)
		}

		// The store is what writes session.json, so it has to follow the
		// per-tenant work dir, not the shared root.
		storeA, ok := a.store.(*FileSessionStore)
		if !ok {
			t.Fatalf("store is %T, want *FileSessionStore", a.store)
		}
		if storeA.Dir != dirA {
			t.Errorf("store dir = %s, want the tenant work dir %s", storeA.Dir, dirA)
		}

		// Setting one tenant's session must not be visible in the other.
		a.session = &Session{Name: "a.vtt"}
		if b.session != nil {
			t.Errorf("tenant b session = %v, want nil after only a was set", b.session)
		}
	})

	t.Run("tenants share one cache dir so a new cookie doesn't re-synthesize", func(t *testing.T) {
		h, root := tenantTestServer(t)
		a, _ := resolveWithCookie(t, h, "")
		b, _ := resolveWithCookie(t, h, "")

		want := filepath.Join(root, "cache")
		if got := tenantCacheDir(t, a); got != want {
			t.Errorf("tenant a cache dir = %s, want %s", got, want)
		}
		if got := tenantCacheDir(t, b); got != want {
			t.Errorf("tenant b cache dir = %s, want %s", got, want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Errorf("shared cache dir was not created: %v", err)
		}
	})

	t.Run("an explicit CacheDir is honored instead of the default", func(t *testing.T) {
		root, custom := t.TempDir(), t.TempDir()
		h := newHTTPServer(localTenantFunc(ServiceConfig{WorkDir: root, CacheDir: custom}), "", 0, 0)
		tn, _ := resolveWithCookie(t, h, "")
		if got := tenantCacheDir(t, tn); got != custom {
			t.Errorf("cache dir = %s, want the configured %s", got, custom)
		}
	})

	t.Run("a path-traversal cookie is replaced by a fresh id, never used as a dir", func(t *testing.T) {
		h, root := tenantTestServer(t)
		const evil = "../../../../pwned"

		r := httptest.NewRequest(http.MethodGet, "/session", nil)
		// AddCookie sanitizes some values, so set the header directly --
		// a real attacker isn't going through net/http's client either.
		r.Header.Set("Cookie", sessionCookieName+"="+evil)
		w := httptest.NewRecorder()
		tn, ok := h.resolveTenant(w, r)
		if !ok {
			t.Fatalf("resolveTenant failed: %d %s", w.Code, w.Body.String())
		}

		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("got %d cookies, want a freshly issued one", len(cookies))
		}
		if !validSessionID(cookies[0].Value) {
			t.Errorf("replacement cookie %q is not a valid session id", cookies[0].Value)
		}

		dir := tenantWorkDir(t, tn)
		if parent := filepath.Dir(dir); parent != filepath.Join(root, "sessions") {
			t.Errorf("work dir %s escaped %s/sessions", dir, root)
		}
		if strings.Contains(dir, "pwned") {
			t.Errorf("work dir %s contains the attacker-supplied value", dir)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(root), "pwned")); err == nil {
			t.Error("traversal created a directory outside the work-dir root")
		}
	})
}

func TestHandleGetSessionIsPerTenant(t *testing.T) {
	h, _ := tenantTestServer(t)
	mux := h.mux()

	// First contact: no session yet, and a cookie comes back.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/session", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"empty": true`) {
		t.Errorf("body = %s, want the empty-session DTO", w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies from GET /session, want 1", len(cookies))
	}

	// Give that tenant a session, then confirm a different browser's
	// request doesn't see it.
	h.tenants[cookies[0].Value].session = &Session{Name: "mine.vtt"}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/session", nil))
	if strings.Contains(w.Body.String(), "mine.vtt") {
		t.Errorf("a second browser saw the first one's session: %s", w.Body.String())
	}

	// ...while the original cookie still does.
	r := httptest.NewRequest(http.MethodGet, "/session", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookies[0].Value})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "mine.vtt") {
		t.Errorf("the owning cookie lost its session: %s", w.Body.String())
	}
}
