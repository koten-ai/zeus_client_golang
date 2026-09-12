// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/koten-ai/zeus_client_golang/adapters/catalogfs"
	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const catalogAPIComponent = "api.catalog"

// CatalogOptions constructs CatalogAPI. Store nil → lazy FsCatalogStore from
// Config.ChatRequestsDir. Runtime services.Catalog stays nil unless injected.
type CatalogOptions struct {
	Store      ports.CatalogStore
	Config     config.RuntimeConfig
	BundledDir string
	Log        func(msg string, attrs map[string]any)
}

// LoadParams is catalog.load (Python CatalogAPI.load kwargs).
type LoadParams struct {
	Mode   string
	Target *config.DataTarget
	BaseID string
}

// MiniSchemaParams is catalog.mini_schema.get kwargs.
type MiniSchemaParams struct {
	Mode   string
	Target *config.DataTarget
	BaseID string
	Values bool
}

type richCatalogStore interface {
	LoadRich(context.Context, ports.CatalogKey) (domain.LoadedCatalog, error)
}

type listingCatalogStore interface {
	ListEntries(context.Context) []domain.CatalogListEntry
}

// CatalogAPI is client.catalog (Python CatalogAPI).
type CatalogAPI struct {
	opts       CatalogOptions
	Contract   *CatalogContractAPI
	MiniSchema *CatalogMiniSchemaAPI

	mu          sync.Mutex
	lastLoadLog map[string]any
	cache       map[string]catalogCacheEntry
}

type catalogCacheEntry struct {
	path   string
	mtime  int64
	loaded domain.LoadedCatalog
}

// NewCatalogAPI binds store + config. Nil-safe: methods fail closed.
func NewCatalogAPI(opts CatalogOptions) *CatalogAPI {
	c := &CatalogAPI{opts: opts}
	c.Contract = &CatalogContractAPI{parent: c}
	c.MiniSchema = &CatalogMiniSchemaAPI{parent: c}
	return c
}

// LastLoadLog is the last zeus_client.catalog.loaded attrs (testable; no body dump).
func (c *CatalogAPI) LastLoadLog() map[string]any {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastLoadLog == nil {
		return nil
	}
	out := make(map[string]any, len(c.lastLoadLog))
	for k, v := range c.lastLoadLog {
		out[k] = v
	}
	return out
}

func (c *CatalogAPI) log(msg string, attrs map[string]any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.lastLoadLog = attrs
	logf := c.opts.Log
	c.mu.Unlock()
	if logf != nil {
		logf(msg, attrs)
	}
}

func (c *CatalogAPI) store() (ports.CatalogStore, error) {
	if c == nil {
		return nil, domain.NewCatalog(domain.CodeCatalogNotFound, catalogAPIComponent,
			domain.WithMessage("catalog API is nil"))
	}
	if c.opts.Store != nil {
		return c.opts.Store, nil
	}
	root := strings.TrimSpace(c.opts.Config.ChatRequestsDir)
	if root == "" {
		return nil, domain.NewCatalog(domain.CodeCatalogNotFound, catalogAPIComponent,
			domain.WithMessage("catalog store not wired and RuntimeConfig.chat_requests_dir is unset"))
	}
	return catalogfs.New(root, catalogfs.Options{BundledDir: c.opts.BundledDir}), nil
}

func (c *CatalogAPI) target(t *config.DataTarget) config.DataTarget {
	if t != nil {
		return *t
	}
	return c.opts.Config.Target
}

func (c *CatalogAPI) mode(mode string) string {
	m := strings.TrimSpace(mode)
	if m != "" {
		return m
	}
	m = strings.TrimSpace(c.opts.Config.Settings.Mode)
	if m != "" {
		return m
	}
	return "analytics"
}

func (c *CatalogAPI) key(params LoadParams) ports.CatalogKey {
	t := c.target(params.Target)
	return ports.CatalogKey{
		Mode:   c.mode(params.Mode),
		Bucket: t.Bucket,
		Scope:  t.Scope,
		BaseID: params.BaseID,
	}
}

