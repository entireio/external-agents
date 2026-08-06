package hermes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func loadConfig(path string) (*yaml.Node, error) {
	root := &yaml.Node{Kind: yaml.DocumentNode}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = []*yaml.Node{mapping}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return root, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Hermes config: %w", err)
	}
	if len(data) == 0 {
		return root, nil
	}
	if err := yaml.Unmarshal(data, root); err != nil {
		return nil, fmt.Errorf("parse Hermes config without modifying it: %w", err)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("hermes config must contain a YAML mapping")
	}
	return root, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func ensureMappingValue(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if value := mappingValue(mapping, key); value != nil {
		if value.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("hermes config %q must be a mapping", key)
		}
		return value, nil
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, keyNode, valueNode)
	return valueNode, nil
}

func sequenceValue(mapping *yaml.Node, key string, create bool) (*yaml.Node, error) {
	if value := mappingValue(mapping, key); value != nil {
		if value.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("hermes config plugins.%s must be a sequence", key)
		}
		return value, nil
	}
	if !create {
		return nil, nil
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	mapping.Content = append(mapping.Content, keyNode, valueNode)
	return valueNode, nil
}

func sequenceContains(sequence *yaml.Node, item string) bool {
	if sequence == nil {
		return false
	}
	for _, node := range sequence.Content {
		if node.Kind == yaml.ScalarNode && node.Value == item {
			return true
		}
	}
	return false
}

func sequenceRemove(sequence *yaml.Node, item string) bool {
	if sequence == nil {
		return false
	}
	filtered := sequence.Content[:0]
	removed := false
	for _, node := range sequence.Content {
		if node.Kind == yaml.ScalarNode && node.Value == item {
			removed = true
			continue
		}
		filtered = append(filtered, node)
	}
	sequence.Content = filtered
	return removed
}

func updatePluginConfig(home string, enabled bool) (bool, error) {
	path := filepath.Join(home, "config.yaml")
	root, err := loadConfig(path)
	if err != nil {
		return false, err
	}
	mapping := root.Content[0]
	plugins := mappingValue(mapping, "plugins")
	if plugins == nil && !enabled {
		return false, nil
	}
	if plugins == nil {
		plugins, err = ensureMappingValue(mapping, "plugins")
	} else if plugins.Kind != yaml.MappingNode {
		return false, errors.New("hermes config plugins must be a mapping")
	}
	if err != nil {
		return false, err
	}
	enabledList, err := sequenceValue(plugins, "enabled", enabled)
	if err != nil {
		return false, err
	}
	disabledList, err := sequenceValue(plugins, "disabled", false)
	if err != nil {
		return false, err
	}

	changed := false
	if enabled {
		if !sequenceContains(enabledList, pluginName) {
			enabledList.Content = append(enabledList.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pluginName})
			changed = true
		}
		changed = sequenceRemove(disabledList, pluginName) || changed
	} else {
		changed = sequenceRemove(enabledList, pluginName)
	}
	if !changed {
		return false, nil
	}
	data, err := yaml.Marshal(root)
	if err != nil {
		return false, fmt.Errorf("encode Hermes config: %w", err)
	}
	if err := atomicWrite(path, data, 0o600); err != nil {
		return false, fmt.Errorf("update Hermes config: %w", err)
	}
	return true, nil
}

func pluginEnabled(home string) bool {
	root, err := loadConfig(filepath.Join(home, "config.yaml"))
	if err != nil {
		return false
	}
	plugins := mappingValue(root.Content[0], "plugins")
	if plugins == nil || plugins.Kind != yaml.MappingNode {
		return false
	}
	enabled, err := sequenceValue(plugins, "enabled", false)
	if err != nil || !sequenceContains(enabled, pluginName) {
		return false
	}
	disabled, err := sequenceValue(plugins, "disabled", false)
	return err == nil && !sequenceContains(disabled, pluginName)
}
