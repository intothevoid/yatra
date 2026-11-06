package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/karan/travel-diary/internal/config"
)

type AuthManager struct {
	path     string
	mu       sync.Mutex
	sessions map[string]time.Time
	keys     map[string][]byte
}
type passwordRecord struct {
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

func NewAuthManager(cfg *config.Config) *AuthManager {
	return &AuthManager{path: filepath.Join(cfg.DataDir, "app-password.json"), sessions: make(map[string]time.Time), keys: make(map[string][]byte)}
}
func (a *AuthManager) record() (passwordRecord, error) {
	b, e := os.ReadFile(a.path)
	if e != nil {
		return passwordRecord{}, e
	}
	var r passwordRecord
	e = json.Unmarshal(b, &r)
	return r, e
}
func (a *AuthManager) configured() bool { _, e := os.Stat(a.path); return e == nil }
func passwordHash(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
}
func (a *AuthManager) status(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]bool{"configured": a.configured(), "authenticated": a.Authenticated(r)})
}
func (a *AuthManager) issue(w http.ResponseWriter, r *http.Request, key []byte) error {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return e
	}
	token := hex.EncodeToString(b)
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(12 * time.Hour)
	a.keys[token] = key
	a.mu.Unlock()
	https := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{Name: "yatra_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: https, MaxAge: 43200})
	return nil
}
func (a *AuthManager) setup(w http.ResponseWriter, r *http.Request) {
	if a.configured() {
		http.Error(w, "password already configured", http.StatusConflict)
		return
	}
	a.acceptPassword(w, r, true)
}
func (a *AuthManager) login(w http.ResponseWriter, r *http.Request) {
	if !a.configured() {
		http.Error(w, "set up a password first", http.StatusConflict)
		return
	}
	a.acceptPassword(w, r, false)
}
func (a *AuthManager) acceptPassword(w http.ResponseWriter, r *http.Request, setup bool) {
	var req struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || len(req.Password) < 12 {
		http.Error(w, "use a password with at least 12 characters", http.StatusBadRequest)
		return
	}
	var key []byte
	if setup {
		salt := make([]byte, 16)
		if _, e := rand.Read(salt); e != nil {
			http.Error(w, "could not set password", 500)
			return
		}
		key = passwordHash(req.Password, salt)
		rec := passwordRecord{Salt: hex.EncodeToString(salt), Hash: hex.EncodeToString(key)}
		b, _ := json.Marshal(rec)
		if e := os.MkdirAll(filepath.Dir(a.path), 0700); e != nil {
			http.Error(w, "could not save password", 500)
			return
		}
		f, e := os.OpenFile(a.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			http.Error(w, "password setup has already started", http.StatusConflict)
			return
		}
		if _, e = f.Write(b); e != nil {
			f.Close()
			http.Error(w, "could not save password", 500)
			return
		}
		f.Close()
	} else {
		rec, e := a.record()
		if e != nil {
			http.Error(w, "could not read password configuration", 500)
			return
		}
		salt, _ := hex.DecodeString(rec.Salt)
		want, _ := hex.DecodeString(rec.Hash)
		key = passwordHash(req.Password, salt)
		if len(key) != len(want) || subtle.ConstantTimeCompare(key, want) != 1 {
			http.Error(w, "incorrect password", http.StatusUnauthorized)
			return
		}
	}
	if e := a.issue(w, r, key); e != nil {
		http.Error(w, "could not create session", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *AuthManager) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("yatra_session"); e == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		delete(a.keys, c.Value)
		a.mu.Unlock()
	}
	https := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{Name: "yatra_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: https, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}
func (a *AuthManager) SessionKey(r *http.Request) []byte {
	c, e := r.Cookie("yatra_session")
	if e != nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]byte(nil), a.keys[c.Value]...)
}
func (a *AuthManager) Authenticated(r *http.Request) bool {
	c, e := r.Cookie("yatra_session")
	if e != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	expires, ok := a.sessions[c.Value]
	if ok && time.Now().Before(expires) {
		return true
	}
	delete(a.sessions, c.Value)
	delete(a.keys, c.Value)
	return false
}
func (a *AuthManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		public := p == "/" || strings.HasPrefix(p, "/assets/") || p == "/health/live" || p == "/health/ready" || p == "/api/v1/auth/status" || p == "/api/v1/auth/setup" || p == "/api/v1/auth/login"
		if !public && !a.Authenticated(r) {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