func (c *CatalogAPI) dirs() (userDir, bundled string) {
	userDir = c.opts.Config.ChatRequestsDir
	bundled = c.opts.BundledDir
	if s, ok := c.opts.Store.(*catalogfs.Store); ok && s != nil {
		userDir = s.Root
		if bundled == "" {
			bundled = s.BundledDir
		}
	}
	return userDir, bundled
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return de
		}
		return err
	}
	return nil
}

func catalogMemoKey(k ports.CatalogKey) string {
	return k.Mode + "\x00" + k.Bucket + "\x00" + k.Scope + "\x00" + k.BaseID
}

func (c *CatalogAPI) cacheHit(key ports.CatalogKey) (domain.LoadedCatalog, bool) {
	if c == nil {
		return domain.LoadedCatalog{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		return domain.LoadedCatalog{}, false
	}
	ent, ok := c.cache[catalogMemoKey(key)]
	if !ok || ent.path == "" {
		return domain.LoadedCatalog{}, false
	}
	st, err := os.Stat(ent.path)
	if err != nil || st.ModTime().UnixNano() != ent.mtime {
		delete(c.cache, catalogMemoKey(key))
		return domain.LoadedCatalog{}, false
	}
	return cloneLoadedCatalog(ent.loaded), true
}

func (c *CatalogAPI) remember(key ports.CatalogKey, loaded domain.LoadedCatalog) {
	if c == nil || strings.TrimSpace(loaded.Path) == "" {
		return
	}
	st, err := os.Stat(loaded.Path)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]catalogCacheEntry{}
	}
	c.cache[catalogMemoKey(key)] = catalogCacheEntry{
		path:   loaded.Path,
		mtime:  st.ModTime().UnixNano(),
		loaded: cloneLoadedCatalog(loaded),
	}
}

func cloneLoadedCatalog(in domain.LoadedCatalog) domain.LoadedCatalog {
	out := in
	out.Body = cloneJSONMap(in.Body)
	out.ResponseOutputSchema = cloneJSONMap(in.ResponseOutputSchema)
	out.ResponseOutputExample = cloneJSONMap(in.ResponseOutputExample)
	return out
}

func cloneJSONMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return m
	}
	return out
}

// Load is catalog.load — fail-closed FS resolve, extract stamp, floor check.
func (c *CatalogAPI) Load(ctx context.Context, params LoadParams) (domain.LoadedCatalog, error) {
	if err := ctxErr(ctx); err != nil {
		return domain.LoadedCatalog{}, err
	}
	key := c.key(params)
	if hit, ok := c.cacheHit(key); ok {
		return hit, nil
	}
	store, err := c.store()
	if err != nil {
		return domain.LoadedCatalog{}, err
	}
	var loaded domain.LoadedCatalog
	if rich, ok := store.(richCatalogStore); ok {
		loaded, err = rich.LoadRich(ctx, key)
	} else {
		var doc ports.CatalogDocument
		doc, err = store.Load(ctx, key)
		if err == nil {
			userDir, bundled := c.dirs()
			source := "unknown"
			if doc.Path != "" {
				source = domain.PathSourceLabel(doc.Path, userDir, bundled)
			}
			loaded = domain.EnrichLoaded(doc.Body, doc.Path, source, key.BaseID, doc.ContractHash)
		}
	}
	if err != nil {
		return domain.LoadedCatalog{}, err
	}
	baseID := loaded.BaseID
	if baseID == "" {
		baseID = params.BaseID
	}
	floor := c.opts.Config.ClientFloor
	if floor == "" {
		floor = domain.DefaultClientFloor
	}
	if ferr := domain.FloorViolation(floor, baseID); ferr != nil {
		if !c.opts.Config.AllowDegradedCatalog {
			return domain.LoadedCatalog{}, ferr
		}
		c.log("zeus_client.catalog.floor_degraded", map[string]any{
			"base_id":      baseID,
			"client.floor": floor,
			"source":       loaded.Source,
			"result":       "degraded",
		})
	}
	stampPresent := "false"
	if loaded.ContractHash != "" {
		stampPresent = "true"
	}
	c.log("zeus_client.catalog.loaded", map[string]any{
		"base_id":       loaded.BaseID,
		"client.floor":  floor,
		"source":        loaded.Source,
		"result":        "ok",
		"stamp_present": stampPresent,
	})
	c.remember(key, loaded)
	return loaded, nil
}

