package service

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"

	"github.com/odvcencio/gothub/internal/models"
	"github.com/odvcencio/gts-suite/pkg/model"
)

const (
	symbolSearchBloomVersion             = 1
	symbolSearchBloomNGramMax            = 3
	symbolSearchBloomTargetFalsePositive = 0.02
	symbolSearchBloomMinBits             = 2048
	symbolSearchBloomMaxBits             = 1 << 20
	symbolSearchBloomMaxHashFunctions    = 8
)

// symbolSearchBloomFilter is a compact probabilistic set for negative plain-text symbol lookups.
type symbolSearchBloomFilter struct {
	Version uint8  `json:"version"`
	K       uint8  `json:"k"`
	Bits    []byte `json:"bits"`

	CoversName       bool `json:"covers_name,omitempty"`
	CoversSignature  bool `json:"covers_signature,omitempty"`
	CoversReceiver   bool `json:"covers_receiver,omitempty"`
	CoversDocComment bool `json:"covers_doc_comment,omitempty"`
}

type persistedRepoIndexObject struct {
	Index       *model.Index             `json:"index"`
	SymbolBloom *symbolSearchBloomFilter `json:"symbol_bloom,omitempty"`
}

func newSymbolSearchBloomFilter(tokenEstimate int) *symbolSearchBloomFilter {
	if tokenEstimate < 1 {
		tokenEstimate = 1
	}
	mBits := int(math.Ceil(-float64(tokenEstimate) * math.Log(symbolSearchBloomTargetFalsePositive) / (math.Ln2 * math.Ln2)))
	if mBits < symbolSearchBloomMinBits {
		mBits = symbolSearchBloomMinBits
	}
	if mBits > symbolSearchBloomMaxBits {
		mBits = symbolSearchBloomMaxBits
	}
	byteLen := (mBits + 7) / 8
	mBits = byteLen * 8

	k := int(math.Round((float64(mBits) / float64(tokenEstimate)) * math.Ln2))
	if k < 1 {
		k = 1
	}
	if k > symbolSearchBloomMaxHashFunctions {
		k = symbolSearchBloomMaxHashFunctions
	}

	return &symbolSearchBloomFilter{
		Version: symbolSearchBloomVersion,
		K:       uint8(k),
		Bits:    make([]byte, byteLen),
	}
}

func buildSymbolSearchBloomFromIndex(idx *model.Index) *symbolSearchBloomFilter {
	filter := newSymbolSearchBloomFilter(estimateBloomTokenCountFromIndex(idx))
	filter.CoversName = true
	filter.CoversSignature = true
	filter.CoversReceiver = true

	if idx == nil {
		return filter
	}
	for _, file := range idx.Files {
		for _, sym := range file.Symbols {
			addSearchableTextTokens(filter, sym.Name)
			addSearchableTextTokens(filter, sym.Signature)
			addSearchableTextTokens(filter, sym.Receiver)
		}
	}
	return filter
}

func buildSymbolSearchBloomFromEntries(entries []models.EntityIndexEntry) *symbolSearchBloomFilter {
	filter := newSymbolSearchBloomFilter(estimateBloomTokenCountFromEntries(entries))
	filter.CoversName = true
	filter.CoversSignature = true
	filter.CoversReceiver = true
	filter.CoversDocComment = true

	for _, entry := range entries {
		addSearchableTextTokens(filter, entry.Name)
		addSearchableTextTokens(filter, entry.Signature)
		addSearchableTextTokens(filter, entry.Receiver)
		addSearchableTextTokens(filter, entry.DocComment)
	}
	return filter
}

func (f *symbolSearchBloomFilter) supportsEntityTextSearch() bool {
	return f != nil && f.CoversName && f.CoversSignature && f.CoversDocComment
}

func (f *symbolSearchBloomFilter) supportsIndexContainsSearch() bool {
	return f != nil && f.CoversName && f.CoversSignature && f.CoversReceiver
}

