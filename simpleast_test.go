package simpleast

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"testing"

	"golang.org/x/tools/txtar"
)

func TestExtractStructs(t *testing.T) {
	cases := []string{}
	err := filepath.WalkDir("testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".txtar" {
			cases = append(cases, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			ar, err := txtar.ParseFile(c)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			input := bytes.NewReader(ar.Files[0].Data)
			expected := string(ar.Files[1].Data)

			structs, err := ParseStructs(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var got bytes.Buffer
			encoder := json.NewEncoder(&got)
			encoder.SetIndent("", "\t")
			err = encoder.Encode(structs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if expected != got.String() {
				t.Fatalf("expected:\n%s\ngot:\n%s", expected, got.String())
			}
		})
	}
}

func TestStructTags(t *testing.T) {
	st := StructTags{
		StructTag{Name: "json", Value: "name,omitempty"},
	}
	if st.Get("json") != "name,omitempty" {
		t.Fatalf("unexpected value: %s", st.Get("json"))
	}
	if st.Get("other") != "" {
		t.Fatalf("unexpected value: %s", st.Get("other"))
	}
}
