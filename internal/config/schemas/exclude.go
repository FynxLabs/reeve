package schemas

import "gopkg.in/yaml.v3"

// UnmarshalYAML accepts either a plain string ("projects/sandbox/*") or a
// map form ({stack: "*/scratch"}).
func (e *ExcludeRule) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		e.Pattern = node.Value
		return nil
	}
	var m struct {
		Stack string `yaml:"stack"`
	}
	if err := node.Decode(&m); err != nil {
		return err
	}
	e.Stack = m.Stack
	return nil
}
