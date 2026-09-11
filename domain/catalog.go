// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	catalogComponent = "domain.catalog"
	// ManifestName is the catalog root manifest (Python MANIFEST_NAME).
	ManifestName = "manifest.json"
)

var lineageFileRe = regexp.MustCompile(`^chat_request_(?P<mode>.+)_(?P<base_id>(?:base|cus)-.+)\.json$`)

// LoadedCatalog is the catalog body after load + heal (Python LoadedCatalog).
type LoadedCatalog struct {
	Body                  map[string]any
	Path                  string
	Source                string
	ContractHash          string
	BaseID                string
	LineageID             string
	ResponseOutputSchema  map[string]any
	ResponseOutputExample map[string]any
}

// CatalogResolveOpts is fail-closed path resolution (Python resolve_catalog_path).
type CatalogResolveOpts struct {
	Mode       string
	Bucket     string
	Scope      string
	UserDir    string
	BundledDir string
	BaseID     string
}

// ScopeChatRequestsSubdir is beer-sample/_default → beer-sample__default.
func ScopeChatRequestsSubdir(bucket, scope string) string {
	tail := scope
	if strings.HasPrefix(scope, "_") {
		tail = scope[1:]
	}
	return bucket + "__" + tail
}

// ScopeKeyFromSubdir is the inverse of ScopeChatRequestsSubdir (bucket/_tail).
func ScopeKeyFromSubdir(name string) string {
	if !strings.Contains(name, "__") {
		return ""
	}
	bucket, tail, _ := strings.Cut(name, "__")
	scope := tail
	if tail != "" {
		scope = "_" + tail
	}
	return bucket + "/" + scope
}

// ChatRequestFilename is the write-side name (Python chat_request_filename).
func ChatRequestFilename(mode, baseID string) string {
	m := strings.TrimSpace(mode)
	if m == "" {
		m = "default"
	}
	bid := strings.TrimSpace(baseID)
	if bid != "" {
		return "chat_request_" + m + "_" + bid + ".json"
	}
	if m == "default" {
		return "chat_request_v2.json"
	}
	return "chat_request_" + m + "_v2.json"
}

func isSinglePathComponent(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.Contains(filepath.ToSlash(name), "/") {
		return false
	}
	return true
}

// CatalogFilenamesForMode is the listing-inverse of ModeFromFilename.
func CatalogFilenamesForMode(mode, baseID string) []string {
	bid := strings.TrimSpace(baseID)
	if bid != "" {
		return []string{ChatRequestFilename(mode, bid)}
	}
	m := strings.TrimSpace(mode)
	if m == "" {
		m = "default"
	}
	if m == "default" {
		return []string{"chat_request_v2.json", "chat_request.json"}
	}
	if !isSinglePathComponent(m) || strings.Contains(m, "..") {
		return nil
	}
	names := []string{
		"chat_request_" + m + "_v2.json",
		"chat_request_" + m + ".json",
		m + ".json",
	}
	out := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// ParseCatalogFilename parses chat_request_<mode>_<base-N|cus-*>.json.
func ParseCatalogFilename(name string) (mode, baseID string, ok bool) {
	stem := filepath.Base(name)
	m := lineageFileRe.FindStringSubmatch(stem)
	if m == nil {
		return "", "", false
	}
	idxMode := lineageFileRe.SubexpIndex("mode")
	idxBase := lineageFileRe.SubexpIndex("base_id")
	return m[idxMode], m[idxBase], true
}

// ModeFromFilename maps a catalog filename back to a mode key.
func ModeFromFilename(name string) string {
	if mode, _, ok := ParseCatalogFilename(name); ok {
		return mode
	}
	stem := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if stem == "chat_request" || stem == "chat_request_v2" {
		return "default"
	}
	if strings.HasPrefix(stem, "chat_request_") {
		mode := strings.TrimPrefix(stem, "chat_request_")
		mode = strings.TrimSuffix(mode, "_v2")
		return mode
	}
	return stem
}

// LineageBaseID returns _lineage.base_id when present and non-empty.
func LineageBaseID(doc map[string]any) string {
	if doc == nil {
		return ""
	}
	raw, ok := doc["_lineage"].(map[string]any)
	if !ok || raw == nil {
		return ""
	}
	got := strings.TrimSpace(asString(raw["base_id"]))
	if got == "" {
		return ""
	}
	return got
}

// DocumentBaseID prefers _lineage.base_id, then top-level base_id (mock kit).
func DocumentBaseID(doc map[string]any) string {
	if id := LineageBaseID(doc); id != "" {
		return id
	}
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(asString(doc["base_id"]))
}

// CheckLineage refuses silent lineage mismatch. Never invents stamps.
func CheckLineage(doc map[string]any, baseID string, requireLineage bool, pathName string) error {
	want := strings.TrimSpace(baseID)
	if want == "" {
		return nil
	}
	if !strings.HasPrefix(want, "base-") && !strings.HasPrefix(want, "cus-") {
		return NewCatalog(CodeCatalogLineageUnknown, catalogComponent,
			WithMessage("base_id must look like 'base-N' or 'cus-*', got "+baseID),
			WithDetails(map[string]any{"base_id": baseID}),
		)
	}
	got := LineageBaseID(doc)
	label := pathName
	if label == "" {
		label = "catalog"
	}
	if got != "" && got != want {
		return NewCatalog(CodeCatalogLineageUnknown, catalogComponent,
			WithMessage("lineage base_id '"+got+"' != requested '"+want+"' ("+label+")"),
			WithDetails(map[string]any{"requested": want, "got": got, "path": pathName}),
		)
	}
	if requireLineage && got == "" {
		return NewCatalog(CodeCatalogLineageUnknown, catalogComponent,
			WithMessage("catalog missing _lineage.base_id (refusing silent load): "+label),
			WithDetails(map[string]any{"requested": want, "path": pathName}),
		)
	}
	return nil
}

func isRegularFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode().IsRegular()
}

