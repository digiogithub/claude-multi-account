//go:build linux

// Package core implements private profile storage and the token-free managed-run protocol.
package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
)

type Profile struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	Email          string  `json:"email"`
	Organization   string  `json:"organization"`
	OrganizationID string  `json:"organization_id"`
	SecretBackend  string  `json:"secret_backend"`
	SecretRef      string  `json:"secret_ref"`
	ExpiresAt      *string `json:"expires_at"`
	Verification   string  `json:"verification"`
}
type Registry struct {
	Version  int                `json:"version"`
	Profiles map[string]Profile `json:"profiles"`
}
type Memory struct {
	Mode string `json:"mode"`
}
type Switching struct {
	ConfirmContextTransfer bool `json:"confirmContextTransfer"`
}
type Project struct {
	Version         int       `json:"version"`
	Profile         *string   `json:"profile"`
	AllowedProfiles []string  `json:"allowedProfiles,omitempty"`
	Memory          Memory    `json:"memory"`
	Switching       Switching `json:"switching"`
}
type Run struct {
	Version     int    `json:"version"`
	LaunchID    string `json:"launch_id"`
	Profile     string `json:"profile"`
	ProjectRoot string `json:"project_root"`
}
type Session struct {
	LaunchID    string `json:"launch_id"`
	SessionID   string `json:"session_id"`
	Cwd         string `json:"cwd"`
	ProjectRoot string `json:"project_root"`
}
type SwitchRequest struct {
	LaunchID    string `json:"launch_id"`
	SessionID   string `json:"session_id"`
	Profile     string `json:"profile"`
	Scope       string `json:"scope"`
	ProjectRoot string `json:"project_root"`
	Confirmed   bool   `json:"confirmed"`
}

var errJSON = errors.New("Invalid or inaccessible JSON configuration/state.")

// parseJSON rejects duplicate keys, invalid types, trailing data and non-object roots.
func parseJSON(data []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, e := readValue(d)
	if e != nil {
		return nil, errJSON
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, errJSON
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errJSON
	}
	return m, nil
}
func readValue(d *json.Decoder) (any, error) {
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, e
			}
			s, ok := k.(string)
			if !ok {
				return nil, errJSON
			}
			if _, ok = m[s]; ok {
				return nil, errJSON
			}
			v, e := readValue(d)
			if e != nil {
				return nil, e
			}
			m[s] = v
		}
		if t, e := d.Token(); e != nil || t != json.Delim('}') {
			return nil, errJSON
		}
		return m, nil
	case '[':
		a := []any{}
		for d.More() {
			v, e := readValue(d)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		if t, e := d.Token(); e != nil || t != json.Delim(']') {
			return nil, errJSON
		}
		return a, nil
	}
	return nil, errJSON
}

// checkShape enforces exact JSON field spelling and primitive types, including null.
func checkShape(v any, t reflect.Type, optional bool) error {
	if t.Kind() == reflect.Pointer {
		if v == nil {
			return nil
		}
		return checkShape(v, t.Elem(), optional)
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return errJSON
		}
		known := map[string]bool{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			name := tag
			for j, c := range name {
				if c == ',' {
					name = name[:j]
					break
				}
			}
			known[name] = true
			value, present := m[name]
			if !present {
				if optional {
					continue
				}
				return errJSON
			}
			if e := checkShape(value, f.Type, false); e != nil {
				return e
			}
		}
		for k := range m {
			if !known[k] {
				return errJSON
			}
		}
	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return errJSON
		}
		for _, value := range m {
			if e := checkShape(value, t.Elem(), false); e != nil {
				return e
			}
		}
	case reflect.Slice:
		a, ok := v.([]any)
		if !ok {
			return errJSON
		}
		for _, value := range a {
			if e := checkShape(value, t.Elem(), false); e != nil {
				return e
			}
		}
	case reflect.String:
		if _, ok := v.(string); !ok {
			return errJSON
		}
	case reflect.Bool:
		if _, ok := v.(bool); !ok {
			return errJSON
		}
	case reflect.Int:
		n, ok := v.(json.Number)
		if !ok {
			return errJSON
		}
		if _, e := n.Int64(); e != nil {
			return errJSON
		}
	case reflect.Interface: // Generic map callers supply their own schema validation.
	default:
		return errJSON
	}
	return nil
}
func decodeTyped(data []byte, out any, optional bool) error {
	m, e := parseJSON(data)
	if e != nil {
		return e
	}
	t := reflect.TypeOf(out)
	if t == nil || t.Kind() != reflect.Pointer {
		return errJSON
	}
	if e = checkShape(m, t.Elem(), optional); e != nil {
		return e
	}
	if e = json.Unmarshal(data, out); e != nil {
		return errJSON
	}
	return nil
}
func (p *Project) UnmarshalJSON(data []byte) error {
	type plain Project
	result := plain(DefaultProject())
	if e := decodeTyped(data, &result, true); e != nil {
		return e
	}
	*p = Project(result)
	return validateProject(*p)
}
func (p Project) MarshalJSON() ([]byte, error) {
	// nil means absent. An explicitly empty list is a deny-all policy, not absent.
	type plain Project
	if p.AllowedProfiles == nil {
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		plain
		AllowedProfiles []string `json:"allowedProfiles"`
	}{plain(p), p.AllowedProfiles})
}
func (p *Profile) UnmarshalJSON(data []byte) error {
	type plain Profile
	var result plain
	if e := decodeTyped(data, &result, false); e != nil {
		return e
	}
	*p = Profile(result)
	return nil
}
func (r *Registry) UnmarshalJSON(data []byte) error {
	type plain Registry
	var result plain
	if e := decodeTyped(data, &result, false); e != nil {
		return e
	}
	*r = Registry(result)
	return nil
}
func (r *Run) UnmarshalJSON(data []byte) error {
	type plain Run
	var result plain
	if e := decodeTyped(data, &result, false); e != nil {
		return e
	}
	*r = Run(result)
	return nil
}
func (s *Session) UnmarshalJSON(data []byte) error {
	type plain Session
	var result plain
	if e := decodeTyped(data, &result, false); e != nil {
		return e
	}
	*s = Session(result)
	return nil
}
func (s *SwitchRequest) UnmarshalJSON(data []byte) error {
	type plain SwitchRequest
	var result plain
	if e := decodeTyped(data, &result, false); e != nil {
		return e
	}
	*s = SwitchRequest(result)
	return nil
}
