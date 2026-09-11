// SPDX-License-Identifier: BUSL-1.1

package zeusclient

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "github.com/koten-ai/zeus_client_golang/adapters"
	_ "github.com/koten-ai/zeus_client_golang/api"
	_ "github.com/koten-ai/zeus_client_golang/application"
	_ "github.com/koten-ai/zeus_client_golang/config"
	_ "github.com/koten-ai/zeus_client_golang/conformance"
	_ "github.com/koten-ai/zeus_client_golang/domain"
	_ "github.com/koten-ai/zeus_client_golang/domain/journal"
	_ "github.com/koten-ai/zeus_client_golang/internal/httpx"
	_ "github.com/koten-ai/zeus_client_golang/observability"
	_ "github.com/koten-ai/zeus_client_golang/ports"
	_ "github.com/koten-ai/zeus_client_golang/security"
)

func TestVersionExported(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must be exported and non-empty")
	}
}

func TestNewClose(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("New returned nil Client")
	}
	c2, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c == c2 {
		t.Fatal("New must not return a process-global Client")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close must be idempotent: %v", err)
	}
	if err := c2.Close(); err != nil {
		t.Fatal(err)
	}
	var nilClient *Client
	if err := nilClient.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
}

func TestCloseConcurrent(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestHexagonalDirsExist(t *testing.T) {
	dirs := []string{
		"config", "domain", "ports", "adapters", "application", "api",
		"observability", "security", "internal/httpx", "conformance",
	}
	for _, d := range dirs {
		st, err := os.Stat(d)
		if err != nil || !st.IsDir() {
			t.Errorf("missing hexagonal package %s: %v", d, err)
		}
	}
}

func TestDomainFreeOfNetHTTP(t *testing.T) {
	err := filepath.WalkDir("domain", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			if impPath == "net/http" || strings.HasPrefix(impPath, "net/http/") {
				t.Errorf("%s imports %s (domain must stay free of net/http)", path, impPath)
			}
			const mod = "github.com/koten-ai/zeus_client_golang"
			if impPath == mod+"/adapters" || strings.HasPrefix(impPath, mod+"/adapters/") ||
				impPath == mod+"/internal/httpx" || strings.HasPrefix(impPath, mod+"/internal/httpx/") {
				t.Errorf("%s imports %s (domain must not import adapters/httpx)", path, impPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoProcessGlobalHTTP(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		s := string(src)
		for _, needle := range []string{"http.DefaultClient", "http.DefaultTransport"} {
			if strings.Contains(s, needle) {
				t.Errorf("%s uses %s (no process-global HTTP)", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
