# Development

Linux ONLY, Go >= 1.23, standard library; Go tests with isolated temporary directories.
Do not access real credentials, run login, install on the machine, modify the
shell, or commit without authorization. Never print tokens or read
`.credentials.json`. Tests use fake backends and binaries, without network access.

From the repository root:

```sh
go test ./...
go test -race ./...
go vet ./...
make build
sh -n bin/cma && sh -n bin/claude-cma && sh -n plugin/bin/cma
claude plugin validate ./plugin
claude plugin validate ./.claude-plugin/marketplace.json
```

Claude validation is local; omit it and state why if unavailable.
No external linter/typechecker is required; use `gofmt` and `go vet`.
Do not add dependencies without need. Do not run `make install` during tests.

`make build` produces `dist/cma` for Linux. `bin/cma` and `bin/claude-cma` are POSIX
shims that run this single binary; the latter uses `cma launch --`.
`plugin/bin/cma` uses a stable executable via absolute `CMA_BINARY`, or
`dist/cma` from the full root. It never builds or installs from hooks.
The marketplace packages the full root: do not change the source to `./plugin` or
duplicate the runtime. `dist/` is not versioned: the user must build explicitly
or configure their stable installation; installing the plugin does not install the CLI.
`make install` is an explicit human action only and runs `./dist/cma install`;
the default stable destination is `~/.local/share/claude-multi-account/bin/cma`.
Shell integration requires an explicit `cma install --shell`.

Skills are manual-only and receive no secrets. JSON format version 1 is
compatible with previous records; Linux secret-service/file/config-dir backends only.

Two profile modes: `config-dir` is the default (own Claude directory per profile,
launched via `CLAUDE_CONFIG_DIR`, normal login done by the user, no token stored);
`token` is opt-in (`--mode token`, `CLAUDE_CODE_OAUTH_TOKEN`). Directories:
`CMA_ACCOUNTS_HOME` (default `~/.claude-accounts`) holds `shared/` (`CMA_SHARED_HOME`)
and `profiles/<id>`; entries are linked into each profile by absolute symlink and never
overwritten. Registry v1 is unchanged: a config-dir profile has
`secret_backend: "config-dir"`. Never open `.credentials.json` or `.claude.json`
(`Lstat` only). Tests must set `CMA_ACCOUNTS_HOME` to a temporary directory.
Previous documents in `docs/claude-multi-account/` are historical analyses/plans:
leave them unchanged.

## MANDATORY operating rules for AI agents

These rules are mandatory for every task, including small changes. Follow them
before shortcuts or habitual workflows. Repository-specific safety constraints
above remain in force.

### MANDATORY — Recover context before work

Before planning, editing, designing, or implementing, recover relevant prior
context in this order:

1. Search the knowledge base using `kb_search_documents` without a tags filter
   for the project, conventions, and related prior work. Use
   `hybrid_search_remembrances` when a combined search across stored documents,
   sessions, and indexed code is useful. If no relevant context exists, say so
   briefly and proceed.
2. Follow useful KB document links and backlinks with `kb_get_document` and
   `kb_related_documents`; do not rely only on another search. Use the
   unresolved-concepts listing when it helps locate missing documentation.
3. Use code-index/remembrance tools before blind source reads when exploring
   code: `code_get_symbols_overview`, `code_find_symbol`, `code_hybrid_search`,
   and `code_search_pattern`. Prefer indexed results for locating code, then
   read exact source as needed. If these tools are unavailable, use the closest
   available project search/read tools and state that fallback.
4. Use `recall` only when you already know roughly which short, durable fact or
   key you need; it is not a replacement for broad KB context search.

### MANDATORY — Research facts not established by the project

Do not guess about external or current facts needed for a task. For library,
framework, or API usage, first resolve the library with
`c7_resolve_library_id`, then consult current documentation with
`c7_get_library_docs`; prefer this over memory for third-party APIs. For general
unknowns, current information, errors, or release notes, use available web
search and `fetch` relevant sources, cross-checking load-bearing claims. For
frontend or web UI work, use the available browser tools (`browser_navigate`,
`browser_get_content`, `browser_evaluate`, `browser_click`, `browser_fill`,
`browser_screenshot`, `browser_console_logs`, and `browser_network`) to exercise
and verify the actual page. If a named tool is unavailable, use the closest
equivalent and state the limitation and fallback. Project tests must remain
network-free as specified above.

### MANDATORY — Plan non-trivial work

For work beyond a trivial change, write a phased plan before implementation;
make each phase independently verifiable and save the plan with
`kb_add_document` under a clear project plan path. Search for an existing plan
first; if it is unclear whether one applies, confirm with the user before
choosing a divergent approach. Update the plan as phases complete.

### MANDATORY — Make small changes and verify each increment

Implement in small, testable increments and run relevant tests or builds after
each increment before proceeding. Match surrounding naming, style, comments,
and project idioms. Add or update tests for new behavior in the project's
expected location. Do not claim completion without verification; report skipped
checks or failures plainly with evidence. From the repository root, use the
commands listed above when relevant, including `gofmt` and `go vet`; do not run
machine installation as part of verification.

### MANDATORY — Record every change in the knowledge base

After every modification, implementation, fix, or refactor—including a small
or one-line change—record a summary using `kb_add_document`. Use a clear path
under `claude-multi-account/changes/`, `claude-multi-account/fixes/`, or
`claude-multi-account/features/`, and update an existing related document
instead of making a duplicate. Record what changed, the concrete files and
symbols touched, why, and how it was verified. Link relevant plans, features,
or fixes in the document body with `[[wiki links]]` so the knowledge graph
retains the relationship. In plan mode, where edits are restricted to the plan,
defer this record until approved implementation changes are allowed; do not
skip it.

### MANDATORY — Use each memory tool for its intended purpose

Use `remember` and `recall` only for short, durable facts associated with a
known key. Use `remember` to upsert such a fact and `recall` when seeking one;
do not store long or structured material there. Use `kb_add_document` and
`kb_search_documents` for plans, analyses, design notes, ordered decisions,
references, and mandatory pre-task context searches.

### MANDATORY — Work and act responsibly

Use English for code, comments, and documentation. Parallelize independent work
when useful, without sacrificing context; give sub-agents clear, self-contained
instructions and needed context. Confirm before hard-to-reverse or outward-facing
actions unless durably authorized. Never violate the repository-specific safety
rules above, including restrictions on credentials, login, machine installation,
shell changes, network-dependent tests, and commits without authorization.
