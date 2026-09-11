// SPDX-License-Identifier: BUSL-1.1

package catalogfs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const storeComponent = "adapters.catalog_fs"

// Store is CatalogStore over a user chat_requests root (+ optional bundled).
type Store struct {
	Root       string
	BundledDir string
}

var _ ports.CatalogStore = (*Store)(nil)

// Options constructs Store.
type Options struct {
	BundledDir string
}

// New is FsCatalogStore(root, bundled_dir=).
func New(root string, opts Options) *Store {
	return &Store{Root: root, BundledDir: opts.BundledDir}
}

func (s *Store) userDir() string {
	if s == nil {
		return ""
	}
	return s.Root
}

func (s *Store) bundledDir() string {
	if s == nil {
		return ""
	}
	return s.BundledDir
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

// ResolvePath is the fail-closed resolve (no sibling-scope rglob).
func (s *Store) ResolvePath(key ports.CatalogKey) (string, bool) {
	if s == nil {
		return "", false
	}
	return domain.ResolveCatalogPath(domain.CatalogResolveOpts{
		Mode:       key.Mode,
		Bucket:     key.Bucket,
		Scope:      key.Scope,
		UserDir:    s.Root,
		BundledDir: s.BundledDir,
		BaseID:     key.BaseID,
	})
}

// Load is ports.CatalogStore.Load — miss → 030001, bad JSON → 030002.
func (s *Store) Load(ctx context.Context, key ports.CatalogKey) (ports.CatalogDocument, error) {
	if err := ctxErr(ctx); err != nil {
		return ports.CatalogDocument{}, err
	}
	path, ok := s.ResolvePath(key)
	if !ok {
		scopeHint := domain.ScopeChatRequestsSubdir(key.Bucket, key.Scope)
		msg := "catalog not found for mode='" + key.Mode + "' base_id='" + key.BaseID +
			"' scope=" + key.Bucket + "/" + key.Scope + "; expected under " +
			filepath.Join(s.Root, scopeHint) + " or top-level " +
			filepath.Join(s.Root, domain.ChatRequestFilename(key.Mode, key.BaseID))
		if s.BundledDir != "" {
			msg += " or bundled " + s.BundledDir
		}
		return ports.CatalogDocument{}, domain.NewCatalog(domain.CodeCatalogNotFound, storeComponent,
			domain.WithMessage(msg),
			domain.WithDetails(map[string]any{
				"mode":     key.Mode,
				"base_id":  key.BaseID,
				"bucket":   key.Bucket,
				"scope":    key.Scope,
				"user_dir": s.Root,
			}),
		)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ports.CatalogDocument{}, domain.NewCatalog(domain.CodeCatalogNotFound, storeComponent,
			domain.WithMessage("catalog unreadable: "+path),
			domain.WithDetails(map[string]any{"path": path, "error": err.Error()}),
			domain.WithCause(err),
		)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ports.CatalogDocument{}, domain.NewCatalog(domain.CodeCatalogParseFailed, storeComponent,
			domain.WithMessage("catalog parse failed: "+filepath.Base(path)),
			domain.WithDetails(map[string]any{"path": path, "error": err.Error()}),
			domain.WithCause(err),
		)
	}
	bodyMap, _ := decoded.(map[string]any)
	body := domain.PrepareLoadedDocument(bodyMap)
	if strings.TrimSpace(key.BaseID) != "" {
		if err := domain.CheckLineage(body, key.BaseID, true, filepath.Base(path)); err != nil {
			return ports.CatalogDocument{}, err
		}
	}
	stamped := domain.ExtractStampedHash(body)
	return ports.CatalogDocument{
		Body:         body,
		Path:         path,
		ContractHash: stamped,
	}, nil
}

// LoadRich loads with source label + sibling pack schema (API convenience).
func (s *Store) LoadRich(ctx context.Context, key ports.CatalogKey) (domain.LoadedCatalog, error) {
	doc, err := s.Load(ctx, key)
	if err != nil {
		return domain.LoadedCatalog{}, err
	}
	source := "unknown"
	if doc.Path != "" {
		source = domain.PathSourceLabel(doc.Path, s.userDir(), s.bundledDir())
	}
	return domain.EnrichLoaded(doc.Body, doc.Path, source, key.BaseID, doc.ContractHash), nil
}

// Save writes into the scope subdir under root (never sibling guess).
func (s *Store) Save(ctx context.Context, key ports.CatalogKey, doc ports.CatalogDocument) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	scopeDir := filepath.Join(s.Root, domain.ScopeChatRequestsSubdir(key.Bucket, key.Scope))
	filename := domain.ChatRequestFilename(key.Mode, key.BaseID)
	out := filepath.Join(scopeDir, filename)
	body := doc.Body
	if body == nil {
		body = map[string]any{}
	}
	return atomicWriteJSON(out, body)
}

// ListEntries discovers catalogs under root + bundled.
func (s *Store) ListEntries(_ context.Context) []domain.CatalogListEntry {
	if s == nil {
		return nil
	}
	return domain.ListCatalogEntries(s.Root, s.BundledDir)
}

func atomicWriteJSON(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
