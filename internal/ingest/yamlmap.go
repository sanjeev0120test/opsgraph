package ingest

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// yamlStringMap encodes with sorted keys so pack bytes (and SHA-256) are stable.
type yamlStringMap map[string]string

func (m yamlStringMap) MarshalYAML() (any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: m[k]},
		)
	}
	return n, nil
}

func (m *yamlStringMap) UnmarshalYAML(n *yaml.Node) error {
	if n == nil || n.Tag == "!!null" || (n.Kind == yaml.ScalarNode && n.Value == "") {
		*m = nil
		return nil
	}
	var raw map[string]string
	if err := n.Decode(&raw); err != nil {
		return err
	}
	*m = raw
	return nil
}
