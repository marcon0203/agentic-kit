package descriptor

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// 表达式算子的封闭集合。加算子必须有真实渠道逼出来——参照项目
// open-ai-canvas 那套 25+ 算子，一半是被个别渠道逼出的一次性产物，之后
// 谁也不敢删。
const (
	opRef      = "$ref"
	opLiteral  = "$literal"
	opKeep     = "$keep"
	opCoalesce = "$coalesce"
	opIf       = "$if"
	opMap      = "$map"
	opMerge    = "$merge"
	opConcat   = "$concat"
	opJSON     = "$json"
)

var knownOps = map[string]bool{
	opRef: true, opLiteral: true, opKeep: true, opCoalesce: true,
	opIf: true, opMap: true, opMerge: true, opConcat: true, opJSON: true,
}

// rootVars 是宿主注入的全部根变量。它是封闭的，所以写错 `$.mesages` 在
// **加载期**就能报错，而不是运行时取到 nil 后静默少发一个字段——这是相
// 对"任意 dot-path 字符串"的实质改进。
var rootVars = map[string]bool{
	"model": true, "messages": true, "system": true, "tools": true,
	"max_tokens": true, "temperature": true, "stream": true,
	"options": true, "cred": true,
	// embed 段专用
	"texts": true,
}

// scope 是一次求值的变量表。$map 会在其上叠一层元素变量。
type scope struct {
	vars   map[string]any
	parent *scope
	alias  string
	item   any
}

func newScope(vars map[string]any) *scope { return &scope{vars: vars} }

func (s *scope) child(alias string, item any) *scope {
	return &scope{parent: s, alias: alias, item: item}
}

func (s *scope) root(name string) (any, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.alias == name {
			return cur.item, true
		}
		if cur.vars != nil {
			v, ok := cur.vars[name]
			return v, ok
		}
	}
	return nil, false
}

// eval 求值一个模板节点。
//
// 对象的键在结果为空时**默认丢弃**（omitempty 是默认行为，不是每个字段
// 都要包一层 $omitEmpty）；确实要发一个 null / 空串时用 {"$keep": ...}。
// 注意 false 和 0 不算空——把 temperature: 0 丢掉是错的。
func eval(node any, s *scope) (any, error) {
	switch n := node.(type) {
	case string:
		if path, ok := strings.CutPrefix(n, "$."); ok {
			return resolvePath(s, path)
		}
		return n, nil

	case []any:
		out := make([]any, 0, len(n))
		for _, item := range n {
			v, err := eval(item, s)
			if err != nil {
				return nil, err
			}
			// 数组元素不做 omitempty：一个位置敏感的数组里静默少一项，
			// 比多一个 null 难查得多。
			out = append(out, unwrap(v))
		}
		return out, nil

	case map[string]any:
		if op, arg, ok := operatorOf(n); ok {
			return evalOperator(op, arg, s)
		}
		out := make(map[string]any, len(n))
		for _, key := range sortedKeys(n) {
			v, err := eval(n[key], s)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			if isEmpty(v) {
				continue
			}
			out[key] = unwrap(v)
		}
		return out, nil

	default:
		return node, nil
	}
}

// operatorOf 认出"这个对象是一个算子调用"——恰好一个键且以 $ 开头。混着
// 普通键的对象不是算子，直接当普通对象处理。
func operatorOf(m map[string]any) (op string, arg any, ok bool) {
	if len(m) != 1 {
		return "", nil, false
	}
	for k, v := range m {
		if strings.HasPrefix(k, "$") {
			return k, v, true
		}
	}
	return "", nil, false
}

func evalOperator(op string, arg any, s *scope) (any, error) {
	switch op {
	case opLiteral:
		return arg, nil

	case opKeep:
		// $keep 的作用只在外层的 omitempty 判断上：这里照常求值，由
		// eval 的对象分支通过 keepMarker 识别"这个键即使空也要留"。
		v, err := eval(arg, s)
		if err != nil {
			return nil, err
		}
		return keepMarker{v}, nil

	case opRef:
		path, ok := arg.(string)
		if !ok {
			return nil, fmt.Errorf("$ref 的参数必须是字符串路径")
		}
		return resolvePath(s, strings.TrimPrefix(path, "$."))

	case opCoalesce:
		items, ok := arg.([]any)
		if !ok {
			return nil, fmt.Errorf("$coalesce 的参数必须是数组")
		}
		for _, item := range items {
			v, err := eval(item, s)
			if err != nil {
				return nil, err
			}
			if !isEmpty(v) {
				return v, nil
			}
		}
		return nil, nil

	case opIf:
		spec, ok := arg.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$if 的参数必须是对象")
		}
		cond, err := eval(spec["cond"], s)
		if err != nil {
			return nil, err
		}
		branch := "else"
		if truthy(cond) {
			branch = "then"
		}
		if _, present := spec[branch]; !present {
			return nil, nil
		}
		return eval(spec[branch], s)

	case opMap:
		spec, ok := arg.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$map 的参数必须是对象")
		}
		src, err := eval(spec["each"], s)
		if err != nil {
			return nil, err
		}
		items, ok := src.([]any)
		if !ok {
			if src == nil {
				return nil, nil
			}
			return nil, fmt.Errorf("$map.each 求值结果不是数组")
		}
		alias, _ := spec["as"].(string)
		if alias == "" {
			alias = "item"
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			v, err := eval(spec["do"], s.child(alias, item))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil

	case opMerge:
		items, ok := arg.([]any)
		if !ok {
			return nil, fmt.Errorf("$merge 的参数必须是数组")
		}
		out := map[string]any{}
		for _, item := range items {
			v, err := eval(item, s)
			if err != nil {
				return nil, err
			}
			m, ok := v.(map[string]any)
			if !ok {
				if v == nil {
					continue
				}
				return nil, fmt.Errorf("$merge 的每一项都必须求值成对象")
			}
			for k, val := range m {
				out[k] = val
			}
		}
		return out, nil

	case opConcat:
		items, ok := arg.([]any)
		if !ok {
			return nil, fmt.Errorf("$concat 的参数必须是数组")
		}
		var b strings.Builder
		for _, item := range items {
			v, err := eval(item, s)
			if err != nil {
				return nil, err
			}
			b.WriteString(toString(v))
		}
		return b.String(), nil

	case opJSON:
		v, err := eval(arg, s)
		if err != nil {
			return nil, err
		}
		if isEmpty(v) {
			return nil, nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(raw), nil
	}
	return nil, fmt.Errorf("未知算子 %s", op)
}