// ResolveCatalogPath returns the first existing file in locked order.
// 1. {user_dir}/{bucket}__{scope_tail}/ (+ min/)
// 2. {user_dir}/ top-level only — never sibling *__* rglob
// 3. {bundled_dir}/ (+ min/)
func ResolveCatalogPath(opts CatalogResolveOpts) (string, bool) {
	names := CatalogFilenamesForMode(opts.Mode, opts.BaseID)
	if len(names) == 0 {
		return "", false
	}
	var locations []string
	if opts.Bucket != "" && opts.Scope != "" {
		scopeDir := filepath.Join(opts.UserDir, ScopeChatRequestsSubdir(opts.Bucket, opts.Scope))
		locations = append(locations, scopeDir, filepath.Join(scopeDir, "min"))
	}
	locations = append(locations, opts.UserDir, filepath.Join(opts.UserDir, "min"))
	if opts.BundledDir != "" {
		locations = append(locations, opts.BundledDir, filepath.Join(opts.BundledDir, "min"))
	}
	for _, loc := range locations {
		for _, name := range names {
			p := filepath.Join(loc, name)
			if isRegularFile(p) {
				return p, true
			}
		}
	}
	return "", false
}

// ExtractScopeBrief pulls the live ## SCOPE BRIEF suffix.
func ExtractScopeBrief(chatReq map[string]any) string {
	if chatReq == nil {
		return ""
	}
	const marker = "## SCOPE BRIEF"
	content := ""
	if messages, ok := chatReq["messages"].([]any); ok && len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			content = asString(msg["content"])
		}
	}
	if idx := strings.Index(content, marker); idx >= 0 {
		return strings.TrimSpace(content[idx:])
	}
	if instr, ok := chatReq["instructions"].(map[string]any); ok && instr != nil {
		sp := asString(instr["system_prompt"])
		if idx := strings.Index(sp, marker); idx >= 0 {
			return strings.TrimSpace(sp[idx:])
		}
	}
	return ""
}

// MergeScopeBrief appends a live scope brief to system surfaces (hash-safe).
func MergeScopeBrief(chatReq map[string]any, brief string) map[string]any {
	if brief == "" || chatReq == nil {
		return chatReq
	}
	out, ok := cloneJSON(chatReq).(map[string]any)
	if !ok {
		return chatReq
	}
	stripped := strings.TrimSpace(brief)
	if messages, ok := out["messages"].([]any); ok && len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			current := asString(msg["content"])
			if !strings.Contains(current, "## SCOPE BRIEF") && !strings.Contains(current, "## MINI-SCHEMA") {
				msg["content"] = strings.TrimRight(current, trailingWS) + "\n\n" + stripped
			}
		}
	}
	if instr, ok := out["instructions"].(map[string]any); ok && instr != nil {
		sp := asString(instr["system_prompt"])
		if sp != "" && !strings.Contains(sp, "## SCOPE BRIEF") && !strings.Contains(sp, "## MINI-SCHEMA") {
			instr["system_prompt"] = strings.TrimRight(sp, trailingWS) + "\n\n" + stripped
			out["instructions"] = instr
		}
	}
	return out
}

