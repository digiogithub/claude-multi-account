//go:build linux

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var profileRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var launchRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
var sessionRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func ValidateProfileID(id string) error {
	if !profileRE.MatchString(id) {
		return errors.New("Profile ID must be 1-64 letters, digits, underscores or hyphens, starting with a letter or digit.")
	}
	return nil
}
func expiry(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	// Python's ISO timestamps also accept a space separator and compact offsets.
	v := *value
	if len(v) > 10 && v[10] == ' ' {
		v = v[:10] + "T" + v[11:]
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z0700", "2006-01-02T15:04:05.999999999Z07", "2006-01-02T15:04Z07:00"} {
		if t, e := time.Parse(layout, v); e == nil {
			return &t, nil
		}
	}
	return nil, errors.New("Expiry must be an ISO timestamp with timezone.")
}
func EnsureNotExpired(p Profile) error {
	t, e := expiry(p.ExpiresAt)
	if e != nil {
		return e
	}
	if t != nil && !t.After(time.Now()) {
		return errors.New("Profile token has expired; renew it before use.")
	}
	return nil
}
func validateMetadata(id string, p Profile) error {
	if e := ValidateProfileID(id); e != nil {
		return e
	}
	if p.ID != id || p.Verification != "unverified" {
		return errors.New("Malformed profile identity/verification.")
	}
	for _, v := range []string{p.Label, p.Email, p.Organization, p.OrganizationID} {
		if !utf8.ValidString(v) || strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > 512 {
			return errors.New("Profile metadata must contain nonempty printable text.")
		}
		for _, r := range v {
			if r < 32 {
				return errors.New("Profile metadata must contain nonempty printable text.")
			}
		}
	}
	u := strings.TrimPrefix(p.OrganizationID, "urn:uuid:")
	u = strings.Trim(u, "{}")
	u = strings.ReplaceAll(u, "-", "")
	if !uuidRE.MatchString(u) {
		return errors.New("Organization ID must be a UUID.")
	}
	if p.SecretBackend != "file" && p.SecretBackend != "secret-service" {
		return errors.New("Unknown profile secret backend; Linux supports file and secret-service only.")
	}
	if !launchRE.MatchString(p.SecretRef) {
		return errors.New("Invalid secret reference.")
	}
	_, e := expiry(p.ExpiresAt)
	return e
}
func LoadRegistry() (Registry, error) {
	r := Registry{1, map[string]Profile{}}
	path := filepath.Join(ConfigHome(), "profiles.json")
	if !exists(path) {
		if e := noLinks(ConfigHome()); e != nil {
			return r, e
		}
		return r, nil
	}
	if e := private(ConfigHome(), true); e != nil {
		return r, e
	}
	if e := ReadJSON(path, &r, true); e != nil {
		return r, e
	}
	if r.Version != 1 || r.Profiles == nil {
		return r, errors.New("Unsupported or malformed profile registry.")
	}
	for id, p := range r.Profiles {
		if e := validateMetadata(id, p); e != nil {
			return r, e
		}
	}
	return r, nil
}
func GetProfile(id string) (Profile, error) {
	if e := ValidateProfileID(id); e != nil {
		return Profile{}, e
	}
	r, e := LoadRegistry()
	if e != nil {
		return Profile{}, e
	}
	p, ok := r.Profiles[id]
	if !ok {
		return p, errors.New("Unknown profile: " + id)
	}
	return p, nil
}
func validateToken(token string) error {
	if token == "" || !utf8.ValidString(token) || utf8.RuneCountInString(token) > 16384 {
		return errors.New("Token must be a nonempty single value without whitespace.")
	}
	for _, c := range token {
		if unicode.IsSpace(c) || c < 32 {
			return errors.New("Token must be a nonempty single value without whitespace.")
		}
	}
	return nil
}
func backend(name string) (string, error) {
	if name == "auto" {
		name = "secret-service"
	}
	if name == "file" {
		return name, nil
	}
	if name == "secret-service" {
		if _, e := exec.LookPath("secret-tool"); e == nil {
			return name, nil
		}
	}
	return "", errors.New("Secret backend unavailable; install a supported keyring or explicitly opt in with --backend file.")
}