// keepMarker 包住一个"即使为空也要保留"的值。它只在 eval 的对象分支里被
// 拆开，不会漏到渲染结果里。
type keepMarker struct{ value any }

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case keepMarker:
		return false
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

func unwrap(v any) any {
	if k, ok := v.(keepMarker); ok {
		return unwrap(k.value)
	}
	return v
}

func truthy(v any) bool {
	switch t := unwrap(v).(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}

func toString(v any) string {
	switch t := unwrap(v).(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func toInt64(v any) int64 {
	switch t := unwrap(v).(type) {
	case float64:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	}
	return 0
}

// resolvePath 走一条 dot path。数字段既可以索引数组，也可以当对象的键——
// 上游偶尔用数字字符串做键，两种都试比报错友好。
func resolvePath(s *scope, path string) (any, error) {
	segments := splitPath(path)
	if len(segments) == 0 {
		return nil, fmt.Errorf("空路径")
	}
	cur, ok := s.root(segments[0])
	if !ok {
		return nil, nil
	}
	return walk(cur, segments[1:]), nil
}

// lookup 在一坨已解码的响应/事件负载上走路径，不涉及变量表。响应映射和
// 状态机里的路径都走这里；允许带 `$.` 前缀，写不写都一样。
func lookup(root any, path string) any {
	path = strings.TrimPrefix(path, "$.")
	if path == "" {
		return nil
	}
	return walk(root, splitPath(path))
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

func walk(cur any, segments []string) any {
	for _, seg := range segments {
		if cur == nil {
			return nil
		}
		switch node := cur.(type) {
		case map[string]any:
			cur = node[seg]
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil
			}
			cur = node[idx]
		default:
			return nil
		}
	}
	return cur
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateTemplate 是加载期校验：把模板走一遍，检查根变量在白名单里、算
// 子是已知的。这是描述符体系"错了在装的时候就报错"的第一道关。
func validateTemplate(node any, path string, extraVars map[string]bool, errs *[]string) {
	switch n := node.(type) {
	case string:
		if ref, ok := strings.CutPrefix(n, "$."); ok {
			validateRef(ref, path, extraVars, errs)
		}
	case []any:
		for i, item := range n {
			validateTemplate(item, fmt.Sprintf("%s[%d]", path, i), extraVars, errs)
		}
	case map[string]any:
		if op, arg, ok := operatorOf(n); ok {
			if !knownOps[op] {
				*errs = append(*errs, fmt.Sprintf("%s: 未知算子 %s", path, op))
				return
			}
			if op == opRef {
				if ref, ok := arg.(string); ok {
					validateRef(strings.TrimPrefix(ref, "$."), path, extraVars, errs)
				}
				return
			}
			if op == opLiteral {
				return // $literal 的内容不求值，不校验
			}
			if op == opMap {
				spec, _ := arg.(map[string]any)
				alias, _ := spec["as"].(string)
				if alias == "" {
					alias = "item"
				}
				inner := map[string]bool{alias: true}
				for k := range extraVars {
					inner[k] = true
				}
				validateTemplate(spec["each"], path+".each", extraVars, errs)
				validateTemplate(spec["do"], path+".do", inner, errs)
				return
			}
			validateTemplate(arg, path+"."+op, extraVars, errs)
			return
		}
		for _, key := range sortedKeys(n) {
			validateTemplate(n[key], path+"."+key, extraVars, errs)
		}
	}
}

func validateRef(ref, path string, extraVars map[string]bool, errs *[]string) {
	root := ref
	if i := strings.IndexByte(ref, '.'); i >= 0 {
		root = ref[:i]
	}
	if rootVars[root] || extraVars[root] {
		return
	}
	*errs = append(*errs, fmt.Sprintf("%s: 未知变量 $.%s（可用变量见设计文档 3.2）", path, root))
}