// EnsureScopeBrief merges a live ## SCOPE BRIEF when the catalog has none.
// Fetch is nil this train (no catalog_remote adapter) — same skip note as Python.
func (c *CatalogAPI) EnsureScopeBrief(ctx context.Context, doc map[string]any, mode string, target config.DataTarget) application.ScopeBriefResult {
	t := c.target(&target)
	m := c.mode(mode)
	return application.EnsureScopeBrief(ctx, doc, nil, t.Bucket, t.Scope, m)
}

// LoadForTurn is disk load plus optional live SCOPE BRIEF merge (Python load_for_turn).
func (c *CatalogAPI) LoadForTurn(ctx context.Context, params LoadParams) (domain.LoadedCatalog, error) {
	loaded, err := c.Load(ctx, params)
	if err != nil {
		return domain.LoadedCatalog{}, err
	}
	result := c.EnsureScopeBrief(ctx, loaded.Body, c.mode(params.Mode), c.target(params.Target))
	loaded.Body = result.Body
	if result.Merged {
		loaded.Source = loaded.Source + " + live scope brief"
	} else if result.Note != "" && result.Note != "scope_brief: already present" && domain.ExtractScopeBrief(result.Body) == "" {
		loaded.Source = loaded.Source + "; " + result.Note
	}
	return loaded, nil
}

// List is catalog.list.
func (c *CatalogAPI) List(ctx context.Context) ([]domain.CatalogListEntry, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	store, err := c.store()
	if err != nil {
		return nil, err
	}
	if listing, ok := store.(listingCatalogStore); ok {
		return listing.ListEntries(ctx), nil
	}
	userDir, bundled := c.dirs()
	if strings.TrimSpace(userDir) == "" {
		return nil, nil
	}
	return domain.ListCatalogEntries(userDir, bundled), nil
}

// Info is catalog.info — metadata / lineage / stamp present / verb count; no body dump.
func (c *CatalogAPI) Info(ctx context.Context, params LoadParams) (map[string]string, error) {
	loaded, err := c.Load(ctx, params)
	if err != nil {
		return nil, err
	}
	path := loaded.Path
	if path == "" {
		path = "."
	}
	stats := domain.CatalogEntryStats(path, loaded.Body)
	baseID := loaded.BaseID
	if baseID == "" {
		baseID = stats["base_id"]
	}
	lineage := loaded.LineageID
	if lineage == "" {
		lineage = stats["lineage_id"]
	}
	hasSchema := "false"
	if loaded.ResponseOutputSchema != nil {
		hasSchema = "true"
	}
	envelope := ""
	if loaded.Body != nil {
		envelope = asString(loaded.Body["_format"])
	}
	floor := c.opts.Config.ClientFloor
	if floor == "" {
		floor = domain.DefaultClientFloor
	}
	return map[string]string{
		"mode":                c.mode(params.Mode),
		"base_id":             baseID,
		"lineage_id":          lineage,
		"stamp_present":       stats["stamp_present"],
		"verb_count":          stats["verb_count"],
		"contract_id":         stats["contract_id"],
		"source":              loaded.Source,
		"path":                loaded.Path,
		"client_floor":        floor,
		"envelope":            envelope,
		"has_response_schema": hasSchema,
	}, nil
}

