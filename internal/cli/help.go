//go:build linux

package cli

import "strings"

func help(args []string) (string, bool) {
	usages := map[string]string{
		"":                       "cma {profiles,project,status,switch,launch,install,uninstall} ...",
		"profiles":               "cma profiles {list,add,renew,remove} ...",
		"profiles list":          "cma profiles list [--json]",
		"profiles add":           "cma profiles add ID --label LABEL --email EMAIL --organization ORGANIZATION --organization-id ID [--expires-at DATE] [--backend {auto,secret-service,file}] [--token-stdin]",
		"profiles renew":         "cma profiles renew ID [--token-stdin]",
		"profiles remove":        "cma profiles remove ID",
		"project":                "cma project {init,use,memory} ...",
		"project init":           "cma project init [--profile ID]",
		"project use":            "cma project use ID",
		"project memory":         "cma project memory {shared,per-profile}",
		"status":                 "cma status [--json]",
		"switch":                 "cma switch ID [--scope {session,project}] [--confirm-context-transfer]",
		"launch":                 "cma launch [--profile ID] [-- CLAUDE_ARGS...]",
		"install":                "cma install [--target PATH] [--shell]",
		"uninstall":              "cma uninstall [--target PATH]",
		"internal":               "cma internal {session-start,session-info}",
		"internal session-start": "cma internal session-start",
		"internal session-info":  "cma internal session-info",
	}
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if arg != "--help" && arg != "-h" {
			continue
		}
		// Only command prefixes are help requests, never Claude's own remainder.
		prefix := strings.Join(args[:i], " ")
		if usage, ok := usages[prefix]; ok {
			return "usage: " + usage, true
		}
	}
	return "", false
}
