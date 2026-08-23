package ingest

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestYamlStringMapKeysSortedFromScratch(t *testing.T) {
	in := yamlStringMap{"z-label": "9", "a-label": "1", "m-label": "5"}
	var first []byte
	for i := 0; i < 25; i++ {
		got, err := yaml.Marshal(struct {
			Labels yamlStringMap `yaml:"labels"`
		}{Labels: in})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = append([]byte(nil), got...)
			s := string(got)
			ai := strings.Index(s, "a-label")
			mi := strings.Index(s, "m-label")
			zi := strings.Index(s, "z-label")
			if ai < 0 || mi < 0 || zi < 0 || !(ai < mi && mi < zi) {
				t.Fatalf("keys not sorted:\n%s", s)
			}
			continue
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("run %d yaml drifted\n first:\n%s\n got:\n%s", i+1, first, got)
		}
	}
}