// LoadPin loads RuntimeConfig.ProductionBaseID (human-copied COMPAT id only).
func (c *CatalogAPI) LoadPin(ctx context.Context, params LoadParams) (domain.LoadedCatalog, error) {
	pin := strings.TrimSpace(c.opts.Config.ProductionBaseID)
	if pin == "" {
		return domain.LoadedCatalog{}, domain.NewCatalog(domain.CodeCatalogNotFound, catalogAPIComponent,
			domain.WithMessage("production_base_id unset; pass base_id= to catalog.load or set production_base_id from chat_request COMPAT + Hub stamp"))
	}
	params.BaseID = pin
	return c.Load(ctx, params)
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func scopeContractEntry(contracts map[string]any, bucket, scope, mode string) (id, hash string) {
	if contracts == nil {
		return "", ""
	}
	raw := contracts[bucket+"/"+scope]
	if raw == nil {
		raw = contracts[bucket+"."+scope]
	}
	entry, _ := raw.(map[string]any)
	if entry == nil {
		return "", ""
	}
	modeEnt, _ := entry[mode].(map[string]any)
	if modeEnt == nil {
		modeEnt = entry
	}
	id = strings.TrimSpace(asString(modeEnt["contract_id"]))
	if id == "" {
		id = strings.TrimSpace(asString(modeEnt["id"]))
	}
	hash = strings.TrimSpace(asString(modeEnt["contract_hash"]))
	if hash == "" {
		hash = strings.TrimSpace(asString(modeEnt["hash"]))
	}
	if strings.Contains(hash, "TO_BE_FILLED") {
		hash = ""
	}
	return id, hash
}

// CatalogContractAPI is catalog.contract.hash / id / bind / compute_local.
type CatalogContractAPI struct {
	parent *CatalogAPI
}

func (c *CatalogContractAPI) parentOr() *CatalogAPI {
	if c == nil {
		return nil
	}
	return c.parent
}

// Hash extracts the stamped hash only (030004 if missing). Never computes.
func (c *CatalogContractAPI) Hash(_ context.Context, doc map[string]any) (string, error) {
	return domain.StampedHash(doc)
}

// ID is catalog.contract.id.
func (c *CatalogContractAPI) ID(_ context.Context, doc map[string]any) string {
	return domain.ExtractContractID(doc)
}

// ComputeLocal is diagnostics only — never a production stamp.
func (c *CatalogContractAPI) ComputeLocal(_ context.Context, doc map[string]any) string {
	return domain.ComputeContractHash(doc)
}

// Bind prefers published stamp; falls back to config scope_contracts. Never invents.
func (c *CatalogContractAPI) Bind(_ context.Context, bucket, scope, mode string, doc map[string]any) (map[string]string, error) {
	stamped := ""
	cid := ""
	if doc != nil {
		stamped = domain.ExtractStampedHash(doc)
		cid = domain.ExtractContractID(doc)
	}
	var contracts map[string]any
	if p := c.parentOr(); p != nil {
		contracts = p.opts.Config.ScopeContracts
	}
	boundID, boundHash := scopeContractEntry(contracts, bucket, scope, mode)
	if stamped != "" {
		id := cid
		if id == "" {
			id = boundID
		}
		return map[string]string{
			"contract_id":   id,
			"contract_hash": stamped,
			"hash_source":   "stamp",
		}, nil
	}
	if boundHash != "" {
		id := cid
		if id == "" {
			id = boundID
		}
		return map[string]string{
			"contract_id":   id,
			"contract_hash": boundHash,
			"hash_source":   "bound",
		}, nil
	}
	return nil, domain.NewCatalog(domain.CodeContractHashMissing, "api.catalog.contract",
		domain.WithMessage("no stamped or bound contract_hash (invent forbidden)"),
		domain.WithDetails(map[string]any{"bucket": bucket, "scope": scope, "mode": mode}),
	)
}

// CatalogMiniSchemaAPI is catalog.mini_schema.get / from_catalog.
type CatalogMiniSchemaAPI struct {
	parent *CatalogAPI
}

// FromCatalog is the offline public parse (source=disk).
func (m *CatalogMiniSchemaAPI) FromCatalog(_ context.Context, doc map[string]any, values bool) map[string]any {
	parsed := domain.GetMiniSchema(doc, values)
	parsed["source"] = "disk"
	return parsed
}

// Get is disk-only in ZCG-15 (live Zeus fetch is later). Missing catalog → empty entity_types.
func (m *CatalogMiniSchemaAPI) Get(ctx context.Context, params MiniSchemaParams) (map[string]any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	empty := domain.GetMiniSchema(nil, false)
	empty["source"] = "disk"
	empty["req_id"] = ""
	if m == nil || m.parent == nil {
		return empty, nil
	}
	loaded, err := m.parent.Load(ctx, LoadParams{Mode: params.Mode, Target: params.Target, BaseID: params.BaseID})
	if err != nil {
		if _, ok := domain.AsError(err); ok {
			return empty, nil
		}
		return nil, err
	}
	parsed := domain.GetMiniSchema(loaded.Body, params.Values)
	parsed["source"] = "disk"
	parsed["req_id"] = ""
	return parsed, nil
}
