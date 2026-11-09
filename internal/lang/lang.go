package lang

import (
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

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
	python     = &Lang{Name: "Python", Comment: "#", ptr: tspy.Language}
	rust       = &Lang{Name: "Rust", Comment: "//", ptr: tsrust.Language}
)

var byExt = map[string]*Lang{
	".ts": typescript, ".mts": typescript, ".cts": typescript,
	".tsx": tsx,
	".js":  javascript, ".jsx": javascript, ".mjs": javascript, ".cjs": javascript,
	".go": golang,
	".py": python,
	".rs": rust,
}

// ForFile は対応していない拡張子なら nil を返す。
func ForFile(path string) *Lang {
	return byExt[strings.ToLower(filepath.Ext(path))]
}
