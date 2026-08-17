package migration

import (
    "fmt"
    "strings"
)

// TableDependency 表依赖关系
type TableDependency struct {
    TableName    string
    Dependencies []string // 依赖的表
    Dependents   []string // 依赖该表的表
}

// DependencyGraph 依赖图
type DependencyGraph struct {
    nodes map[string]*TableDependency
}

// NewDependencyGraph 创建依赖图
func NewDependencyGraph() *DependencyGraph {
    return &DependencyGraph{
        nodes: make(map[string]*TableDependency),
    }
}

// AddTable 添加表
func (g *DependencyGraph) AddTable(tableName string) {
    if _, ok := g.nodes[tableName]; !ok {
        g.nodes[tableName] = &TableDependency{
            TableName:    tableName,
            Dependencies: []string{},
            Dependents:   []string{},
        }
    }
}

// AddDependency 添加依赖关系
func (g *DependencyGraph) AddDependency(table, dependsOn string) {
    g.AddTable(table)
    g.AddTable(dependsOn)

    // 添加到依赖列表
    node := g.nodes[table]
    for _, dep := range node.Dependencies {
        if dep == dependsOn {
            return // 已存在
        }
    }
    node.Dependencies = append(node.Dependencies, dependsOn)

    // 添加到反向依赖列表
    dependent := g.nodes[dependsOn]
    for _, dep := range dependent.Dependents {
        if dep == table {
            return
        }
    }
    dependent.Dependents = append(dependent.Dependents, table)
}

// TopologicalSort 拓扑排序
func (g *DependencyGraph) TopologicalSort() ([]string, error) {
    // 计算入度（被依赖次数）
    inDegree := make(map[string]int)
    for name, node := range g.nodes {
        inDegree[name] = len(node.Dependencies)
    }

    // 找出没有依赖的表（可以被首先处理）
    var queue []string
    for name, degree := range inDegree {
        if degree == 0 {
            queue = append(queue, name)
        }
    }

    var result []string
    for len(queue) > 0 {
        // 取出一个没有依赖的表
        table := queue[0]
        queue = queue[1:]
        result = append(result, table)

        // 减少依赖该表的表的入度
        for _, dependent := range g.nodes[table].Dependents {
            inDegree[dependent]--
            if inDegree[dependent] == 0 {
                queue = append(queue, dependent)
            }
        }
    }

    // 检查是否有循环依赖
    if len(result) != len(g.nodes) {
        var remaining []string
        for name := range g.nodes {
            found := false
            for _, r := range result {
                if r == name {
                    found = true
                    break
                }
            }
            if !found {
                remaining = append(remaining, name)
            }
        }
        return result, fmt.Errorf("检测到循环依赖，无法完成排序: %v", remaining)
    }

    return result, nil
}

// ExtractDependencies 从 DDL 提取依赖关系
func ExtractDependencies(ddl string) []string {
    var dependencies []string

    // 查找 REFERENCES 关键字
    upperDDL := strings.ToUpper(ddl)

    // 匹配 REFERENCES table_name 或 REFERENCES table_name(column)
    refsIdx := strings.Index(upperDDL, "REFERENCES")
    if refsIdx < 0 {
        return dependencies
    }

    // 简单实现：查找 REFERENCES 后面的表名
    remaining := ddl[refsIdx+10:]
    remaining = strings.TrimSpace(remaining)

    // 提取表名（到第一个空格或括号为止）
    var tableName strings.Builder
    for _, ch := range remaining {
        if ch == ' ' || ch == '(' || ch == ')' || ch == ',' {
            break
        }
        if ch != '`' && ch != '"' {
            tableName.WriteRune(ch)
        }
    }

    if name := strings.TrimSpace(tableName.String()); name != "" {
        dependencies = append(dependencies, name)
    }

    return dependencies
}

// BuildDependencyGraph 从表列表构建依赖图
func BuildDependencyGraph(tables []*TableInfo, ddls map[string]string) (*DependencyGraph, error) {
    graph := NewDependencyGraph()

    // 添加所有表
    for _, table := range tables {
        graph.AddTable(table.Name)
    }

    // 分析依赖关系
    for _, table := range tables {
        ddl, ok := ddls[table.Name]
        if !ok {
            continue
        }

        // 提取 REFERENCES
        dependencies := ExtractDependencies(ddl)
        for _, dep := range dependencies {
            // 检查依赖的表是否存在
            if _, ok := graph.nodes[dep]; ok {
                graph.AddDependency(table.Name, dep)
            }
        }
    }

    return graph, nil
}

// SortTablesByDependency 按依赖关系排序表
func SortTablesByDependency(tables []*TableInfo, ddls map[string]string) ([]*TableInfo, error) {
    // 构建依赖图
    graph, err := BuildDependencyGraph(tables, ddls)
    if err != nil {
        // 如果依赖分析失败，使用简单排序
        return tables, nil
    }

    // 拓扑排序
    sorted, err := graph.TopologicalSort()
    if err != nil {
        // 如果排序失败，返回原始顺序
        return tables, nil
    }

    // 按排序结果重新排列表
    tableMap := make(map[string]*TableInfo)
    for _, t := range tables {
        tableMap[t.Name] = t
    }

    var result []*TableInfo
    for _, name := range sorted {
        if t, ok := tableMap[name]; ok {
            result = append(result, t)
        }
    }

    return result, nil
}

// DetectCircularDependencies 检测循环依赖
func DetectCircularDependencies(tables []*TableInfo, ddls map[string]string) ([]string, error) {
    graph, err := BuildDependencyGraph(tables, ddls)
    if err != nil {
        return nil, err
    }

    // 尝试拓扑排序
    _, err = graph.TopologicalSort()
    if err != nil {
        // 返回循环依赖的表
        return nil, err
    }

    return nil, nil
}

// GetSuggestedOrder 获取建议的处理顺序
func GetSuggestedOrder(tables []*TableInfo, ddls map[string]string) []string {
    sortedTables, err := SortTablesByDependency(tables, ddls)
    if err != nil {
        // 返回表名列表
        names := make([]string, len(tables))
        for i, t := range tables {
            names[i] = t.Name
        }
        return names
    }

    names := make([]string, len(sortedTables))
    for i, t := range sortedTables {
        names[i] = t.Name
    }
    return names
}