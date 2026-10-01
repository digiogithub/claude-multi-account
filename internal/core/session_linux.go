//go:build linux

package core

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

func runAt(dir, launch string) (string, Run, error) {
	var r Run
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || !launchRE.MatchString(filepath.Base(dir)) {
		return "", r, errors.New("Invalid managed run directory or launch ID.")
	}
	base := filepath.Join(StateHome(), "runs")
	if e := private(StateHome(), true); e != nil {
		return "", r, e
	}
	if e := private(base, true); e != nil {
		return "", r, e
	}
	if filepath.Dir(dir) != base {
		return "", r, errors.New("Managed run directory is outside local state.")
	}
	if e := private(dir, true); e != nil {
		return "", r, e
	}
	if e := ReadJSON(filepath.Join(dir, "run.json"), &r, true); e != nil {
		return "", r, e
	}
	if r.Version != 1 {
		return "", r, errors.New("Invalid managed run state schema/version.")
	}
	if r.LaunchID != filepath.Base(dir) || (launch != "" && launch != r.LaunchID) {
		return "", r, errors.New("Managed launch identity mismatch.")
	}
	if e := ValidateProfileID(r.Profile); e != nil {
		return "", r, e
	}
	root, e := resolved(r.ProjectRoot)
	if e != nil || !filepath.IsAbs(r.ProjectRoot) || root != r.ProjectRoot {
		return "", r, errors.New("Invalid managed project root.")
	}
	return dir, r, nil
}
func ValidateRun() (string, Run, error) {
	if os.Getenv("CMA_MANAGED") != "1" {
		return "", Run{}, errors.New("This operation requires a managed claude-cma session.")
	}
	launch := os.Getenv("CMA_LAUNCH_ID")
	if !launchRE.MatchString(launch) {
		return "", Run{}, errors.New("Managed launch identity mismatch.")
	}
	dir, r, e := runAt(os.Getenv("CMA_RUN_DIR"), launch)
	if e != nil {
		return "", r, e
	}
	if os.Getenv("CMA_ACTIVE_PROFILE") != r.Profile || os.Getenv("CMA_PROJECT_ROOT") != r.ProjectRoot {
		return "", r, errors.New("Managed environment identity mismatch.")
	}
	return dir, r, nil
}
func sessionAt(dir string, r Run) (Session, error) {
	var s Session
	if e := ReadJSON(filepath.Join(dir, "session.json"), &s, true); e != nil {
		return s, e
	}
	if s.LaunchID != r.LaunchID || s.ProjectRoot != r.ProjectRoot {
		return s, errors.New("Registered session identity mismatch.")
	}
	if !sessionRE.MatchString(s.SessionID) {
		return s, errors.New("Invalid registered session ID.")
	}
	cwd, e := resolved(s.Cwd)
	if e != nil || !filepath.IsAbs(s.Cwd) || cwd != s.Cwd {
		return s, errors.New("Session working directory changed project; start a new managed launch.")
	}
	root, e := ProjectRoot(cwd)
	if e != nil || root != r.ProjectRoot {
		return s, errors.New("Session working directory changed project; start a new managed launch.")
	}
	return s, nil
}
func SessionInfo() (Session, error) {
	dir, r, e := ValidateRun()
	if e != nil {
		return Session{}, e
	}
	return sessionAt(dir, r)
}
func RegisterSession(payload Session) error {
	dir, r, e := ValidateRun()
	if e != nil {
		return e
	}
	if !sessionRE.MatchString(payload.SessionID) {
		return errors.New("SessionStart requires a valid exact session_id.")
	}
	if !filepath.IsAbs(payload.Cwd) {
		return errors.New("SessionStart requires an absolute cwd.")
	}
	cwd, e := resolved(payload.Cwd)
	if e != nil {
		return e
	}
	root, e := ProjectRoot(cwd)
	if e != nil || root != r.ProjectRoot {
		return errors.New("SessionStart project mismatch; no switch is permitted.")
	}
	return AtomicWriteJSON(filepath.Join(dir, "session.json"), Session{LaunchID: r.LaunchID, SessionID: payload.SessionID, Cwd: cwd, ProjectRoot: r.ProjectRoot})
}
func RequestSwitch(id, scope string, confirmed bool) error {
	dir, r, e := ValidateRun()
	if e != nil {
		return e
	}
	s, e := sessionAt(dir, r)
	if e != nil {
		return e
	}
	cwd, e := os.Getwd()
	if e != nil {
		return errors.New("Current directory is inaccessible.")
	}
	root, p, e := LoadProject(cwd)
	if e != nil {
		return e
	}
	if root != r.ProjectRoot {
		return errors.New("Current directory differs from managed project; switch refused.")
	}
	if scope != "session" && scope != "project" {
		return errors.New("Switch scope must be session or project.")
	}
	profile, e := GetProfile(id)
	if e != nil {
		return e
	}
	if e = EnsureNotExpired(profile); e != nil {
		return e
	}
	if e = CheckAllowed(p, id); e != nil {
		return e
	}
	if !confirmed {
		return errors.New("Resuming transfers conversation context; pass --confirm-context-transfer after consent.")
	}
	request := SwitchRequest{LaunchID: r.LaunchID, SessionID: s.SessionID, Profile: id, Scope: scope, ProjectRoot: r.ProjectRoot, Confirmed: true}
	if scope == "project" {
		p.Profile = &id
		if e = SaveProject(root, p); e != nil {
			return e
		}
	}
	return AtomicWriteJSON(filepath.Join(dir, "switch.json"), request)
}
func ReadSwitchRequest(dir string, expected Run) (*SwitchRequest, error) {
	dir, actual, e := runAt(dir, "")
	if e != nil {
		return nil, e
	}
	if actual != expected {
		return nil, errors.New("Managed run state changed unexpectedly.")
	}
	path := filepath.Join(dir, "switch.json")
	if !exists(path) {
		return nil, nil
	}
	var req SwitchRequest
	if e = ReadJSON(path, &req, true); e != nil {
		return nil, e
	}
	if !req.Confirmed || (req.Scope != "session" && req.Scope != "project") {
		return nil, errors.New("Invalid switch request.")
	}
	s, e := sessionAt(dir, actual)
	if e != nil {
		return nil, e
	}
	if req.LaunchID != actual.LaunchID || req.ProjectRoot != actual.ProjectRoot || req.SessionID != s.SessionID {
		return nil, errors.New("Switch request does not match this launch/session/project.")
	}
	p, e := GetProfile(req.Profile)
	if e != nil {
		return nil, e
	}
	if e = EnsureNotExpired(p); e != nil {
		return nil, e
	}
	root, project, e := LoadProject(actual.ProjectRoot)
	if e != nil {
		return nil, e
	}
	if root != actual.ProjectRoot {
		return nil, errors.New("Switch project identity changed.")
	}
	if e = CheckAllowed(project, req.Profile); e != nil {
		return nil, e
	}
	return &req, nil
}
func Status(cwd string) (map[string]any, error) {
	root, p, e := LoadProject(cwd)
	if e != nil {
		return nil, e
	}
	managed := os.Getenv("CMA_MANAGED") == "1"
	result := map[string]any{"project_root": root, "project": p, "managed": managed, "active_profile": nil, "session_id": nil, "profile": nil, "expiry_status": "unknown"}
	id := ""
	if p.Profile != nil {
		id = *p.Profile
	}
	if managed {
		dir, r, e := ValidateRun()
		if e != nil {
			return nil, e
		}
		if root != r.ProjectRoot {
			return nil, errors.New("Current project differs from managed session.")
		}
		result["active_profile"] = r.Profile
		id = r.Profile
		if exists(filepath.Join(dir, "session.json")) {
			s, e := sessionAt(dir, r)
			if e != nil {
				return nil, e
			}
			result["session_id"] = s.SessionID
		}
	}
	if id != "" {
		profile, e := GetProfile(id)
		if e != nil {
			return nil, e
		}
		result["profile"] = profile
		t, e := expiry(profile.ExpiresAt)
		if e != nil {
			return nil, e
		}
		if t != nil {
			result["expiry_status"] = "valid"
			if !t.After(time.Now()) {
				result["expiry_status"] = "expired"
			}
		}
	}
	return result, nil
}
