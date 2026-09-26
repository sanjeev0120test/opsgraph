package ingest

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func FuzzServicesYAML(f *testing.F) {
	f.Add([]byte("services:\n  - id: api\n    name: API\n    health: healthy\n"))
	f.Add([]byte(""))
	f.Add([]byte("services: [1, 2, {"))
	f.Add([]byte("services:\n  - id: !!binary abc\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Cap the input. A multi-megabyte fuzz case can still be inside
		// Unmarshal when the 5s fuzz budget ends, and the engine then
		// reports "context deadline exceeded" instead of a clean stop.
		if len(data) > 8<<10 {
			return
		}
		var out fxServices
		// Must never panic on untrusted fixture YAML; parse errors are fine.
		_ = yaml.Unmarshal(data, &out)
	})
}