// limitedOutput bounds memory without ever exposing backend output in errors.
type limitedOutput struct {
	data     []byte
	overflow bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	room := 65536 - len(b.data)
	if room < len(p) {
		b.overflow = true
		p = p[:room]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func secretCommand(action, ref, payload string) (string, error) {
	if _, e := backend("secret-service"); e != nil {
		return "", e
	}
	args := []string{action}
	if action == "store" {
		args = append(args, "--label="+service)
	}
	args = append(args, "service", service, "reference", ref)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", args...)
	if action == "store" {
		cmd.Stdin = strings.NewReader(payload)
	}
	out := &limitedOutput{}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if e := cmd.Run(); e != nil || out.overflow {
		return "", errors.New("Secret backend failed; check its availability and unlock state.")
	}
	return strings.TrimRight(string(out.data), "\r\n"), nil
}
func secretSet(p Profile, token string) error {
	if p.SecretBackend == "file" {
		dir := filepath.Join(ConfigHome(), "secrets")
		if e := SecureDirectory(dir); e != nil {
			return e
		}
		return AtomicWriteJSON(filepath.Join(dir, p.SecretRef+".json"), map[string]string{"token": token})
	}
	_, e := secretCommand("store", p.SecretRef, token)
	return e
}
func GetToken(id string) (string, error) {
	p, e := GetProfile(id)
	if e != nil {
		return "", e
	}
	if e = EnsureNotExpired(p); e != nil {
		return "", e
	}
	var token string
	if p.SecretBackend == "file" {
		dir := filepath.Join(ConfigHome(), "secrets")
		if e = private(dir, true); e != nil {
			return "", e
		}
		var raw map[string]any
		if e = ReadJSON(filepath.Join(dir, p.SecretRef+".json"), &raw, true); e != nil {
			return "", e
		}
		if len(raw) != 1 {
			return "", errors.New("Malformed secret file.")
		}
		v, ok := raw["token"].(string)
		if !ok {
			return "", errors.New("Malformed secret file.")
		}
		token = v
	} else {
		token, e = secretCommand("lookup", p.SecretRef, "")
		if e != nil {
			return "", e
		}
	}
	if e = validateToken(token); e != nil {
		return "", e
	}
	return token, nil
}
func secretRemove(p Profile) error {
	if p.SecretBackend == "file" {
		dir := filepath.Join(ConfigHome(), "secrets")
		if e := private(dir, true); e != nil {
			return e
		}
		path := filepath.Join(dir, p.SecretRef+".json")
		if !exists(path) {
			return nil
		}
		if e := private(path, false); e != nil {
			return e
		}
		if e := os.Remove(path); e != nil {
			return errors.New("Cannot remove local secret.")
		}
		return nil
	}
	_, e := secretCommand("clear", p.SecretRef, "")
	return e
}
func newRef() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", errors.New("Cannot create secret reference.")
	}
	return hex.EncodeToString(b), nil
}
func AddProfile(p Profile, token, backendName string) (Profile, error) {
	if e := ValidateProfileID(p.ID); e != nil {
		return Profile{}, e
	}
	if e := validateToken(token); e != nil {
		return Profile{}, e
	}
	var e error
	p.SecretBackend, e = backend(backendName)
	if e != nil {
		return Profile{}, e
	}
	p.SecretRef, e = newRef()
	if e != nil {
		return Profile{}, e
	}
	p.Verification = "unverified"
	if e = validateMetadata(p.ID, p); e != nil {
		return Profile{}, e
	}
	unlock, e := registryLock()
	if e != nil {
		return Profile{}, e
	}
	defer unlock()
	r, e := LoadRegistry()
	if e != nil {
		return Profile{}, e
	}
	if _, ok := r.Profiles[p.ID]; ok {
		return Profile{}, errors.New("Profile already exists; use profiles renew.")
	}
	if e = secretSet(p, token); e != nil {
		return Profile{}, e
	}
	r.Profiles[p.ID] = p
	if e = AtomicWriteJSON(filepath.Join(ConfigHome(), "profiles.json"), r); e != nil {
		_ = secretRemove(p)
		return Profile{}, e
	}
	return p, nil
}
func RenewProfile(id, token string) (Profile, error) {
	if e := ValidateProfileID(id); e != nil {
		return Profile{}, e
	}
	if e := validateToken(token); e != nil {
		return Profile{}, e
	}
	unlock, e := registryLock()
	if e != nil {
		return Profile{}, e
	}
	defer unlock()
	r, e := LoadRegistry()
	if e != nil {
		return Profile{}, e
	}
	p, ok := r.Profiles[id]
	if !ok {
		return Profile{}, errors.New("Unknown profile: " + id)
	}
	old := p
	p.SecretRef, e = newRef()
	if e != nil {
		return Profile{}, e
	}
	p.ExpiresAt = nil
	if e = secretSet(p, token); e != nil {
		return Profile{}, e
	}
	r.Profiles[id] = p
	if e = AtomicWriteJSON(filepath.Join(ConfigHome(), "profiles.json"), r); e != nil {
		_ = secretRemove(p)
		return Profile{}, e
	}
	if e = secretRemove(old); e != nil {
		return p, e
	}
	return p, nil
}
func RemoveProfile(id string) error {
	if e := ValidateProfileID(id); e != nil {
		return e
	}
	unlock, e := registryLock()
	if e != nil {
		return e
	}
	defer unlock()
	r, e := LoadRegistry()
	if e != nil {
		return e
	}
	p, ok := r.Profiles[id]
	if !ok {
		return errors.New("Unknown profile: " + id)
	}
	delete(r.Profiles, id)
	if e = AtomicWriteJSON(filepath.Join(ConfigHome(), "profiles.json"), r); e != nil {
		return e
	}
	return secretRemove(p)
}
