package lang

import (
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	hcl "github.com/tree-sitter-grammars/tree-sitter-hcl/bindings/go"
	tsyaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type Lang struct {
	Name    string
	Comment string
	// KindNames はヒントに出すノード種別の言い換え。同じ種別名でも言語によって意味が違うため、言語ごとに持つ
	KindNames map[string]string

	ptr  func() unsafe.Pointer
	once sync.Once
	lang *sitter.Language
}

func (l *Lang) language() *sitter.Language {
	// 使わない言語の grammar まで初期化しないよう、最初に使うときに作る
	l.once.Do(func() { l.lang = sitter.NewLanguage(l.ptr()) })
	return l.lang
}

// Parse の戻り値は C 側のメモリを持つので、呼び出し側で Close する。
func (l *Lang) Parse(src []byte) *sitter.Tree {
	p := sitter.NewParser()
	defer p.Close()
	_ = p.SetLanguage(l.language())
	return p.Parse(src, nil)
}

var (
	typescript = &Lang{Name: "TypeScript", Comment: "//", ptr: tsts.LanguageTypescript}
	tsx        = &Lang{Name: "TSX", Comment: "//", ptr: tsts.LanguageTSX}
	javascript = &Lang{Name: "JavaScript", Comment: "//", ptr: tsjs.Language}
	golang     = &Lang{Name: "Go", Comment: "//", ptr: tsgo.Language}
	python     = &Lang{Name: "Python", Comment: "#", ptr: tspy.Language,
		KindNames: map[string]string{"attribute": "attribute access", "call": "function call"}}
	rust = &Lang{Name: "Rust", Comment: "//", ptr: tsrust.Language}
	// Terraform 専用の方言は Go のバインディングが無いので、素の HCL で解析する。Terraform の構文は HCL そのものなので困らない
	terraform = &Lang{Name: "HCL", Comment: "#", ptr: hcl.Language,
		KindNames: map[string]string{
			"attribute": "argument", "variable_expr": "reference", "function_call": "function call",
			"literal_value": "literal value", "template_expr": "string template",
			"collection_value": "list or map", "for_expr": "for expression",
		}}
	yaml = &Lang{Name: "YAML", Comment: "#", ptr: tsyaml.Language,
		KindNames: map[string]string{
			"plain_scalar": "value", "double_quote_scalar": "quoted string",
			"single_quote_scalar": "quoted string", "flow_sequence": "list",
			"flow_mapping": "map", "block_mapping_pair": "key: value pair",
			"block_sequence_item": "list item",
		}}
)

// IsHCL は HCL 固有の候補の選び方を使うかどうかを返す。
func (l *Lang) IsHCL() bool { return l == terraform }

// IsYAML は YAML 固有の候補の選び方を使うかどうかを返す。
func (l *Lang) IsYAML() bool { return l == yaml }

// IsIndentBlock はインデントでブロックを作る言語かどうかを返す。
func (l *Lang) IsIndentBlock() bool { return l == python }

// KindName はヒント用に種別名を言い換える。
func (l *Lang) KindName(kind string) string {
	if n, ok := l.KindNames[kind]; ok {
		return n
	}
	return kind
}

var byExt = map[string]*Lang{
	".ts": typescript, ".mts": typescript, ".cts": typescript,
	".tsx": tsx,
	".js":  javascript, ".jsx": javascript, ".mjs": javascript, ".cjs": javascript,
	".go": golang,
	".py": python,
	".rs": rust,
	".tf": terraform, ".tfvars": terraform, ".hcl": terraform,
	".yaml": yaml, ".yml": yaml,
}

// ForFile は対応していない拡張子なら nil を返す。
func ForFile(path string) *Lang {
	return byExt[strings.ToLower(filepath.Ext(path))]
}
