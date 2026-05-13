// Package locator finds a named TypeScript symbol in a source buffer and
// returns its AST range. It supports four shapes: top-level function
// declarations, methods on a class, arrow-function consts, and any of the
// above wrapped in `export` / `export default`. When the symbol exists more
// than once (e.g. a method name reused across classes), the first occurrence
// in source order wins; Phase 3 of SPEC.md is where disambiguation lands.
package locator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// SchemaVersion identifies the extraction logic the locator is currently
// shipping. Bump this when Kind values, byte-range semantics, or any other
// observable Symbol field changes; downstream caches (e.g. internal/cache)
// include it in their key space so older entries are invalidated.
const SchemaVersion = 1

type Kind int

const (
	KindFunction   Kind = iota // function foo() {}   (incl. export / export default)
	KindMethod                 // class C { foo() {} }
	KindArrowConst             // const foo = () => {} (incl. export const)
)

func (k Kind) String() string {
	switch k {
	case KindFunction:
		return "function"
	case KindMethod:
		return "method"
	case KindArrowConst:
		return "arrow-const"
	}
	return "unknown"
}

type Symbol struct {
	Name      string
	Kind      Kind
	StartByte uint32
	EndByte   uint32
	StartLine uint32 // 1-indexed
	EndLine   uint32 // 1-indexed
}

type NotFoundError struct {
	Name        string
	Suggestions []string
}

func (e *NotFoundError) Error() string {
	if len(e.Suggestions) == 0 {
		return fmt.Sprintf("symbol %q not found", e.Name)
	}
	return fmt.Sprintf("symbol %q not found; did you mean: %s?", e.Name, strings.Join(e.Suggestions, ", "))
}

func Locate(source []byte, name string) (Symbol, error) {
	matches, err := parseAndCollect(source)
	if err != nil {
		return Symbol{}, err
	}
	for _, s := range matches {
		if s.Name == name {
			return s, nil
		}
	}
	names := make([]string, 0, len(matches))
	for _, s := range matches {
		names = append(names, s.Name)
	}
	return Symbol{}, &NotFoundError{Name: name, Suggestions: suggest(name, names)}
}

// Enumerate returns every supported symbol in the source, in source order.
// When the same name appears more than once (e.g. a method name reused across
// classes), each occurrence is returned; callers wanting the first-wins
// behaviour should still use Locate.
func Enumerate(source []byte) ([]Symbol, error) {
	return parseAndCollect(source)
}

func parseAndCollect(source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(typescript.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse typescript source: %w", err)
	}
	defer tree.Close()

	var matches []Symbol
	var names []string
	collect(tree.RootNode(), source, nil, &matches, &names)
	return matches, nil
}

func collect(n *sitter.Node, src []byte, outer *sitter.Node, matches *[]Symbol, names *[]string) {
	if n == nil {
		return
	}

	descendOuter := (*sitter.Node)(nil)

	switch n.Type() {
	case "export_statement":
		count := int(n.NamedChildCount())
		for i := 0; i < count; i++ {
			collect(n.NamedChild(i), src, n, matches, names)
		}
		return

	case "function_declaration", "generator_function_declaration":
		if name := nodeName(n, "name", src); name != "" {
			*names = append(*names, name)
			*matches = append(*matches, symbolFrom(name, KindFunction, n, outer))
		}

	case "method_definition":
		if name := nodeName(n, "name", src); name != "" {
			*names = append(*names, name)
			*matches = append(*matches, symbolFrom(name, KindMethod, n, nil))
		}

	case "lexical_declaration", "variable_declaration":
		count := int(n.NamedChildCount())
		for i := 0; i < count; i++ {
			child := n.NamedChild(i)
			if child == nil || child.Type() != "variable_declarator" {
				continue
			}
			value := child.ChildByFieldName("value")
			if value == nil || value.Type() != "arrow_function" {
				continue
			}
			name := nodeName(child, "name", src)
			if name == "" {
				continue
			}
			*names = append(*names, name)
			*matches = append(*matches, symbolFrom(name, KindArrowConst, n, outer))
		}
	}

	count := int(n.NamedChildCount())
	for i := 0; i < count; i++ {
		collect(n.NamedChild(i), src, descendOuter, matches, names)
	}
}

func symbolFrom(name string, kind Kind, inner, outer *sitter.Node) Symbol {
	n := inner
	if outer != nil {
		n = outer
	}
	return Symbol{
		Name:      name,
		Kind:      kind,
		StartByte: n.StartByte(),
		EndByte:   n.EndByte(),
		StartLine: n.StartPoint().Row + 1,
		EndLine:   n.EndPoint().Row + 1,
	}
}

func nodeName(n *sitter.Node, field string, src []byte) string {
	child := n.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return child.Content(src)
}

func suggest(target string, names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, n := range names {
		if seen[n] || n == target {
			continue
		}
		seen[n] = true
		unique = append(unique, n)
	}
	threshold := len(target) / 3
	if threshold < 2 {
		threshold = 2
	}
	type ranked struct {
		name string
		dist int
	}
	var picks []ranked
	for _, n := range unique {
		d := Levenshtein(target, n)
		if d <= threshold {
			picks = append(picks, ranked{n, d})
		}
	}
	sort.Slice(picks, func(i, j int) bool {
		if picks[i].dist != picks[j].dist {
			return picks[i].dist < picks[j].dist
		}
		return picks[i].name < picks[j].name
	})
	if len(picks) > 3 {
		picks = picks[:3]
	}
	out := make([]string, len(picks))
	for i, p := range picks {
		out[i] = p.name
	}
	return out
}

// Levenshtein is the standard edit distance between two strings, in runes.
// Exported so other packages (e.g. internal/history) can reuse it without
// duplicating the implementation.
func Levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
