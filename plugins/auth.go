package plugins

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
	"github.com/mirkobrombin/goup/internal/plugin"
)

// sessionCookieName is the cookie that carries the opaque session token. Auth
// is bound to this unguessable token, never to the client IP (which is
// spoofable via X-Forwarded-For and shared behind NAT).
const sessionCookieName = "goup_session"

// newSessionToken returns a cryptographically random opaque session token.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AuthPluginConfig represents the configuration for the AuthPlugin.
type AuthPluginConfig struct {
	// Whather the plugin is enabled.
	Enable bool `json:"enable"`
	// URL paths to protect with authentication.
	ProtectedPaths []string `json:"protected_paths"`
	// username:password pairs for authentication.
	Credentials map[string]string `json:"credentials"`
	// Session expiration in seconds.
	// -1 means sessions never expire. Maximum allowed is 86400 seconds (24 hours).
	SessionExpiration int `json:"session_expiration"`
}

// session and AuthPluginState remain per domain.
type session struct {
	Username string
	Expiry   time.Time
}

// AuthPluginState internal state for the plugin.
type AuthPluginState struct {
	sessions map[string]session
	mu       sync.RWMutex
}

// AuthPlugin provides HTTP Basic Authentication for protected paths.
// Instead of storing one global conf/state, we now store a map of domain->config
// and a map of domain->plugin state, so each site has its own settings.
type AuthPlugin struct {
	plugin.BasePlugin
	siteConfigs map[string]AuthPluginConfig
	states      map[string]*AuthPluginState
}

func (p *AuthPlugin) Name() string {
	return "AuthPlugin"
}

func (p *AuthPlugin) OnInit() error {
	return nil
}

func (p *AuthPlugin) OnInitForSite(conf config.SiteConfig, domainLogger *logger.Logger) error {
	if err := p.SetupLoggers(conf, p.Name(), domainLogger); err != nil {
		return err
	}

	// Initialize maps once
	if p.siteConfigs == nil {
		p.siteConfigs = make(map[string]AuthPluginConfig)
	}
	if p.states == nil {
		p.states = make(map[string]*AuthPluginState)
	}

	pluginConfigRaw, ok := conf.PluginConfigs[p.Name()]
	if !ok {
		// Default to disabled if plugin config is not present
		p.siteConfigs[conf.Domain] = AuthPluginConfig{Enable: false}
		return nil
	}

	authConfig := AuthPluginConfig{
		Enable: false,
	}

	if rawMap, ok := pluginConfigRaw.(map[string]any); ok {
		authConfig.Enable = p.IsEnabled(rawMap)

		// ProtectedPaths
		if paths, ok := rawMap["protected_paths"].([]any); ok {
			for _, path := range paths {
				if pStr, ok := path.(string); ok {
					authConfig.ProtectedPaths = append(authConfig.ProtectedPaths, pStr)
				}
			}
		}

		// Credentials
		if creds, ok := rawMap["credentials"].(map[string]any); ok {
			authConfig.Credentials = make(map[string]string)
			for user, pass := range creds {
				if passStr, ok := pass.(string); ok {
					authConfig.Credentials[user] = passStr
				}
			}
		}

		// SessionExpiration
		if se, ok := rawMap["session_expiration"].(float64); ok {
			authConfig.SessionExpiration = int(se)
		}
	}

	// Validate session expiration
	if authConfig.SessionExpiration > 86400 {
		return errors.New("session_expiration cannot exceed 86400 seconds (24h)")
	}
	if authConfig.SessionExpiration < -1 {
		return errors.New("session_expiration cannot be less than -1")
	}

	p.siteConfigs[conf.Domain] = authConfig

	if !authConfig.Enable {
		return nil
	}

	// Initialize a new AuthPluginState for this domain
	p.states[conf.Domain] = &AuthPluginState{
		sessions: make(map[string]session),
	}

	if authConfig.SessionExpiration != -1 {
		go p.states[conf.Domain].cleanupExpiredSessions(time.Minute, p.DomainLogger)
	}

	p.DomainLogger.Infof("[AuthPlugin] Initialized for domain=%s with session_expiration=%d",
		conf.Domain, authConfig.SessionExpiration)

	return nil
}

