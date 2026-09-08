package appconfig

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ToYamlKey 将 camelCase 转换为 snake_case
func ToYamlKey(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteRune('_')
			}
			result.WriteRune(r + 32) // 转小写
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// SaveConfigWithComments 使用 go-yaml/v3 AST 保留注释保存配置
func SaveConfigWithComments(configPath string, updates map[string]interface{}) error {
	// 读取原始文件
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析 YAML，保留注释（headComment, LineComment 等）
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Errorf("解析 YAML 失败: %w", err)
	}

	// 获取根节点
	root := &doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}

	// 应用更新
	for yamlPath, value := range updates {
		path := parseYamlPath(yamlPath)
		updateFieldInNode(root, path, value)
	}

	// 序列化回 YAML
	output, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("序列化 YAML 失败: %w", err)
	}

	// 写回文件
	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// parseYamlPath 解析 YAML 路径，如 "app.name" -> ["app", "name"]
func parseYamlPath(path string) []string {
	parts := strings.Split(path, ".")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// updateFieldInNode 递归更新 YAML 节点中的字段值
// 如果路径不存在，会创建中间 mapping 节点
func updateFieldInNode(node *yaml.Node, path []string, value interface{}) bool {
	if len(path) == 0 || node == nil {
		return false
	}

	key := path[0]

	// 确保节点是 Mapping 类型
	if node.Kind != yaml.MappingNode {
		// 如果不是 Mapping，尝试转换为 Mapping（仅支持 Scalar → Mapping）
		if node.Kind == yaml.ScalarNode {
			node.Kind = yaml.MappingNode
			node.Tag = ""
			node.Value = ""
			node.Content = nil
		} else {
			return false
		}
	}

	// 查找键名
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]

		if keyNode.Value == key || keyNode.Value == ToYamlKey(key) {
			if len(path) == 1 {
				return setNodeValue(valNode, value)
			}
			return updateFieldInNode(valNode, path[1:], value)
		}
	}

	// 键不存在，创建新的 key-value 对
	newKey := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	if len(path) == 1 {
		// 到达目标，创建 value 节点
		newVal := &yaml.Node{Kind: yaml.ScalarNode}
		setNodeValue(newVal, value)
		node.Content = append(node.Content, newKey, newVal)
		return true
	}

	// 创建中间 mapping 节点
	newVal := &yaml.Node{Kind: yaml.MappingNode}
	node.Content = append(node.Content, newKey, newVal)
	return updateFieldInNode(newVal, path[1:], value)
}

// setNodeValue 根据值类型设置节点
func setNodeValue(node *yaml.Node, value interface{}) bool {
	if node == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		node.Kind = yaml.ScalarNode
		node.Tag = ""
		node.Value = v
	case int:
		node.Kind = yaml.ScalarNode
		node.Tag = ""
		node.Value = fmt.Sprintf("%d", v)
	case int64:
		node.Kind = yaml.ScalarNode
		node.Tag = ""
		node.Value = fmt.Sprintf("%d", v)
	case float64:
		node.Kind = yaml.ScalarNode
		node.Tag = ""
		node.Value = fmt.Sprintf("%v", v)
	case bool:
		node.Kind = yaml.ScalarNode
		node.Tag = ""
		node.Value = fmt.Sprintf("%t", v)
	case []interface{}:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: node.Style}
		for _, item := range v {
			switch m := item.(type) {
			case map[string]interface{}:
				mapNode := &yaml.Node{Kind: yaml.MappingNode}
				for k, val := range m {
					keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: k}
					valStr := fmt.Sprintf("%v", val)
					valNode := &yaml.Node{Kind: yaml.ScalarNode, Value: valStr}
					mapNode.Content = append(mapNode.Content, keyNode, valNode)
				}
				seq.Content = append(seq.Content, mapNode)
			default:
				itemNode := &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%v", item)}
				seq.Content = append(seq.Content, itemNode)
			}
		}
		*node = *seq
		return true
	}
	return true
}

// ConfigUpdates 配置更新映射类型
type ConfigUpdates map[string]interface{}

// NewConfigUpdates 创建配置更新对象
func NewConfigUpdates() ConfigUpdates {
	return make(ConfigUpdates)
}

// Add 添加更新项
func (c ConfigUpdates) Add(key string, value interface{}) ConfigUpdates {
	c[key] = value
	return c
}

// Apply 应用更新到配置文件
func (c ConfigUpdates) Apply(configPath string) error {
	return SaveConfigWithComments(configPath, c)
}
