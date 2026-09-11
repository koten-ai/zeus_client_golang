// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"reflect"
	"testing"
)

const miniBrief = `

## SCOPE BRIEF (authoritative — DO NOT call get_stats ...)

scope:           beer-sample/_default
mode:            auto
nodes_total:     7303      edges_total: 24500      entities_total: 0

## MINI-SCHEMA (per entity_type — derived from the scope's EntityMap)
rule: ` + "`find_nodes`" + ` ` + "`where`" + ` is equality-only on these paths.
rule: an ` + "`ex:`" + ` trailer shows a NON-EXHAUSTIVE sample of that field's stored values.

### Beer  (fields: 8)
  - abv                    number       [gsi]
  - category               display      [none]  -- result-only, NOT filterable
  - ibu                    number       [gsi]
  - description            text_fts     [fts]   -- use fts_search / hybrid_search
  - brewery_id             entity_fk    [gsi]   fk_to=Brewery  ex: coopers_brewery
  - name                   scalar_ent   [gsi]   fk_to=Beer  ex: Coopers Sparkling Ale

### Brewery  (fields: 9)
  - website                display      [none]  -- result-only, NOT filterable
  - city                   scalar_ent   [gsi]   fk_to=City  ex: Arena
  - state                  scalar_ent   [gsi]   fk_to=State  ex: Maine
  inverse_fks:
    ` + "\u2190" + ` Beer.brewery_id  (entity_fk)  -- back-walk: find_nodes(entity_type:"Beer", where:{brewery_id:"<this.doc_key>"})

## WALK_PATHS (pre-validated FK chains)
  - Beer ` + "\u2192" + ` Brewery                   path=[brewery_id]                             hops=1
`

func baseChatReq() map[string]any {
	return map[string]any{
		"_format": "zeus.chat_request.v2",
		"messages": []any{
			map[string]any{"role": "system", "content": "BASE RULES ..." + miniBrief},
		},
		"guidance": map[string]any{
			"injections": map[string]any{"business_logic": []any{}},
		},
		"verbs": []any{},
	}
}

func TestGetMiniSchemaStructure(t *testing.T) {
	s := GetMiniSchema(baseChatReq(), false)
	if s["scope"] != "beer-sample/_default" {
		t.Fatalf("scope %v", s["scope"])
	}
	if s["mode"] != "auto" {
		t.Fatalf("mode %v", s["mode"])
	}
	ents, _ := s["entity_types"].(map[string]any)
	if _, ok := ents["Beer"]; !ok {
		t.Fatalf("Beer missing %#v", ents)
	}
	if _, ok := ents["Brewery"]; !ok {
		t.Fatal("Brewery")
	}
	if len(ents) != 2 {
		t.Fatalf("ents %d", len(ents))
	}
	beer, _ := ents["Beer"].(map[string]any)
	if beer["field_count"] != 8 {
		t.Fatalf("field_count %v", beer["field_count"])
	}
	fields, _ := beer["fields"].(map[string]any)
	abv, _ := fields["abv"].(map[string]any)
	if abv["kind"] != "number" || abv["indexed"] != "gsi" || abv["filterable"] != true {
		t.Fatalf("abv %#v", abv)
	}
	cat, _ := fields["category"].(map[string]any)
	if cat["filterable"] != false {
		t.Fatalf("category %#v", cat)
	}
	bid, _ := fields["brewery_id"].(map[string]any)
	if bid["fk_to"] != "Brewery" {
		t.Fatalf("fk %#v", bid)
	}
	if _, ok := bid["examples"]; ok {
		t.Fatal("examples must be omitted when values=false")
	}
	brew, _ := ents["Brewery"].(map[string]any)
	inv, _ := brew["inverse_fks"].([]any)
	if len(inv) != 1 {
		t.Fatalf("inverse %#v", inv)
	}
	row, _ := inv[0].(map[string]any)
	want := map[string]any{"from_entity": "Beer", "from_path": "brewery_id", "kind": "entity_fk"}
	if !reflect.DeepEqual(row, want) {
		t.Fatalf("inverse %#v", row)
	}
}

func TestGetMiniSchemaValues(t *testing.T) {
	s := GetMiniSchema(baseChatReq(), true)
	ents := s["entity_types"].(map[string]any)
	beer := ents["Beer"].(map[string]any)
	fields := beer["fields"].(map[string]any)
	bid := fields["brewery_id"].(map[string]any)
	ex, ok := bid["examples"].([]string)
	if !ok || !reflect.DeepEqual(ex, []string{"coopers_brewery"}) {
		t.Fatalf("examples %#v", bid["examples"])
	}
}

func TestGetMiniSchemaNoBriefIsEmpty(t *testing.T) {
	s := GetMiniSchema(map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "no brief here"}},
	}, false)
	if s["scope"] != "" || s["mode"] != "" {
		t.Fatalf("%v", s)
	}
	ents, _ := s["entity_types"].(map[string]any)
	if len(ents) != 0 {
		t.Fatalf("invented %#v", ents)
	}
}

func TestClassifyMiniSchemaMissing(t *testing.T) {
	got := ClassifyMiniSchema(GetMiniSchema(map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "no brief here"}},
	}, false))
	if got["result"] != false {
		t.Fatalf("result %v", got["result"])
	}
	if got["error_class"] != MissingMiniSchema {
		t.Fatalf("class %v", got["error_class"])
	}
}

func TestClassifyMiniSchemaPresent(t *testing.T) {
	got := ClassifyMiniSchema(GetMiniSchema(baseChatReq(), false))
	if got["result"] != true {
		t.Fatal("result")
	}
	if got["error_class"] != nil {
		t.Fatalf("class %v", got["error_class"])
	}
	ents, _ := got["entity_types"].(map[string]any)
	if _, ok := ents["Beer"]; !ok {
		t.Fatal("Beer")
	}
}

func TestGetMiniSchemaEmptyOnNil(t *testing.T) {
	s := GetMiniSchema(nil, false)
	ents, _ := s["entity_types"].(map[string]any)
	if s["scope"] != "" || len(ents) != 0 {
		t.Fatalf("%v", s)
	}
}

func TestMiniSchemaFromMockHasEmptyEntityTypes(t *testing.T) {
	dir := mockFixtureDir(t)
	body := readJSONFile(t, filepathJoinMock(dir))
	s := GetMiniSchema(body, false)
	ents, _ := s["entity_types"].(map[string]any)
	if len(ents) != 0 {
		t.Fatalf("invented %#v", ents)
	}
}

func filepathJoinMock(dir string) string {
	return dir + "/chat_request.mock.json"
}
