//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func main() {
	js.Global().Set("gothubHighlight", js.FuncOf(highlight))
	js.Global().Set("gothubExtractEntities", js.FuncOf(extractEntities))
	js.Global().Set("gothubDiffEntities", js.FuncOf(diffEntities))
	js.Global().Set("gothubSupportedLanguages", js.FuncOf(supportedLanguages))

	// Block forever — WASM module stays alive
	select {}
}

// highlight(filename, source) → JSON array of {start_byte, end_byte, capture}
func highlight(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return jsError("highlight requires (filename, source)")
	}
	filename := args[0].String()
	source := []byte(args[1].String())

	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		return jsJSON([]any{}) // unsupported language — return empty
	}

	lang := entry.Language()
	if lang == nil || entry.HighlightQuery == "" {
		return jsJSON([]any{})
	}

	hlOpts := []gotreesitter.HighlighterOption{}
	if entry.TokenSourceFactory != nil {
		hlOpts = append(hlOpts, gotreesitter.WithTokenSourceFactory(
			func(src []byte) gotreesitter.TokenSource {
				return entry.TokenSourceFactory(src, lang)
			},
		))
	}

	highlighter, err := gotreesitter.NewHighlighter(lang, entry.HighlightQuery, hlOpts...)
	if err != nil {
		return jsError(err.Error())
	}

	ranges := highlighter.Highlight(source)

	type hlRange struct {
		StartByte uint32 `json:"start_byte"`
		EndByte   uint32 `json:"end_byte"`
		Capture   string `json:"capture"`
	}
	result := make([]hlRange, len(ranges))
	for i, r := range ranges {
		result[i] = hlRange{
			StartByte: r.StartByte,
			EndByte:   r.EndByte,
			Capture:   r.Capture,
		}
	}
	return jsJSON(result)
}

// extractEntities(filename, source) → JSON array of entities
func extractEntities(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return jsError("extractEntities requires (filename, source)")
	}
	filename := args[0].String()
	source := []byte(args[1].String())

	el, err := entity.Extract(filename, source)
	if err != nil {
		return jsError(err.Error())
	}

	type entityInfo struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		DeclKind  string `json:"decl_kind"`
		Receiver  string `json:"receiver,omitempty"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Key       string `json:"key"`
	}

	kindNames := map[entity.EntityKind]string{
		entity.KindPreamble:     "preamble",
		entity.KindImportBlock:  "import",
		entity.KindDeclaration:  "declaration",
		entity.KindInterstitial: "interstitial",
	}

	result := make([]entityInfo, len(el.Entities))
	for i, e := range el.Entities {
		result[i] = entityInfo{
			Kind:      kindNames[e.Kind],
			Name:      e.Name,
			DeclKind:  e.DeclKind,
			Receiver:  e.Receiver,
			StartLine: e.StartLine,
			EndLine:   e.EndLine,
			Key:       e.IdentityKey(),
		}
	}
	return jsJSON(result)
}

// diffEntities(filename, before, after) → JSON of {path, changes: [...]}
func diffEntities(this js.Value, args []js.Value) any {
	if len(args) < 3 {
		return jsError("diffEntities requires (filename, before, after)")
	}
	filename := args[0].String()
	before := []byte(args[1].String())
	after := []byte(args[2].String())

	fd, err := diff.DiffFiles(filename, before, after)
	if err != nil {
		return jsError(err.Error())
	}

	changeTypeNames := map[diff.ChangeType]string{
		diff.Added:    "added",
		diff.Removed:  "removed",
		diff.Modified: "modified",
	}

	type changeInfo struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}

	changes := make([]changeInfo, len(fd.Changes))
	for i, c := range fd.Changes {
		changes[i] = changeInfo{
			Type: changeTypeNames[c.Type],
			Key:  c.Key,
		}
	}

	return jsJSON(map[string]any{
		"path":    fd.Path,
		"changes": changes,
	})
}

// supportedLanguages() → JSON array of {name, extensions}
func supportedLanguages(this js.Value, args []js.Value) any {
	langs := grammars.AllLanguages()
	type langInfo struct {
		Name       string   `json:"name"`
		Extensions []string `json:"extensions"`
	}
	result := make([]langInfo, len(langs))
	for i, l := range langs {
		result[i] = langInfo{Name: l.Name, Extensions: l.Extensions}
	}
	return jsJSON(result)
}

func jsJSON(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return jsError(err.Error())
	}
	return string(data)
}

func jsError(msg string) any {
	data, _ := json.Marshal(map[string]string{"error": msg})
	return string(data)
}