// NormalizeChatRequestShape ensures a dict; leaves structure intact.
func NormalizeChatRequestShape(doc any) map[string]any {
	m, ok := doc.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	return m
}

// PrepareLoadedDocument normalizes + heals trailing-ws stamp drift.
func PrepareLoadedDocument(raw map[string]any) map[string]any {
	return HealTrailingWSStampDrift(NormalizeChatRequestShape(raw))
}

func absPath(p string) string {
	if p == "" {
		return p
	}
	got, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return got
}

func underDir(root, target string) (rel string, ok bool) {
	if root == "" || target == "" {
		return "", false
	}
	rel, err := filepath.Rel(absPath(root), absPath(target))
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// PathSourceLabel is a human-readable origin for LoadedCatalog.Source.
func PathSourceLabel(path, userDir, bundledDir string) string {
	if rel, ok := underDir(userDir, path); ok {
		slash := filepath.ToSlash(rel)
		parts := strings.Split(slash, "/")
		if len(parts) >= 2 && strings.Contains(parts[0], "__") {
			return "synced (" + slash + ")"
		}
		return "user_general (" + slash + ")"
	}
	if bundledDir != "" {
		if rel, ok := underDir(bundledDir, path); ok {
			return "bundled (" + filepath.ToSlash(rel) + ")"
		}
	}
	return "file (" + filepath.Base(path) + ")"
}

func verbList(doc map[string]any) []any {
	if doc == nil {
		return nil
	}
	raw := doc["verbs"]
	if _, ok := raw.([]any); !ok {
		raw = doc["tools"]
	}
	sl, _ := raw.([]any)
	return sl
}

func contractIDOf(doc map[string]any) string {
	if doc == nil {
		return ""
	}
	cb, _ := doc["contract"].(map[string]any)
	if cb == nil {
		return ""
	}
	return asString(cb["id"])
}

// CatalogEntryStats is light list/info stats. Never invents a production stamp.
func CatalogEntryStats(path string, body map[string]any) map[string]string {
	mode, parsedBase, parsedOK := ParseCatalogFilename(filepath.Base(path))
	_ = mode
	doc := body
	if doc == nil {
		raw, err := os.ReadFile(path)
		if err == nil {
			parsed := map[string]any{}
			if json.Unmarshal(raw, &parsed) == nil {
				doc = parsed
			}
		}
	}
	verbs := verbList(doc)
	stamped := ""
	if doc != nil {
		stamped = ExtractStampedHash(doc)
	}
	lineage := LineageBaseID(doc)
	baseID := lineage
	if parsedOK && parsedBase != "" {
		baseID = parsedBase
	}
	if baseID == "" {
		baseID = DocumentBaseID(doc)
	}
	stampPresent := "false"
	if stamped != "" {
		stampPresent = "true"
	}
	return map[string]string{
		"base_id":       baseID,
		"lineage_id":    lineage,
		"stamp_present": stampPresent,
		"verb_count":    itoa(len(verbs)),
		"contract_id":   contractIDOf(doc),
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func jsonFilesSorted(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func subdirsSorted(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// CatalogListEntry is one list_catalog_entries row (Python dict[str,str]).
type CatalogListEntry struct {
	Mode         string
	File         string
	Source       string
	Origin       string
	Path         string
	BaseID       string
	LineageID    string
	StampPresent string
	VerbCount    string
}

// Map is the Python list-entry shape.
func (e CatalogListEntry) Map() map[string]string {
	return map[string]string{
		"mode":          e.Mode,
		"file":          e.File,
		"source":        e.Source,
		"origin":        e.Origin,
		"path":          e.Path,
		"base_id":       e.BaseID,
		"lineage_id":    e.LineageID,
		"stamp_present": e.StampPresent,
		"verb_count":    e.VerbCount,
	}
}

// ListCatalogEntries discovers catalogs under user + bundled roots.
func ListCatalogEntries(userDir, bundledDir string) []CatalogListEntry {
	var out []CatalogListEntry
	seen := map[string]struct{}{}

	add := func(path, file, source, origin string) {
		if filepath.Base(path) == ManifestName {
			return
		}
		mode := ModeFromFilename(path)
		key := file + "\x00" + mode
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		stats := CatalogEntryStats(path, nil)
		out = append(out, CatalogListEntry{
			Mode:         mode,
			File:         file,
			Source:       source,
			Origin:       origin,
			Path:         path,
			BaseID:       stats["base_id"],
			LineageID:    stats["lineage_id"],
			StampPresent: stats["stamp_present"],
			VerbCount:    stats["verb_count"],
		})
	}

	addFrom := func(root, origin string) {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			return
		}
		for _, name := range jsonFilesSorted(root) {
			add(filepath.Join(root, name), name, "general", origin)
		}
		minDir := filepath.Join(root, "min")
		if st, err := os.Stat(minDir); err == nil && st.IsDir() {
			for _, name := range jsonFilesSorted(minDir) {
				add(filepath.Join(minDir, name), "min/"+name, "general", origin)
			}
		}
		for _, sub := range subdirsSorted(root) {
			src := ScopeKeyFromSubdir(sub)
			if src == "" {
				src = sub
			}
			subDir := filepath.Join(root, sub)
			for _, name := range jsonFilesSorted(subDir) {
				add(filepath.Join(subDir, name), sub+"/"+name, src, origin)
			}
			nestedMin := filepath.Join(subDir, "min")
			if st, err := os.Stat(nestedMin); err == nil && st.IsDir() {
				for _, name := range jsonFilesSorted(nestedMin) {
					add(filepath.Join(nestedMin, name), sub+"/min/"+name, src, origin)
				}
			}
		}
	}

	addFrom(userDir, "synced")
	if bundledDir != "" {
		addFrom(bundledDir, "bundled")
	}
	return out
}

var requiredFour = []string{"summary", "query_decomposition", "decomposition", "confidence"}

func stringSetFromAny(v any) map[string]struct{} {
	out := map[string]struct{}{}
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if s := asString(item); s != "" {
				out[s] = struct{}{}
			}
		}
	case []string:
		for _, s := range x {
			if s != "" {
				out[s] = struct{}{}
			}
		}
	case map[string]any:
		for k := range x {
			out[k] = struct{}{}
		}
	}
	return out
}

func setHasAll(have map[string]struct{}, need []string) bool {
	for _, n := range need {
		if _, ok := have[n]; !ok {
			return false
		}
	}
	return true
}

// SchemaRequiredFourDefined is suite expect schema.required_four_defined.
func SchemaRequiredFourDefined(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	req := stringSetFromAny(schema["required"])
	props := stringSetFromAny(schema["properties"])
	return setHasAll(req, requiredFour) || setHasAll(props, requiredFour)
}

// CatalogLoadMockExpect is L0.catalog.load_mock.001 field shape (extract only).
func CatalogLoadMockExpect(body, schema map[string]any) map[string]any {
	baseID := DocumentBaseID(body)
	stamped := ExtractStampedHash(body)
	hash := stamped
	if hash == "" {
		if cb, _ := body["contract"].(map[string]any); cb != nil {
			hash = asString(cb["hash"])
		}
	}
	verbs := verbList(body)
	mini := GetMiniSchema(body, false)
	_, miniOK := mini["entity_types"]
	return map[string]any{
		"result":                       true,
		"catalog.base_id":              baseID,
		"catalog.contract.id":          ExtractContractID(body),
		"catalog.contract.hash":        hash,
		"catalog.verb_count_gte":       len(verbs),
		"schema.required_four_defined": SchemaRequiredFourDefined(schema),
		"catalog.mini_schema.parsed":   miniOK,
	}
}

// EnrichLoaded builds LoadedCatalog from a store document (extract stamp only).
func EnrichLoaded(body map[string]any, path, source, keyBaseID, contractHash string) LoadedCatalog {
	lineage := LineageBaseID(body)
	baseID := strings.TrimSpace(keyBaseID)
	if baseID == "" {
		baseID = lineage
	}
	if baseID == "" {
		baseID = DocumentBaseID(body)
	}
	schema, example := LoadSiblingPackSchema(path)
	return LoadedCatalog{
		Body:                  body,
		Path:                  path,
		Source:                source,
		ContractHash:          contractHash,
		BaseID:                baseID,
		LineageID:             lineage,
		ResponseOutputSchema:  schema,
		ResponseOutputExample: example,
	}
}
