package cli

import "encoding/json"

type helpParams struct {
	Command string `json:"command"`
	Search  string `json:"search"`
}

// commandHelp stores help text for a command.
type commandHelp struct {
	Description string               `json:"description"`
	Usage       string               `json:"usage,omitempty"`
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string    `json:"exit_codes,omitempty"`
	Examples    []string             `json:"examples,omitempty"`
}

type paramHelp struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// HelpListData is the data payload for help (list all commands).
type HelpListData struct {
	Commands []HelpListEntry `json:"commands"`
}

// HelpListEntry is one command in the help list.
type HelpListEntry struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// HelpCommandData is the data payload for help (single command info).
type HelpCommandData struct {
	Command     string               `json:"command"`
	Description string               `json:"description"`
	Usage       string               `json:"usage,omitempty"`
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string    `json:"exit_codes,omitempty"`
	Examples    []string             `json:"examples,omitempty"`
}

// SearchFunc is a function that searches rules/checks by query string.
type SearchFunc func(query string) HelpSearchData

func NewHelpHandler(commands func() []string, searchFn SearchFunc) Handler {
	registry := map[string]commandHelp{
		"check": {
			Description: "Scan files, match rules, evaluate, return findings. Positional sugar: `md check <path>` = `md check '{\"scope\":\"<path>\"}'` (path must exist). skill_tree runs the embedded wikilink-integrity pack over a shipped skill directory — config-less, no meridian.yaml needed. strict promotes warn findings to exit 1 (default from meridian.yaml `strict:`; per-run override here is the escape hatch — error findings fail in both modes)",
			Usage:       "md check <path>  |  md check '{\"scope\":\"<path>\", ...}'",
			Params: map[string]paramHelp{
				"scope":      {Type: "string", Required: false},
				"skill_tree": {Type: "string", Required: false},
				"strict":     {Type: "bool", Required: false},
				"no_cache":   {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "1": "error findings (warn too when strict)", "2": "error (or MD_CACHE_VERIFY=1 divergence)"},
			Examples: []string{
				`md check wiki/`,
				`md check '{"scope":"wiki/","strict":true}'`,
				`md check '{"skill_tree":"skills/my-skill"}'`,
			},
		},
		"rules ls": {
			Description: "List loaded rules",
			Usage:       "md rules ls  |  md rules ls '{\"profile\":\"<name>\"}'",
			Params: map[string]paramHelp{
				"profile": {Type: "string", Required: false},
			},
		},
		"rules check": {
			Description: "Detect rule overlaps and conflicts",
			Usage:       "md rules check  |  md rules check '{\"profile\":\"<name>\"}'",
			Params: map[string]paramHelp{
				"profile": {Type: "string", Required: false},
			},
		},
		"debug": {
			Description: "Deep inspection of one rule",
			Usage:       "md debug '{\"rule\":\"<rule-id>\"}'",
			Params: map[string]paramHelp{
				"rule":  {Type: "string", Required: true},
				"scope": {Type: "string", Required: false},
			},
			Examples: []string{
				`md debug '{"rule":"broken-wikilink"}'`,
				`md debug '{"rule":"broken-wikilink","scope":"wiki/"}'`,
			},
		},
		"help": {
			Description: "Queryable help: list commands, detail one command, or search rules/checks",
			Usage:       "md help  |  md help '{\"command\":\"<cmd>\"}'  |  md help '{\"search\":\"<query>\"}'",
			Params: map[string]paramHelp{
				"command": {Type: "string", Required: false},
				"search":  {Type: "string", Required: false},
			},
			Examples: []string{
				`md help '{"command":"check"}'`,
				`md help '{"search":"wikilink"}'`,
			},
		},
		"version": {
			Description: "Show version information",
			Usage:       "md version",
		},
		"debt": {
			Description: "List incorporation debt (wiki/sources flagged do/incorporate, not yet incorporated)",
			Usage:       "md debt",
		},
		"cache stats": {
			Description: "Inventory this scan root's persistent fact cache: resolved path, accumulated version dirs, shard-file count, cached-document count, and total bytes. Read-only",
			Usage:       "md cache stats",
		},
		"cache clean": {
			Description: "Remove this scan root's entire persistent fact cache — every accumulated version dir (Decision 10: clean removes all versions; there is no auto-prune). Reports entries and bytes removed. Refuses unless the resolved path is a single roothash segment under UserCacheDir/meridian",
			Usage:       "md cache clean",
			ExitCodes:   map[string]string{"0": "removed (or nothing to remove)", "2": "refused unsafe path or removal error"},
		},
		"llm-wiki check": {
			Description: "Environment doctor for the llm-wiki system: verifies CCC_LLM_WIKI_PATH / CCC_LLM_WIKI_REPOS_ROOT and that every cataloged repo (sources/git/<slug>/) resolves at <root>/<slug> with the right git identity. Failures point at skill-shipped setup references; absent repos are a state, not a failure",
			Usage:       "md llm-wiki check  |  md llm-wiki check '{\"setup_dir\":\"<dir>\"}'",
			Params: map[string]paramHelp{
				"setup_dir": {Type: "string", Required: false},
				"format":    {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "environment healthy", "1": "check failures (setup refs named)", "2": "tool failure"},
		},
		"domains tree": {
			Description: "Show domain hierarchy from scanned wiki",
			Usage:       "md domains tree  |  md domains tree '{\"scope\":\"<path>\"}'",
			Params: map[string]paramHelp{
				"scope": {Type: "string", Required: false},
			},
		},
		"domains show": {
			Description: "Detail for one domain prefix",
			Usage:       "md domains show '{\"prefix\":\"<domain-prefix>\"}'",
			Params: map[string]paramHelp{
				"prefix": {Type: "string", Required: true},
				"scope":  {Type: "string", Required: false},
			},
			Examples: []string{
				`md domains show '{"prefix":"lang"}'`,
			},
		},
		"fix": {
			Description: "Auto-fix frontmatter violations",
			Usage:       "md fix '{\"scope\":\"<path>\",\"rules\":[\"<rule-id>\"],\"dry-run\":true}' (all optional)",
			Params: map[string]paramHelp{
				"scope":   {Type: "string", Required: false},
				"rules":   {Type: "array", Required: false},
				"dry-run": {Type: "bool", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "2": "error"},
			Examples: []string{
				`md fix '{"dry-run":true}'`,
				`md fix '{"scope":"wiki/","rules":["created"]}'`,
			},
		},
		"mv": {
			Description: "Move/rename files, update frontmatter domains",
			Usage:       "md mv '{\"source\":\"<from.md>\",\"dest\":\"<to.md>\"}'",
			Params: map[string]paramHelp{
				"source":  {Type: "string", Required: true},
				"dest":    {Type: "string", Required: true},
				"dry-run": {Type: "bool", Required: false},
			},
			Examples: []string{
				`md mv '{"source":"wiki/old.md","dest":"wiki/new.md","dry-run":true}'`,
			},
		},
		"run": {
			Description: "Execute frontmatter-addressed task blocks (md-<name> keys → ^id fences); format json captures task stdout/stderr into the envelope, text streams it live. timeout bounds each task's wall clock (Go duration, e.g. \"30s\") — at the deadline the process group is killed and the task reports exit 124. record:true writes each task's outcome + output to a sidecar run record (<stem>.runs.md, block-addressable per task as ^<task>) without ever mutating the source doc; response carries record_path. inherit:true resolves any requested task the file does not itself declare by walking its ancestor blurb pages (the page's own directory blurb <DIR>/<UPPER>.md first, up to LLM_WIKI.md at the git toplevel) — nearest ancestor wins, a leaf-defined task replaces the inherited one wholesale — and projects MD_PARAM_PAGE = the file's repo-relative path into every task's env so a shared ^check/^apply block can locate the page it runs against. Any OTHER top-level key is a named task param: it must be declared by a requested task's md-<task>-params contract (undeclared keys fail loud regardless of value) and reaches the task as MD_PARAM_<UPPER> in its env — string/number verbatim, true → \"1\", false → omitted (opt-in: false and absent are the same env state). A param value of null or empty string fails loud (a param with no value is a mistake, not the default — omit the key to use the default)",
			Usage:       "md run '{\"file\":\"<doc.md>\",\"name\":\"<task>\"}'  |  md run '{\"file\":\"<doc.md>\",\"list\":true}'",
			Params: map[string]paramHelp{
				"file":       {Type: "string", Required: true},
				"name":       {Type: "string|array", Required: false},
				"args":       {Type: "array", Required: false},
				"list":       {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
				"timeout":    {Type: "string", Required: false},
				"record":     {Type: "bool", Required: false},
				"inherit":    {Type: "bool", Required: false},
				"<declared>": {Type: "string|number|bool", Required: false},
			},
			ExitCodes: map[string]string{"0": "all tasks succeeded", "1": "a task exited non-zero (124 = timed out)", "2": "resolution or tool failure"},
			Examples: []string{
				`md run '{"file":"tasks.md","list":true}'`,
				`md run '{"file":"tasks.md","name":"build","timeout":"30s"}'`,
				`md run '{"file":"tasks.md","name":["lint","test"],"record":true}'`,
				`md run '{"file":"health/asset-normalization.md","name":"sweep","include_untracked":true}'`,
			},
		},
		"encode": {
			Description: "The ONE cross-wiki reference encoder (C24 canon): (slug, path[, fragment]) → canonical obsidian://open (no fragment) or advanced-uri (heading/^block) navigation URI; form nav = [display](uri) with caller display, form citation = [wiki://slug/path[@commit]](uri). parse extracts the triple from either grammar (strict decode: non-canonical encoding is flagged, never normalized). Config-less, pure grammar",
			Usage:       "md encode '{\"slug\":\"<wiki-slug>\",\"path\":\"<note-path>\"}'  |  md encode '{\"parse\":\"<encoded-ref>\"}'",
			Params: map[string]paramHelp{
				"slug":     {Type: "string", Required: false},
				"path":     {Type: "string", Required: false},
				"fragment": {Type: "string", Required: false},
				"commit":   {Type: "string", Required: false},
				"display":  {Type: "string", Required: false},
				"form":     {Type: "string", Required: false},
				"parse":    {Type: "string", Required: false},
				"format":   {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "encoded/parsed", "2": "invalid params or unrecognized grammar"},
			Examples: []string{
				`md encode '{"slug":"locus","path":"wiki/meridian/development.md"}'`,
				`md encode '{"slug":"locus","path":"wiki/meridian/development.md","fragment":"Testing","form":"citation"}'`,
			},
		},
		"pipe": {
			Description: "Unavailable: every invocation is refused with the named error \"pipe: unavailable\"",
			Usage:       "md pipe",
			ExitCodes: map[string]string{
				"2": "pipe: unavailable",
			},
		},
		"read": {
			Description: "Read vault-addressed content: path, [[note]], [[note#Heading]], or [[note#^block]]; text mode prints verification metadata (base, matches, warnings) to stderr, stdout stays pure content. With embeds:true, ![[...]] embeds are recursively inlined (frontmatter stripped from whole-note embeds). With strip-frontmatter:true, the matched file's own frontmatter is dropped — returns the deployable body",
			Usage:       "md read '{\"target\":\"<path | [[note]] | [[note#Heading]] | [[note#^block]]>\"}'",
			Params: map[string]paramHelp{
				"target":            {Type: "string", Required: true},
				"expect-unique":     {Type: "bool", Required: false},
				"expect-cwd":        {Type: "string", Required: false},
				"embeds":            {Type: "bool", Required: false},
				"strip-frontmatter": {Type: "bool", Required: false},
				"format":            {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "content resolved", "2": "not found, ambiguous (expect-unique/embed), or wrong cwd"},
			Examples: []string{
				`md read '{"target":"[[development#Testing]]"}'`,
				`md read '{"target":"wiki/meridian/development.md","strip-frontmatter":true}'`,
			},
		},
		"skill render": {
			Description: "Render a skill folder the way a harness does at load time: read <skill>/SKILL.md, strip frontmatter, execute both embedded-command styles in document order — fenced ```! blocks (whole fence replaced by merged stdout+stderr; a non-zero exit still inlines the output, the block's own failure line is the signal) and inline !`cmd` pre-resolution directives (stdout replaces on exit 0, stderr discarded; on failure the literal directive remains so fallback prose engages). Doc examples written as `` !`cmd` `` never execute. Env: caller's cwd, skill bin/ prepended to PATH, CCC_SKILL_DIR defaulted to the skill's parent dir (env wins). timeout bounds each directive (process group killed, exit 124). Text mode: rendered body on stdout, receipts on stderr; directive failures are warnings, never exit 1 — render is the load path",
			Usage:       "md skill render <skill-dir>  |  md skill render '{\"skill\":\"<skill-dir>\"}'",
			Params: map[string]paramHelp{
				"skill":   {Type: "string", Required: true},
				"timeout": {Type: "string", Required: false},
				"format":  {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "rendered (directive failures are warnings)", "2": "no SKILL.md, invalid params, or spawn failure"},
			Examples: []string{
				`md skill render skills/llm-wiki`,
				`md skill render '{"skill":"skills/llm-wiki","timeout":"30s"}'`,
			},
		},
		"watch": {
			Description: "Start filesystem watcher daemon",
			Usage:       "md watch",
		},
		"status": {
			Description: "Query running watch daemon status",
			Usage:       "md status",
		},
		"schema": {
			Description: "Print the effective frontmatter schema: contract defaults merged with the nearest SCHEMA.md overlay (searched from cwd up to git toplevel, or scan.root when config is loaded)",
			Usage:       "md schema",
			Params: map[string]paramHelp{
				"format": {Type: "string", Required: false},
			},
		},
		"resolve": {
			Description: "Resolve wikilinks into slice nodes and report the merkle-v1 chain hash of each node's embed closure (the attestation chain's input-hash semantics). hash mode (default) folds embeds into each node's hash, never expands reference links, and fails closed — an unresolved ref, dangling anchor, or node-cap overflow is an error finding, never a guessed hash; read mode expands reference links as child nodes up to depth and warns-and-continues. Exactly one of links or page. Config-gated: resolution needs the corpus index built from the scan root",
			Usage:       "md resolve '{\"page\":\"<page | [[note]]>\"}'  |  md resolve '{\"links\":[\"[[a]]\",\"[[b]]\"]}'",
			Params: map[string]paramHelp{
				"links":     {Type: "array", Required: false},
				"page":      {Type: "string", Required: false},
				"mode":      {Type: "string", Required: false},
				"depth":     {Type: "number", Required: false},
				"content":   {Type: "bool", Required: false},
				"max_nodes": {Type: "number", Required: false},
				"format":    {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "resolved (read mode, or clean hash)", "1": "hash-mode failure (unresolved ref, dangling anchor, node-cap overflow)", "2": "invalid params or scan error"},
			Examples: []string{
				`md resolve '{"page":"wiki/meridian/development.md"}'`,
				`md resolve '{"links":["[[development#Testing]]"],"mode":"read","depth":2}'`,
			},
		},
		"toc": {
			Description: "Render a document's shape — its heading table with per-section words, sec_rev, byte spans and line ranges — through the body engine. Config-free like read: a whole-file shape query, no meridian.yaml, no #fragment (use md read for one section)",
			Usage:       "md toc <path>  |  md toc '{\"target\":\"<path | [[note]]>\"}'",
			Params: map[string]paramHelp{
				"target": {Type: "string", Required: true},
				"format": {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "shape rendered", "2": "not found, ambiguous, or a #fragment target"},
			Examples: []string{
				`md toc wiki/meridian/development.md`,
				`md toc '{"target":"[[development]]"}'`,
			},
		},
		"append": {
			Description: "Add content to the tail of a section through the ONE write path (body.Splice, append rung): anchor-free and rev-free, with a 10-minute content-hash dedupe absorbing an at-least-once retry as a no-op ack. The actor is the invoking session (never a flag). Single: target file#Section (or target + at) + content; batch: edits[] (each with its own at) and/or properties, all in one atomic splice",
			Usage:       "md append '{\"target\":\"<file#Section>\",\"content\":\"<text>\"}'",
			Params: map[string]paramHelp{
				"target":     {Type: "string", Required: true},
				"at":         {Type: "string", Required: false},
				"content":    {Type: "string", Required: false},
				"edits":      {Type: "array", Required: false},
				"properties": {Type: "object", Required: false},
				"force":      {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "appended (or dedupe no-op)", "2": "invalid params, target not found, or write conflict"},
			Examples: []string{
				`md append '{"target":"notes.md#Log","content":"- new entry"}'`,
				`md append '{"target":"notes.md","at":"Log","content":"- new entry"}'`,
			},
		},
		"edit-section": {
			Description: "Replace an exact old span with new inside a section, guarded by the section's hash (sec_rev) as a compare-and-swap, through the ONE write path (body.Splice, replace rung). Omitting new is never an empty replacement — deleting the old span must be explicit (new:\"\"). On a CAS conflict the refusal carries the section's current content + fresh sec_rev for a one-round-trip retry. Single: target + old + new; batch: edits[] and/or properties in one atomic splice",
			Usage:       "md edit-section '{\"target\":\"<file#Section>\",\"old\":\"<bytes>\",\"new\":\"<bytes>\"}'",
			Params: map[string]paramHelp{
				"target":     {Type: "string", Required: true},
				"at":         {Type: "string", Required: false},
				"hash":       {Type: "string", Required: false},
				"old":        {Type: "string", Required: false},
				"new":        {Type: "string", Required: false},
				"all":        {Type: "bool", Required: false},
				"edits":      {Type: "array", Required: false},
				"properties": {Type: "object", Required: false},
				"force":      {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "replaced", "2": "invalid params, no match, or CAS conflict"},
			Examples: []string{
				`md edit-section '{"target":"notes.md#Log","old":"draft","new":"final"}'`,
			},
		},
		"set-prop": {
			Description: "Set one or more frontmatter properties through the ONE write path (body.Splice, property plane), so a property write gets the same lock, journal, atomicity, and authorization as a body write — one flock, one rev bump, one journal entry. Writes frontmatter, not a section — no #fragment. The actor is the invoking session (never a flag)",
			Usage:       "md set-prop '{\"target\":\"<file | [[note]]>\",\"properties\":{\"<key>\":\"<value>\"}}'",
			Params: map[string]paramHelp{
				"target":     {Type: "string", Required: true},
				"properties": {Type: "object", Required: true},
				"force":      {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "properties set", "2": "invalid params, target not found, or write conflict"},
			Examples: []string{
				`md set-prop '{"target":"notes.md","properties":{"status":"done"}}'`,
			},
		},
		"def check": {
			Description: "Validate a record against its resolved def (session → preset → builtin cascade, or an explicit defs layer list) and surface the tri-state verdict + findings. A malformed or missing def fails closed: findings only, no verdict, never writes. Config-free — the cascade resolves from the record's own directory upward plus $UCC_HOME/defs",
			Usage:       "md def check <path>  |  md def check '{\"target\":\"<path>\"}'",
			Params: map[string]paramHelp{
				"target": {Type: "string", Required: true},
				"defs":   {Type: "array", Required: false},
				"format": {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "1": "error findings", "2": "invalid params or unreadable record"},
			Examples: []string{
				`md def check agents/7533cd60.md`,
			},
		},
		"def fix": {
			Description: "Repair a record against its def — stamp derived fields, fill default tags, apply legacy marks — through ONE body.Splice batch (authorization applies; fixing another agent's file is refused naming the owner). Check-mostly in v1: missing sections are reported, never scaffolded. A malformed or missing def fails closed (findings only, nothing written); idempotent — a second run plans nothing",
			Usage:       "md def fix <path>  |  md def fix '{\"target\":\"<path>\"}'",
			Params: map[string]paramHelp{
				"target": {Type: "string", Required: true},
				"defs":   {Type: "array", Required: false},
				"format": {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "fixed or already clean", "1": "reported findings", "2": "invalid params or unreadable record"},
			Examples: []string{
				`md def fix agents/7533cd60.md`,
			},
		},
		"def census": {
			Description: "Fleet-WARN census over a directory tree: warn counts per rule, off-suggest vocabulary accretion, populated legacy Todo files, and per-actor forced-warning stats from the journals. Reports only — never writes, never rejects. root defaults to cwd",
			Usage:       "md def census [<root>]  |  md def census '{\"root\":\"<dir>\"}'",
			Params: map[string]paramHelp{
				"root":   {Type: "string", Required: false},
				"defs":   {Type: "array", Required: false},
				"format": {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "census reported"},
			Examples: []string{
				`md def census`,
				`md def census wiki/`,
			},
		},
		// The three chain/attest verbs: catalog one-liner is the mechanism (this
		// card); the full --help body (usage/params/exit_codes/examples) is
		// spec-verbatim, owned by the surface-spec verb card. The one-liner keeps
		// the catalog honest and answers `md <verb> --help` today; the body
		// enriches it in place under the shared dispatcher mutex.
		"attest": {
			Description: "Mint or refresh a v1 attestation receipt for a page — the strict sole receipt writer: check gate, input + procedure hashing, four-case idempotency (an unchanged page reports a no-op, never a fresh mint), CAS + ancestry guards, atomic working-tree write (never git add/commit/push). Exactly one of page or scope. Config-gated: hashing resolves against the corpus index built from the scan root",
			Usage:       "md attest <page>  |  md attest '{\"page\":\"<page>\"}'",
			Params: map[string]paramHelp{
				"page":          {Type: "string", Required: false},
				"scope":         {Type: "string", Required: false},
				"dry_run":       {Type: "bool", Required: false},
				"verdict":       {Type: "string", Required: false},
				"commit":        {Type: "string", Required: false},
				"bulk_reattest": {Type: "object", Required: false},
				"format":        {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "attested (or unchanged no-op)", "1": "a page failed or was refused", "2": "invalid params or tool failure"},
			Examples: []string{
				`md attest domains/correct-context/substrate.md`,
				`md attest '{"page":"domains/correct-context/substrate.md"}'`,
			},
		},
		"chain promote": {
			Description: "Scaffold an effect page's inputs: chain from its draws-from provenance — resolve and hash each entry. format:json emits the ready-to-paste ^inputs scaffold with no write; {\"write\":true} persists it to pages with no chain yet (never merges into an existing chain — that is md chain declare). Exactly one of page or scope. Config-gated like attest",
			Usage:       "md chain promote '{\"page\":\"<page>\"}'",
			Params: map[string]paramHelp{
				"page":    {Type: "string", Required: false},
				"scope":   {Type: "string", Required: false},
				"write":   {Type: "bool", Required: false},
				"dry_run": {Type: "bool", Required: false},
				"format":  {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "scaffolded (dead/ambiguous draws-from are warnings)", "2": "invalid params or tool failure"},
			Examples: []string{
				`md chain promote '{"page":"effects/skills/build-correct-context.md"}'`,
				`md chain promote '{"page":"effects/skills/build-correct-context.md","write":true}'`,
			},
		},
		"chain declare": {
			Description: "Declare explicit draw edges page→each draws-from selector and merge them into the page's ^inputs chain (pure-writer composition) — the merge path chain promote refuses. Computes each new edge's hash and splices it into an existing chain. Config-gated like attest",
			Usage:       "md chain declare '{\"page\":\"<page>\",\"draws-from\":[\"<selector>\"]}'",
			Params: map[string]paramHelp{
				"page":       {Type: "string", Required: true},
				"draws-from": {Type: "array", Required: true},
				"dry_run":    {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "declared (dead/ambiguous selectors are warnings)", "2": "invalid params or tool failure"},
			Examples: []string{
				`md chain declare '{"page":"results/report.md","draws-from":["[[substrate#^b1]]","6becbd2c#seq-234"]}'`,
			},
		},
	}

	return func(req *Request) *Response {
		var p helpParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return ErrorResponse(ErrInvalidParams, "invalid help params: "+err.Error())
			}
		}

		// Specific command help
		if p.Command != "" {
			info, ok := registry[p.Command]
			if !ok {
				return ErrorResponse(ErrInvalidParams, "unknown command: "+p.Command)
			}
			return &Response{
				Version: ResponseVersion,
				Data: HelpCommandData{
					Command:     p.Command,
					Description: info.Description,
					Usage:       info.Usage,
					Params:      info.Params,
					ExitCodes:   info.ExitCodes,
					Examples:    info.Examples,
				},
			}
		}

		// Search — delegate to search function
		if p.Search != "" {
			if searchFn != nil {
				return &Response{
					Version: ResponseVersion,
					Data:    searchFn(p.Search),
				}
			}
			return &Response{
				Version: ResponseVersion,
				Data:    HelpSearchData{Results: []SearchResult{}},
			}
		}

		// List all commands
		cmds := commands()
		cmdList := make([]HelpListEntry, 0, len(cmds))
		for _, name := range cmds {
			desc := ""
			if info, ok := registry[name]; ok {
				desc = info.Description
			}
			cmdList = append(cmdList, HelpListEntry{
				Command:     name,
				Description: desc,
			})
		}

		return &Response{
			Version: ResponseVersion,
			Data:    HelpListData{Commands: cmdList},
		}
	}
}
