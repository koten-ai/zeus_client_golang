// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
)

var (
	packSchemaNames = []string{
		"response_output_schema.json",
		"response_output_schema.min.json",
	}
	packExampleNames = []string{
		"response_output_example.json",
		"response_output_example.min.json",
	}
)

func readJSONObjectFile(path string) map[string]any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// LoadSiblingPackSchema returns (schema, example) from the catalog file's directory.
func LoadSiblingPackSchema(catalogPath string) (schema, example map[string]any) {
	if catalogPath == "" {
		return nil, nil
	}
	parent := filepath.Dir(catalogPath)
	for _, name := range packSchemaNames {
		p := filepath.Join(parent, name)
		if isRegularFile(p) {
			if doc := readJSONObjectFile(p); doc != nil {
				schema = doc
				break
			}
		}
	}
	for _, name := range packExampleNames {
		p := filepath.Join(parent, name)
		if isRegularFile(p) {
			if doc := readJSONObjectFile(p); doc != nil {
				example = doc
				break
			}
		}
	}
	return schema, example
}