func (p *AuthPlugin) BeforeRequest(r *http.Request) {}

func (p *AuthPlugin) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	// Determine the domain (host without port) to select the correct config/state
	host := r.Host
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	// If we have no config for this domain, do nothing
	conf, ok := p.siteConfigs[host]
	if !ok || !conf.Enable {
		return false
	}

	if conf.Credentials == nil {
		return false
	}

	protected := false
	for _, path := range conf.ProtectedPaths {
		if strings.HasPrefix(r.URL.Path, path) {
			protected = true
			break
		}
	}
	if !protected {
		// Not protected, continue with normal flow.
		return false
	}

	st, hasState := p.states[host]
	if !hasState {
		return false
	}

	// A valid session cookie authenticates the request.
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		if sess, exists := st.getSession(cookie.Value); exists {
			p.DomainLogger.Infof("[AuthPlugin] Valid session for user=%s", sess.Username)
			return false
		}
	}

	// No valid session, check for Authorization header.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		unauthorized(w)
		return true
	}

	// Parse Basic Auth
	username, password, ok := parseBasicAuth(authHeader)
	if !ok {
		unauthorized(w)
		return true
	}

	if !credentialsValid(conf.Credentials, username, password) {
		unauthorized(w)
		return true
	}

	token, err := newSessionToken()
	if err != nil {
		p.PluginLogger.Errorf("[AuthPlugin] Failed to generate session token: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return true
	}
	st.createSession(token, username, conf.SessionExpiration, p.PluginLogger)

	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
	if conf.SessionExpiration > 0 {
		cookie.MaxAge = conf.SessionExpiration
	}
	http.SetCookie(w, cookie)
	p.PluginLogger.Infof("[AuthPlugin] Authenticated user=%s", username)

	return false
}

// credentialsValid checks the supplied username/password against the configured
// credentials in constant time, and always performs a comparison (even for an
// unknown user) so response timing does not leak which usernames exist.
func credentialsValid(creds map[string]string, username, password string) bool {
	expected, userExists := creds[username]
	if !userExists {
		// Compare against a dummy of equal work to equalize timing.
		subtle.ConstantTimeCompare([]byte(password), []byte(password))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(password)) == 1
}

func (p *AuthPlugin) AfterRequest(w http.ResponseWriter, r *http.Request) {}
func (p *AuthPlugin) OnExit() error                                       { return nil }

// parseBasicAuth parses the Basic Authentication header.
func parseBasicAuth(authHeader string) (username, password string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
	if err != nil {
		return
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return
	}
	username, password = parts[0], parts[1]
	ok = true
	return
}

// unauthorized sends a 401 Unauthorized response with the appropriate header.
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

// getSession retrieves a session for the given token, if it exists and is valid.
func (s *AuthPluginState) getSession(token string) (session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, exists := s.sessions[token]
	if !exists {
		return session{}, false
	}

	// Check expiration
	if !sess.Expiry.IsZero() && sess.Expiry.Before(time.Now()) {
		return session{}, false
	}
	return sess, true
}

// createSession stores a new session under the given opaque token.
func (s *AuthPluginState) createSession(token, username string, expiration int, l *logger.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expiry time.Time
	if expiration != -1 {
		expiry = time.Now().Add(time.Duration(expiration) * time.Second)
	}
	s.sessions[token] = session{Username: username, Expiry: expiry}

	if expiration != -1 {
		l.Infof("[AuthPlugin] Created session user=%s expires=%v", username, expiry)
	} else {
		l.Infof("[AuthPlugin] Created session user=%s never expires", username)
	}
}

// cleanupExpiredSessions periodically removes expired sessions.
func (s *AuthPluginState) cleanupExpiredSessions(interval time.Duration, l *logger.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for token, sess := range s.sessions {
			if !sess.Expiry.IsZero() && sess.Expiry.Before(time.Now()) {
				delete(s.sessions, token)
				l.Infof("[AuthPlugin] Session expired removed user=%s", sess.Username)
			}
		}
		s.mu.Unlock()
	}
}