func bloomMightContainPlainTextQuery(filter *symbolSearchBloomFilter, rawQuery string) bool {
	if filter == nil || filter.K == 0 || len(filter.Bits) == 0 {
		return true
	}
	query := normalizeSearchableText(rawQuery)
	if query == "" {
		return true
	}

	gramSize := symbolSearchBloomNGramMax
	if len(query) < gramSize {
		gramSize = len(query)
	}

	for i := 0; i+gramSize <= len(query); i++ {
		if !filter.mightContain(bloomNGramToken(gramSize, query[i:i+gramSize])) {
			return false
		}
	}
	return true
}

func estimateBloomTokenCountFromIndex(idx *model.Index) int {
	if idx == nil {
		return 1
	}
	total := 0
	for _, file := range idx.Files {
		for _, sym := range file.Symbols {
			total += tokenCountForSearchableText(sym.Name)
			total += tokenCountForSearchableText(sym.Signature)
			total += tokenCountForSearchableText(sym.Receiver)
		}
	}
	if total < 1 {
		return 1
	}
	return total
}

func estimateBloomTokenCountFromEntries(entries []models.EntityIndexEntry) int {
	total := 0
	for _, entry := range entries {
		total += tokenCountForSearchableText(entry.Name)
		total += tokenCountForSearchableText(entry.Signature)
		total += tokenCountForSearchableText(entry.Receiver)
		total += tokenCountForSearchableText(entry.DocComment)
	}
	if total < 1 {
		return 1
	}
	return total
}

func tokenCountForSearchableText(raw string) int {
	text := normalizeSearchableText(raw)
	if text == "" {
		return 0
	}

	total := 0
	for n := 1; n <= symbolSearchBloomNGramMax; n++ {
		if len(text) < n {
			break
		}
		total += len(text) - n + 1
	}
	return total
}

func normalizeSearchableText(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func addSearchableTextTokens(filter *symbolSearchBloomFilter, raw string) {
	if filter == nil {
		return
	}
	text := normalizeSearchableText(raw)
	if text == "" {
		return
	}

	for n := 1; n <= symbolSearchBloomNGramMax; n++ {
		if len(text) < n {
			break
		}
		for i := 0; i+n <= len(text); i++ {
			filter.add(bloomNGramToken(n, text[i:i+n]))
		}
	}
}

func bloomNGramToken(size int, gram string) string {
	return string([]byte{byte('0' + size), ':'}) + gram
}

func (f *symbolSearchBloomFilter) add(value string) {
	if f == nil || f.K == 0 || len(f.Bits) == 0 || strings.TrimSpace(value) == "" {
		return
	}
	modulus := uint64(len(f.Bits) * 8)
	h1, h2 := bloomHashPair(value)
	for i := uint8(0); i < f.K; i++ {
		bit := (h1 + uint64(i)*h2) % modulus
		f.Bits[bit/8] |= byte(1 << (bit % 8))
	}
}

func (f *symbolSearchBloomFilter) mightContain(value string) bool {
	if f == nil || f.K == 0 || len(f.Bits) == 0 || strings.TrimSpace(value) == "" {
		return true
	}
	modulus := uint64(len(f.Bits) * 8)
	h1, h2 := bloomHashPair(value)
	for i := uint8(0); i < f.K; i++ {
		bit := (h1 + uint64(i)*h2) % modulus
		if f.Bits[bit/8]&(byte(1<<(bit%8))) == 0 {
			return false
		}
	}
	return true
}

func bloomHashPair(value string) (uint64, uint64) {
	sum := sha256.Sum256([]byte(value))
	h1 := binary.LittleEndian.Uint64(sum[0:8])
	h2 := binary.LittleEndian.Uint64(sum[8:16])
	if h2 == 0 {
		h2 = binary.LittleEndian.Uint64(sum[16:24])
	}
	if h2 == 0 {
		h2 = 0x9e3779b97f4a7c15
	}
	if h2%2 == 0 {
		h2++
	}
	return h1, h2
}
