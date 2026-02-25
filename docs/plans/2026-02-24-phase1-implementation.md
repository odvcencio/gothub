# Phase 1: Fast & Smart Core — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Got becomes objectively faster than Git for common operations. GotHub stops blocking on pushes. Foundation is production-grade with full observability.

**Architecture:** Six parallel workstreams — (A) Pack file engine in Got, (B) Async indexing pipeline in GotHub, (C) Code intelligence engine in GotHub, (D) Merge performance in Got+GotHub, (E) Observability, (F) PR intelligence UX in GotHub. A and B are foundational; C/D/E/F build on them.

**Tech Stack:** Go 1.25, SHA-256, Git v2 pack format, zlib/zstd, FTS5/tsvector, bloom filters, OpenTelemetry, pprof, Prometheus client_golang

**Dependency Graph:**
```
Task 1 (Pack: types)
  → Task 2 (Pack: zlib write)
    → Task 3 (Pack: zlib read)
      → Task 4 (Pack: idx write)
        → Task 5 (Pack: idx read/lookup)
          → Task 6 (Pack: delta compression)
            → Task 7 (Pack: delta read)
              → Task 8 (Pack: entity trailer)
                → Task 9 (Pack: gc command)
                  → Task 10 (Pack: verify command)
                    → Task 11 (Pack: Store integration)

Task 12 (Async: DB schema)
  → Task 13 (Async: job queue)
    → Task 14 (Async: worker pool)
      → Task 15 (Async: incremental extraction)
        → Task 16 (Async: push path refactor)
          → Task 17 (Async: index status API)

Task 18 (CodeIntel: entity_index table)
  → Task 19 (CodeIntel: FTS5/tsvector)
    → Task 20 (CodeIntel: symbol search API)
      → Task 21 (CodeIntel: bloom filters)
        → Task 22 (CodeIntel: xref graph)
          → Task 23 (CodeIntel: impact analysis API)
            → Task 24 (CodeIntel: semantic diff classification)

Task 25 (Merge: generation numbers)
  → Task 26 (Merge: LCA with pruning)
    → Task 27 (Merge: merge base cache)
      → Task 28 (Merge: preview cache)
        → Task 29 (Merge: parallel file merge)

Task 30 (Obs: OTel tracing)
  → Task 31 (Obs: Prometheus metrics)
    → Task 32 (Obs: pprof + health endpoint)
      → Task 33 (Obs: Got benchmarks)
        → Task 34 (Obs: GotHub benchmarks)

Task 40 (PR UX: impact summary card)
  → Task 41 (PR UX: semver recommendation card)
    → Task 42 (PR UX: entity owner approval checklist)

Task 43 (Got CLI: structural blame)
  → Task 44 (Got CLI: entity-level log)
    → Task 45 (Got core: entity rename tracking)
      → Task 46 (Got CLI: cherry-pick by entity)

Task 47 (GotHub UI: structural blame panel)
  → Task 48 (GotHub UI: visual call graph)
    → Task 49 (GotHub UI: inline code intelligence hover/nav)
      → Task 50 (GotHub UX: demo onboarding repository flow)
```

## Execution Tracker (Live)

Updated: 2026-02-25 17:45 PST

### Completed

| Task | Status | Repo | Commit | Verification |
|------|--------|------|--------|--------------|
| 1 | complete | got | `d14d240` | `go test ./pkg/object -run TestPack -v`, `go test ./...` |
| 2 | complete | got | `e27ceeb` | `go test ./pkg/object -run TestPackWriter -v`, `go test ./...` |
| 3 | complete | got | `251b62d` | `go test ./pkg/object -run TestReadPack -v`, `go test ./...` |
| 4 | complete | got | `4b916f1` | `go test ./pkg/object -run TestWritePackIndex -v`, `go test ./...` |
| 5 | complete | got | `eed3d13` | `go test ./pkg/object -run 'TestReadPackIndex|TestReadPackIndexFromReader' -v`, `go test ./...` |
| 6 | complete | got | `5c4f60f` | `go test ./pkg/object -run 'TestOfsDelta|TestBuildInsertOnlyDelta|TestPackWriterWriteOfsDelta' -v`, `go test ./...` |
| 11 | complete | got | `c3b2d67` | `go test ./pkg/object -run TestStoreHasChecksPackedObjects -v`, `go test ./...` |
| 13 | complete | gothub | `d678897` | `go test ./internal/jobs -run TestWorkerPool -v`, `go test ./...` |
| 40 | complete | gothub | `2930c83` | `frontend/src/views/PRDetail.tsx` (`PRImpactSummary`, `summarizeDiff`) |
| 41 | complete | gothub | `2930c83` | `frontend/src/views/PRDetail.tsx` (`SemverPanel`), `frontend/src/api/client.ts` (`getSemver`) |
| 42 | complete | gothub | `2930c83` | `frontend/src/views/PRDetail.tsx` (`MergeGatePanel`), `internal/service/policy.go` (`entity_owner_approvals`) |
| 43 | complete | got | `5178221` | `go test ./cmd/got ./pkg/repo` |
| 44 | complete | got | `b655d12` | `go test ./cmd/got ./pkg/repo` |
| 45 | complete | got | `fad365d` | `go test ./pkg/repo -run 'TestBlame|TestLogEntity'` |
| 46 | complete | got | `b7302c5` | `go test ./cmd/got -run TestCherryPickEntity` |
| 47 | complete | gothub | `2614e72` | `frontend/src/views/Code.tsx`, `internal/api/browse_handlers.go`, `internal/api/router.go` |
| 48 | complete | gothub | `a4a6969` | `frontend/src/views/CallGraph.tsx` |
| 49 | complete | gothub | `5d2e39c` | `frontend/src/components/CodeViewer.tsx`, `frontend/src/api/client.ts` |
| 50 | complete | gothub | `72cb54f` | `frontend/src/views/Home.tsx` (guided 4-step onboarding demo) |

### In Progress

| Task | Status | Notes |
|------|--------|-------|
| 7 | next | implement pack reader delta resolution (`OFS_DELTA` / `REF_DELTA`) |
| 14 | partial | unchanged-tree fast-path landed (`entity_versions` copied from parent commit instead of full extraction) |

### Remaining (Not Yet Implemented)

- Open core gaps: 7, 8, 9, 10 (pack/gc hardening), 14 (full incremental extraction still pending), 24, 25, 26, 27 (merge-base perf/cache), 33, 34 (bench suites).
- Tasks 35-39 are queued post-Phase-1 cloud multi-tenancy work.

---

## Workstream A: Git-Compatible Pack File Engine (Got)

All files in `/home/draco/work/got/`.

### Task 1: Pack File Types and Constants

**Files:**
- Create: `pkg/object/pack.go`
- Test: `pkg/object/pack_test.go`

**Context:** Define the Git v2 pack format structures. The Git pack format uses a 12-byte header (`PACK` magic + version + object count), followed by compressed object entries, followed by a 20-byte SHA-1 checksum (we use SHA-256, 32 bytes). Each entry has a variable-length header encoding object type and uncompressed size.

**Step 1: Write the failing test**

```go
// pkg/object/pack_test.go
package object

import (
	"testing"
)

func TestPackHeader(t *testing.T) {
	h := PackHeader{
		Version:    2,
		NumObjects: 42,
	}
	data := h.Marshal()
	if len(data) != 12 {
		t.Fatalf("expected 12 bytes, got %d", len(data))
	}
	got, err := UnmarshalPackHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.NumObjects != 42 {
		t.Fatalf("round-trip failed: %+v", got)
	}
}

func TestPackHeaderMagic(t *testing.T) {
	bad := []byte("JUNK00000000")
	_, err := UnmarshalPackHeader(bad)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}

func TestPackEntryTypeEncoding(t *testing.T) {
	tests := []struct {
		objType PackObjectType
		size    uint64
	}{
		{PackBlob, 0},
		{PackCommit, 127},
		{PackTree, 256},
		{PackBlob, 1 << 20},
		{PackOfsDelta, 100},
		{PackRefDelta, 100},
	}
	for _, tt := range tests {
		data := encodePackEntryHeader(tt.objType, tt.size)
		gotType, gotSize, n := decodePackEntryHeader(data)
		if gotType != tt.objType || gotSize != tt.size {
			t.Errorf("type=%d size=%d: got type=%d size=%d (consumed %d bytes)", tt.objType, tt.size, gotType, gotSize, n)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPack -v`
Expected: FAIL — types not defined

**Step 3: Write minimal implementation**

```go
// pkg/object/pack.go
package object

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Git pack object types (3-bit encoding in entry header).
type PackObjectType uint8

const (
	PackCommit   PackObjectType = 1
	PackTree     PackObjectType = 2
	PackBlob     PackObjectType = 3
	PackTag      PackObjectType = 4
	PackOfsDelta PackObjectType = 6
	PackRefDelta PackObjectType = 7
)

// PackHeader is the 12-byte header at the start of a Git v2 pack file.
//
//	Bytes 0-3:  "PACK"
//	Bytes 4-7:  version (network byte order, must be 2)
//	Bytes 8-11: number of objects (network byte order)
type PackHeader struct {
	Version    uint32
	NumObjects uint32
}

var packMagic = [4]byte{'P', 'A', 'C', 'K'}

func (h *PackHeader) Marshal() []byte {
	buf := make([]byte, 12)
	copy(buf[0:4], packMagic[:])
	binary.BigEndian.PutUint32(buf[4:8], h.Version)
	binary.BigEndian.PutUint32(buf[8:12], h.NumObjects)
	return buf
}

func UnmarshalPackHeader(data []byte) (*PackHeader, error) {
	if len(data) < 12 {
		return nil, errors.New("pack header too short")
	}
	if string(data[0:4]) != "PACK" {
		return nil, fmt.Errorf("bad pack magic: %q", data[0:4])
	}
	v := binary.BigEndian.Uint32(data[4:8])
	if v != 2 {
		return nil, fmt.Errorf("unsupported pack version: %d", v)
	}
	return &PackHeader{
		Version:    v,
		NumObjects: binary.BigEndian.Uint32(data[8:12]),
	}, nil
}

// encodePackEntryHeader encodes the variable-length entry header used in Git packs.
// Format: first byte = (type << 4) | (size & 0x0F), continuation bytes for size >> 4.
// The MSB of each byte indicates whether more bytes follow.
func encodePackEntryHeader(objType PackObjectType, size uint64) []byte {
	b := byte(uint8(objType)<<4) | byte(size&0x0F)
	size >>= 4
	if size > 0 {
		b |= 0x80
	}
	result := []byte{b}
	for size > 0 {
		b = byte(size & 0x7F)
		size >>= 7
		if size > 0 {
			b |= 0x80
		}
		result = append(result, b)
	}
	return result
}

// decodePackEntryHeader decodes the variable-length entry header.
// Returns object type, uncompressed size, and number of bytes consumed.
func decodePackEntryHeader(data []byte) (PackObjectType, uint64, int) {
	b := data[0]
	objType := PackObjectType((b >> 4) & 0x07)
	size := uint64(b & 0x0F)
	shift := uint(4)
	i := 1
	for b&0x80 != 0 {
		b = data[i]
		size |= uint64(b&0x7F) << shift
		shift += 7
		i++
	}
	return objType, size, i
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPack -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/draco/work/got
git add pkg/object/pack.go pkg/object/pack_test.go
git commit -m "add(pack): Git v2 pack header and entry type encoding"
```

---

### Task 2: Pack Writer — Compressed Object Entries (zlib)

**Files:**
- Create: `pkg/object/pack_writer.go`
- Test: `pkg/object/pack_writer_test.go`

**Context:** Write a PackWriter that creates Git-compatible pack files. Each object entry is zlib-compressed. The pack ends with a SHA-256 checksum of all preceding bytes. This task handles non-delta objects only (full objects compressed with zlib).

**Step 1: Write the failing test**

```go
// pkg/object/pack_writer_test.go
package object

import (
	"bytes"
	"testing"
)

func TestPackWriterSingleBlob(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatal(err)
	}
	blobData := []byte("hello world")
	if err := pw.WriteEntry(PackBlob, blobData); err != nil {
		t.Fatal(err)
	}
	checksum, err := pw.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if checksum == "" {
		t.Fatal("expected non-empty checksum")
	}
	// Verify header
	data := buf.Bytes()
	hdr, err := UnmarshalPackHeader(data[:12])
	if err != nil {
		t.Fatal(err)
	}
	if hdr.NumObjects != 1 {
		t.Fatalf("expected 1 object, got %d", hdr.NumObjects)
	}
}

func TestPackWriterMultipleObjects(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := pw.WriteEntry(PackBlob, []byte("data")); err != nil {
			t.Fatal(err)
		}
	}
	_, err = pw.Finish()
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackWriterCountMismatch(t *testing.T) {
	var buf bytes.Buffer
	pw, _ := NewPackWriter(&buf, 2)
	pw.WriteEntry(PackBlob, []byte("one"))
	_, err := pw.Finish()
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPackWriter -v`
Expected: FAIL — NewPackWriter not defined

**Step 3: Write minimal implementation**

```go
// pkg/object/pack_writer.go
package object

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
)

// PackWriter writes Git v2 compatible pack files.
type PackWriter struct {
	w        io.Writer
	hasher   hash.Hash
	mw       io.Writer // multi-writer: w + hasher
	expected uint32
	written  uint32
}

// NewPackWriter creates a writer that emits a Git v2 pack.
// numObjects must match the number of WriteEntry calls before Finish.
func NewPackWriter(w io.Writer, numObjects uint32) (*PackWriter, error) {
	h := sha256.New()
	mw := io.MultiWriter(w, h)
	hdr := (&PackHeader{Version: 2, NumObjects: numObjects}).Marshal()
	if _, err := mw.Write(hdr); err != nil {
		return nil, err
	}
	return &PackWriter{
		w:        w,
		hasher:   h,
		mw:       mw,
		expected: numObjects,
	}, nil
}

// WriteEntry writes a single non-delta object entry.
func (pw *PackWriter) WriteEntry(objType PackObjectType, data []byte) error {
	header := encodePackEntryHeader(objType, uint64(len(data)))
	if _, err := pw.mw.Write(header); err != nil {
		return err
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(data); err != nil {
		zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if _, err := pw.mw.Write(compressed.Bytes()); err != nil {
		return err
	}
	pw.written++
	return nil
}

// Finish writes the trailing checksum and returns it as a hex string.
func (pw *PackWriter) Finish() (string, error) {
	if pw.written != pw.expected {
		return "", fmt.Errorf("pack: wrote %d objects but header declared %d", pw.written, pw.expected)
	}
	sum := pw.hasher.Sum(nil)
	if _, err := pw.w.Write(sum); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum), nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPackWriter -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/draco/work/got
git add pkg/object/pack_writer.go pkg/object/pack_writer_test.go
git commit -m "add(pack): zlib-compressed pack writer for Git v2 format"
```

---

### Task 3: Pack Reader — Decompress Object Entries

**Files:**
- Create: `pkg/object/pack_reader.go`
- Test: `pkg/object/pack_reader_test.go`

**Context:** Read pack files written by Task 2. Stream through entries, decompress each, and yield (type, data) pairs. Verify trailing checksum. This is the sequential scan reader — random access comes with the index in Task 5.

**Step 1: Write the failing test**

```go
// pkg/object/pack_reader_test.go
package object

import (
	"bytes"
	"testing"
)

func writeTestPack(t *testing.T, entries []packTestEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, uint32(len(entries)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := pw.WriteEntry(e.objType, e.data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type packTestEntry struct {
	objType PackObjectType
	data    []byte
}

func TestPackReaderRoundTrip(t *testing.T) {
	entries := []packTestEntry{
		{PackBlob, []byte("hello")},
		{PackCommit, []byte("tree abc\nauthor test\n\ncommit msg")},
		{PackTree, []byte("100644 file.go\x00abcdef")},
	}
	packData := writeTestPack(t, entries)

	pr, err := NewPackReader(bytes.NewReader(packData))
	if err != nil {
		t.Fatal(err)
	}
	if pr.NumObjects() != 3 {
		t.Fatalf("expected 3 objects, got %d", pr.NumObjects())
	}
	for i, want := range entries {
		gotType, gotData, err := pr.Next()
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if gotType != want.objType {
			t.Errorf("entry %d: type %d, want %d", i, gotType, want.objType)
		}
		if !bytes.Equal(gotData, want.data) {
			t.Errorf("entry %d: data mismatch", i)
		}
	}
	// Verify no more entries
	_, _, err = pr.Next()
	if err == nil {
		t.Fatal("expected EOF or error after last entry")
	}
}

func TestPackReaderChecksumValidation(t *testing.T) {
	packData := writeTestPack(t, []packTestEntry{{PackBlob, []byte("test")}})
	// Corrupt last byte of checksum
	packData[len(packData)-1] ^= 0xFF
	pr, err := NewPackReader(bytes.NewReader(packData))
	if err != nil {
		t.Fatal(err)
	}
	pr.Next() // read the one entry
	err = pr.Verify()
	if err == nil {
		t.Fatal("expected checksum error")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPackReader -v`
Expected: FAIL — NewPackReader not defined

**Step 3: Write minimal implementation**

```go
// pkg/object/pack_reader.go
package object

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// PackReader reads Git v2 pack files sequentially.
type PackReader struct {
	r          *bytes.Reader
	raw        []byte
	numObjects uint32
	read       uint32
	pos        int // current byte position after header
}

// NewPackReader parses the header and prepares for sequential entry reading.
func NewPackReader(r io.Reader) (*PackReader, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(raw) < 12+32 { // header + SHA-256 checksum
		return nil, errors.New("pack too small")
	}
	hdr, err := UnmarshalPackHeader(raw[:12])
	if err != nil {
		return nil, err
	}
	return &PackReader{
		r:          bytes.NewReader(raw),
		raw:        raw,
		numObjects: hdr.NumObjects,
		pos:        12,
	}, nil
}

func (pr *PackReader) NumObjects() uint32 { return pr.numObjects }

// Next returns the next entry's type and decompressed data.
func (pr *PackReader) Next() (PackObjectType, []byte, error) {
	if pr.read >= pr.numObjects {
		return 0, nil, errors.New("no more entries")
	}
	if pr.pos >= len(pr.raw)-32 {
		return 0, nil, errors.New("unexpected end of pack data")
	}
	objType, size, n := decodePackEntryHeader(pr.raw[pr.pos:])
	pr.pos += n

	// Decompress zlib data
	zr, err := zlib.NewReader(bytes.NewReader(pr.raw[pr.pos:]))
	if err != nil {
		return 0, nil, fmt.Errorf("zlib open: %w", err)
	}
	decompressed, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return 0, nil, fmt.Errorf("zlib read: %w", err)
	}
	if uint64(len(decompressed)) != size {
		return 0, nil, fmt.Errorf("size mismatch: header says %d, got %d", size, len(decompressed))
	}

	// Advance past compressed data by re-reading just enough zlib bytes.
	// We need to find where the zlib stream ends in the raw data.
	pr.pos += zlibCompressedSize(pr.raw[pr.pos:], decompressed)

	pr.read++
	return objType, decompressed, nil
}

// Verify checks the trailing SHA-256 checksum.
func (pr *PackReader) Verify() error {
	checksumStart := len(pr.raw) - 32
	h := sha256.New()
	h.Write(pr.raw[:checksumStart])
	expected := hex.EncodeToString(pr.raw[checksumStart:])
	actual := hex.EncodeToString(h.Sum(nil))
	if expected != actual {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

// zlibCompressedSize determines how many raw bytes the zlib stream consumes
// by re-compressing isn't reliable; instead we probe the zlib reader boundary.
func zlibCompressedSize(raw []byte, decompressed []byte) int {
	// Try increasing lengths of raw bytes until zlib decompresses successfully
	// and produces the right output. This is the reliable way to find the boundary.
	for size := 2; size <= len(raw); size++ {
		zr, err := zlib.NewReader(bytes.NewReader(raw[:size]))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(zr)
		zr.Close()
		if err == nil && bytes.Equal(data, decompressed) {
			return size
		}
	}
	return len(raw) // fallback
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPackReader -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/draco/work/got
git add pkg/object/pack_reader.go pkg/object/pack_reader_test.go
git commit -m "add(pack): sequential pack reader with zlib decompression and checksum verification"
```

---

### Task 4: Pack Index Writer (.idx v2)

**Files:**
- Create: `pkg/object/pack_index.go`
- Test: `pkg/object/pack_index_test.go`

**Context:** The `.idx` file enables O(1) lookups by hash into a pack file. Git v2 index format: 4-byte magic (`\377tOc`), 4-byte version (2), 256-entry fan-out table (1024 bytes), sorted hashes, CRC32 table, offset table, optional large offset table. We use SHA-256 (32 bytes per hash) instead of SHA-1 (20 bytes). This means our idx is not byte-compatible with Git's but structurally identical.

**Step 1: Write the failing test**

```go
// pkg/object/pack_index_test.go
package object

import (
	"bytes"
	"testing"
)

func TestPackIndexRoundTrip(t *testing.T) {
	// Create some objects and their pack offsets
	entries := []PackIndexEntry{
		{Hash: HashBytes([]byte("object-a")), Offset: 12, CRC32: 0xAABBCCDD},
		{Hash: HashBytes([]byte("object-b")), Offset: 150, CRC32: 0x11223344},
		{Hash: HashBytes([]byte("object-c")), Offset: 300, CRC32: 0x55667788},
	}
	packChecksum := "deadbeef" + "00000000000000000000000000000000000000000000000000000000"

	var buf bytes.Buffer
	err := WritePackIndex(&buf, entries, packChecksum)
	if err != nil {
		t.Fatal(err)
	}

	idx, err := ReadPackIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if idx.NumObjects() != 3 {
		t.Fatalf("expected 3 objects, got %d", idx.NumObjects())
	}

	// Lookup each entry
	for _, want := range entries {
		offset, found := idx.Lookup(want.Hash)
		if !found {
			t.Errorf("hash %s not found", want.Hash[:16])
			continue
		}
		if offset != want.Offset {
			t.Errorf("hash %s: offset %d, want %d", want.Hash[:16], offset, want.Offset)
		}
	}

	// Lookup nonexistent
	_, found := idx.Lookup(HashBytes([]byte("nonexistent")))
	if found {
		t.Error("found nonexistent hash")
	}
}

func TestPackIndexLargeOffset(t *testing.T) {
	bigOffset := uint64(1) << 33 // > 4GB, requires large offset table
	entries := []PackIndexEntry{
		{Hash: HashBytes([]byte("big")), Offset: bigOffset, CRC32: 0},
	}
	var buf bytes.Buffer
	err := WritePackIndex(&buf, entries, HashBytes([]byte("pack")).String())
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ReadPackIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	offset, found := idx.Lookup(entries[0].Hash)
	if !found || offset != bigOffset {
		t.Fatalf("large offset lookup: found=%v offset=%d", found, offset)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPackIndex -v`
Expected: FAIL — types not defined

**Step 3: Write implementation**

The index format implementation. The fan-out table provides O(1) bucket lookup, then binary search within the bucket. Write `PackIndexEntry`, `WritePackIndex`, `ReadPackIndex`, and `PackIndex.Lookup`.

Implementation details:
- Fan-out table: 256 uint32 entries, each = cumulative count of hashes with first byte <= i
- Sorted hashes: all N hashes sorted lexicographically (32 bytes each for SHA-256)
- CRC32 table: N uint32 entries
- Offset table: N uint32 entries (MSB set = index into large offset table)
- Large offset table: variable, uint64 entries for offsets >= 2^31

**Step 4: Run test to verify it passes**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPackIndex -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/draco/work/got
git add pkg/object/pack_index.go pkg/object/pack_index_test.go
git commit -m "add(pack): Git v2 pack index (.idx) writer and reader with O(1) lookup"
```

---

### Task 5: Pack Index Random Access

**Files:**
- Modify: `pkg/object/pack_reader.go`
- Test: `pkg/object/pack_reader_test.go` (add tests)

**Context:** Use the index to read a specific object from a pack by hash without scanning the whole file. Seek to offset, decode entry header, decompress.

**Step 1: Write the failing test**

```go
func TestPackRandomAccess(t *testing.T) {
	entries := []packTestEntry{
		{PackBlob, []byte("first object")},
		{PackBlob, []byte("second object")},
		{PackBlob, []byte("third object")},
	}
	packData := writeTestPack(t, entries)

	// Build an index by scanning
	idx, err := BuildPackIndex(bytes.NewReader(packData))
	if err != nil {
		t.Fatal(err)
	}

	// Random access: read second object by hash
	for _, want := range entries {
		wantHash := HashObject(TypeBlob, want.data)
		objType, data, err := ReadPackObject(packData, idx, wantHash)
		if err != nil {
			t.Fatalf("ReadPackObject(%s): %v", wantHash[:16], err)
		}
		if objType != want.objType {
			t.Errorf("type mismatch for %s", wantHash[:16])
		}
		if !bytes.Equal(data, want.data) {
			t.Errorf("data mismatch for %s", wantHash[:16])
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestPackRandomAccess -v`
Expected: FAIL

**Step 3: Implement `BuildPackIndex` (scans pack, builds in-memory index) and `ReadPackObject` (seeks to offset, decompresses)**

**Step 4: Run test to verify it passes**

**Step 5: Commit**

```bash
cd /home/draco/work/got
git add pkg/object/pack_reader.go pkg/object/pack_reader_test.go
git commit -m "add(pack): random access object reads via pack index"
```

---

### Task 6: Delta Compression — OFS_DELTA Writer

**Files:**
- Create: `pkg/object/delta.go`
- Test: `pkg/object/delta_test.go`
- Modify: `pkg/object/pack_writer.go` (add `WriteDeltaEntry`)

**Context:** Git delta format encodes one object as a diff against a base. The delta instruction stream: source size (varint), target size (varint), then copy/insert instructions. Copy: `(1xxxxxxx)` + offset/size bytes. Insert: `(0xxxxxxx)` + literal bytes. OFS_DELTA uses a negative offset from current position to the base object.

**Step 1: Write the failing test**

```go
// pkg/object/delta_test.go
package object

import (
	"bytes"
	"testing"
)

func TestDeltaRoundTrip(t *testing.T) {
	base := []byte("Hello, World! This is the base content for delta compression testing.")
	target := []byte("Hello, World! This is the modified content for delta compression testing.")

	delta := EncodeDelta(base, target)
	if len(delta) >= len(target) {
		// Delta should be smaller for similar objects
		t.Logf("warning: delta (%d) not smaller than target (%d)", len(delta), len(target))
	}

	reconstructed, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reconstructed, target) {
		t.Fatalf("delta round-trip failed:\ngot:  %q\nwant: %q", reconstructed, target)
	}
}

func TestDeltaIdentical(t *testing.T) {
	data := []byte("identical content")
	delta := EncodeDelta(data, data)
	result, err := ApplyDelta(data, delta)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, data) {
		t.Fatal("identical round-trip failed")
	}
}

func TestDeltaEmpty(t *testing.T) {
	base := []byte("some content")
	target := []byte("")
	delta := EncodeDelta(base, target)
	result, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, target) {
		t.Fatal("empty target round-trip failed")
	}
}

func TestDeltaLargeObject(t *testing.T) {
	base := bytes.Repeat([]byte("line of source code\n"), 10000)
	// Modify a few lines in the middle
	target := make([]byte, len(base))
	copy(target, base)
	copy(target[5000:], []byte("MODIFIED LINE HERE\n"))

	delta := EncodeDelta(base, target)
	if float64(len(delta))/float64(len(target)) > 0.1 {
		t.Errorf("delta too large: %d bytes for %d byte target (%.1f%%)", len(delta), len(target), 100*float64(len(delta))/float64(len(target)))
	}

	result, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, target) {
		t.Fatal("large object round-trip failed")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/got && go test ./pkg/object/ -run TestDelta -v`
Expected: FAIL

**Step 3: Implement `EncodeDelta` and `ApplyDelta`**

Use the Git delta format: Rabin fingerprint based matching for copy instructions, insert instructions for novel bytes. The format is:
1. Source size (varint)
2. Target size (varint)
3. Instructions: copy (bit 7 set) with offset+size encoding, or insert (bit 7 clear) with length + literal bytes

**Step 4: Run test to verify it passes**

**Step 5: Commit**

```bash
cd /home/draco/work/got
git add pkg/object/delta.go pkg/object/delta_test.go
git commit -m "add(pack): delta compression with Git-compatible instruction format"
```

---

### Task 7: Pack Reader — Delta Resolution

**Files:**
- Modify: `pkg/object/pack_reader.go`
- Test: `pkg/object/pack_reader_test.go` (add delta tests)

**Context:** When reading a pack entry of type OFS_DELTA or REF_DELTA, resolve the delta chain to get the final object. OFS_DELTA references a base by negative offset within the pack. REF_DELTA references by hash.

**Step 1: Write the failing test**

```go
func TestPackWriterDeltaRoundTrip(t *testing.T) {
	base := []byte("base content that is quite long for good delta compression")
	target := []byte("base content that is quite long for good delta testing yeah")

	var buf bytes.Buffer
	pw, _ := NewPackWriter(&buf, 2)
	pw.WriteEntry(PackBlob, base)
	baseOffset := int64(12) // right after header
	pw.WriteDeltaEntry(PackOfsDelta, base, target, baseOffset)
	pw.Finish()

	idx, _ := BuildPackIndex(bytes.NewReader(buf.Bytes()))
	targetHash := HashObject(TypeBlob, target)
	_, data, err := ReadPackObject(buf.Bytes(), idx, targetHash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, target) {
		t.Fatal("delta resolution failed")
	}
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(pack): delta chain resolution in pack reader"
```

---

### Task 8: Entity Index Trailer

**Files:**
- Create: `pkg/object/pack_entity_trailer.go`
- Test: `pkg/object/pack_entity_trailer_test.go`

**Context:** Got-specific extension to pack files. After the standard Git pack checksum, append an entity index that maps object hashes to entity stable IDs. Git clients ignore data after the checksum. Got clients detect the trailer by a magic signature.

Format:
- Magic: `GENT` (4 bytes)
- Version: uint16 (2 bytes)
- Entry count: uint32 (4 bytes)
- Entries: [object_hash (32 bytes) + stable_id_length (uint16) + stable_id (variable)]
- Trailer checksum: SHA-256 (32 bytes)

**Step 1: Write tests for writing and reading entity trailers.**

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(pack): Got entity index trailer extension"
```

---

### Task 9: `got gc` Command

**Files:**
- Create: `pkg/object/gc.go`
- Test: `pkg/object/gc_test.go`
- Create: `cmd/got/cmd_gc.go`

**Context:** Pack all loose objects into a single pack file + index. Walk all refs to find reachable objects, mark unreachable ones for deletion. Uses the pack writer from Task 2, delta compression from Task 6.

**Step 1: Write the failing test**

```go
// pkg/object/gc_test.go
package object

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGCPacksLooseObjects(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "objects"))

	// Write 10 loose blobs
	var hashes []Hash
	for i := 0; i < 10; i++ {
		h, err := store.WriteBlob(&Blob{Data: []byte(fmt.Sprintf("blob %d", i))})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h)
	}

	// Verify loose objects exist
	for _, h := range hashes {
		if !store.Has(h) {
			t.Fatalf("loose object %s missing before gc", h[:16])
		}
	}

	// Run GC
	result, err := store.GC(hashes) // pass all as reachable
	if err != nil {
		t.Fatal(err)
	}
	if result.PackedObjects != 10 {
		t.Errorf("packed %d, want 10", result.PackedObjects)
	}

	// Verify objects still readable
	for _, h := range hashes {
		if !store.Has(h) {
			t.Fatalf("object %s missing after gc", h[:16])
		}
	}

	// Verify loose objects cleaned up
	looseDir := filepath.Join(dir, "objects")
	entries, _ := os.ReadDir(looseDir)
	packDirFound := false
	for _, e := range entries {
		if e.Name() == "pack" {
			packDirFound = true
			continue
		}
		// 2-char fanout dirs should be empty or gone
	}
	if !packDirFound {
		t.Fatal("pack directory not created")
	}
}
```

**Step 2-5:** Implement, test, commit.

The GC algorithm:
1. Enumerate all loose objects
2. Filter to reachable set (passed in; caller walks refs)
3. Sort by type + size for optimal delta selection
4. For each pair of same-type objects, try delta; keep if ratio < 0.5
5. Write pack file + index
6. Verify pack is readable
7. Delete packed loose objects
8. Delete unreachable loose objects

```bash
git commit -m "add(gc): pack loose objects with delta compression and prune unreachable"
```

---

### Task 10: `got verify` Command

**Files:**
- Create: `pkg/object/verify.go`
- Test: `pkg/object/verify_test.go`
- Create: `cmd/got/cmd_verify.go`

**Context:** Verify integrity of all objects — loose and packed. Check hashes, decompress, verify checksums.

**Step 1-5:** TDD as above.

```bash
git commit -m "add(verify): integrity check for loose and packed objects"
```

---

### Task 11: Store Integration — Unified Read Path

**Files:**
- Modify: `pkg/object/store.go`
- Test: `pkg/object/store_test.go` (add pack-aware tests)

**Context:** The Store's `Read` and `Has` methods must now check loose objects first, then fall back to searching pack files. Write path remains loose-only (packing is GC's job).

**Step 1: Write the failing test**

```go
func TestStoreReadsFromPack(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "objects"))

	// Write some objects as loose
	h1, _ := store.WriteBlob(&Blob{Data: []byte("will be packed")})
	h2, _ := store.WriteBlob(&Blob{Data: []byte("also packed")})

	// Pack them
	store.GC([]Hash{h1, h2})

	// Delete loose copies
	os.Remove(store.loosePath(h1))
	os.Remove(store.loosePath(h2))

	// Should still be readable from pack
	b1, err := store.ReadBlob(h1)
	if err != nil {
		t.Fatalf("ReadBlob from pack: %v", err)
	}
	if string(b1.Data) != "will be packed" {
		t.Fatalf("wrong data: %s", b1.Data)
	}
}
```

**Step 2-5:** Implement unified read path, test, commit.

Key changes to `Store`:
- Add `packs []*loadedPack` field (lazy-loaded on first pack read)
- `Read()`: try loose path first, then iterate packs
- `Has()`: same fallback chain
- Pack discovery: scan `objects/pack/` directory for `.pack` + `.idx` pairs

```bash
git commit -m "refactor(store): unified read path across loose objects and pack files"
```

---

## Workstream B: Async Indexing Pipeline (GotHub)

All files in `/home/draco/work/gothub/`.

### Task 12: Indexing Jobs Database Schema

**Files:**
- Modify: `internal/database/database.go` (add interface methods)
- Modify: `internal/database/sqlite.go` (add migration + implementation)
- Modify: `internal/database/postgres.go` (add migration + implementation)
- Modify: `internal/models/models.go` (add IndexingJob model)
- Test: `internal/database/sqlite_test.go`

**Context:** Add the `indexing_jobs` table and DB interface methods to enqueue, claim, and complete jobs.

**Step 1: Write the failing test**

```go
// Add to sqlite_test.go
func TestIndexingJobLifecycle(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Enqueue
	job := &models.IndexingJob{
		RepoID:     1,
		CommitHash: "abc123",
		Status:     models.IndexJobPending,
	}
	err := db.EnqueueIndexingJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == 0 {
		t.Fatal("expected ID to be set")
	}

	// Claim
	claimed, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil {
		t.Fatal("expected to claim a job")
	}
	if claimed.Status != models.IndexJobRunning {
		t.Fatalf("expected running, got %s", claimed.Status)
	}

	// Complete
	err = db.CompleteIndexingJob(ctx, claimed.ID, models.IndexJobCompleted, "")
	if err != nil {
		t.Fatal(err)
	}

	// No more jobs
	next, err := db.ClaimIndexingJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatal("expected no more jobs")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/gothub && go test ./internal/database/ -run TestIndexingJob -v`
Expected: FAIL

**Step 3: Write implementation**

Add to `models.go`:
```go
type IndexJobStatus string

const (
	IndexJobPending   IndexJobStatus = "pending"
	IndexJobRunning   IndexJobStatus = "running"
	IndexJobCompleted IndexJobStatus = "completed"
	IndexJobFailed    IndexJobStatus = "failed"
)

type IndexingJob struct {
	ID         int64          `json:"id"`
	RepoID     int64          `json:"repo_id"`
	CommitHash string         `json:"commit_hash"`
	Status     IndexJobStatus `json:"status"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
}
```

Add to `database.go` interface:
```go
EnqueueIndexingJob(ctx context.Context, job *models.IndexingJob) error
ClaimIndexingJob(ctx context.Context) (*models.IndexingJob, error)
CompleteIndexingJob(ctx context.Context, jobID int64, status models.IndexJobStatus, errMsg string) error
GetIndexingJobStatus(ctx context.Context, repoID int64, commitHash string) (*models.IndexingJob, error)
```

Add to both `sqlite.go` and `postgres.go`:
- CREATE TABLE migration
- Implementation of all 4 methods
- `ClaimIndexingJob` uses `UPDATE ... WHERE status='pending' ... RETURNING *` (atomic claim)

**Step 4: Run test to verify it passes**

**Step 5: Commit**

```bash
cd /home/draco/work/gothub
git add internal/models/models.go internal/database/database.go internal/database/sqlite.go internal/database/postgres.go internal/database/sqlite_test.go
git commit -m "add(async): indexing_jobs table with enqueue/claim/complete lifecycle"
```

---

### Task 13: Background Worker Pool

**Files:**
- Create: `internal/service/indexworker.go`
- Test: `internal/service/indexworker_test.go`

**Context:** A goroutine pool that claims jobs from the database and processes them. Configurable worker count. Graceful shutdown via context cancellation.

**Step 1: Write the failing test**

```go
// internal/service/indexworker_test.go
package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolProcessesJobs(t *testing.T) {
	var processed atomic.Int32
	handler := func(ctx context.Context, job *models.IndexingJob) error {
		processed.Add(1)
		return nil
	}

	pool := NewIndexWorkerPool(2, handler, testDB)
	ctx, cancel := context.WithCancel(context.Background())

	// Enqueue 5 jobs
	for i := 0; i < 5; i++ {
		testDB.EnqueueIndexingJob(ctx, &models.IndexingJob{
			RepoID:     1,
			CommitHash: fmt.Sprintf("commit%d", i),
			Status:     models.IndexJobPending,
		})
	}

	pool.Start(ctx)
	time.Sleep(500 * time.Millisecond) // let workers process
	cancel()
	pool.Wait()

	if processed.Load() != 5 {
		t.Fatalf("processed %d, want 5", processed.Load())
	}
}

func TestWorkerPoolGracefulShutdown(t *testing.T) {
	handler := func(ctx context.Context, job *models.IndexingJob) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}
	pool := NewIndexWorkerPool(2, handler, testDB)
	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	cancel() // immediate shutdown
	pool.Wait() // should complete without hanging
}
```

**Step 2-5:** Implement, test, commit.

```go
// internal/service/indexworker.go
type IndexWorkerPool struct {
	workers int
	handler func(ctx context.Context, job *models.IndexingJob) error
	db      database.DB
	wg      sync.WaitGroup
}
```

```bash
git commit -m "add(async): background index worker pool with graceful shutdown"
```

---

### Task 14: Incremental Entity Extraction

**Files:**
- Create: `internal/service/indexer.go`
- Test: `internal/service/indexer_test.go`

**Context:** The job handler that does the actual work. Given a commit, diffs against parent to find changed files, extracts entities only for those files, updates lineage. This replaces the synchronous full-tree extraction.

**Step 1: Write the failing test**

Test that given a commit with 100 files where only 3 changed, only 3 files are entity-extracted.

**Step 2-5:** Implement, test, commit.

Key logic:
1. Read commit → get tree hash
2. Read parent commit → get parent tree hash
3. Diff the two trees (path-level, not entity-level)
4. For each changed/added file: extract entities via `entity.Extract`
5. For unchanged files: copy entity records from parent
6. Update entity lineage via `EntityLineageService.IndexCommit`

```bash
git commit -m "add(indexer): incremental entity extraction for changed files only"
```

---

### Task 15: Refactor Push Path — Make Entity Extraction Async

**Files:**
- Modify: `internal/gitinterop/protocol.go` (lines 338-348: replace sync extraction with job enqueue)
- Modify: `internal/api/router.go` (pass indexer service to handler)
- Test: `internal/gitinterop/protocol_test.go`

**Context:** The critical refactor. Replace the synchronous `extractEntitiesForCommits` call in `handleReceivePack` with an enqueue to the indexing job queue.

**Step 1: Write the failing test**

```go
func TestReceivePackEnqueuesIndexingJob(t *testing.T) {
	// Push a commit via receive-pack
	// Assert: push returns success immediately
	// Assert: indexing_jobs table has a pending job for this commit
	// Assert: entity extraction has NOT happened yet
}
```

**Step 2-5:** Implement, test, commit.

Change in `protocol.go` around line 338:
```go
// BEFORE (synchronous):
// entityCommitMappings, err := h.extractEntitiesForCommits(...)

// AFTER (async):
for _, u := range updates {
    if err := h.enqueueIndex(r.Context(), repoID, u.newHash); err != nil {
        log.Printf("warning: failed to enqueue indexing for %s: %v", u.newHash, err)
        // Non-fatal — push still succeeds
    }
}
```

```bash
git commit -m "refactor(push): make entity extraction async via job queue"
```

---

### Task 16: Index Status API

**Files:**
- Create: `internal/api/index_handlers.go`
- Modify: `internal/api/router.go` (register route)
- Test: `internal/api/api_test.go` (add test)

**Context:** `GET /api/v1/repos/{owner}/{repo}/index/status` returns the indexing state. Used by frontend to show "indexing..." badge.

**Step 1: Write the failing test**

```go
func TestGetIndexStatus(t *testing.T) {
	// Create repo, push commit, check index status
	// Expect: {"commit_hash": "abc", "status": "pending"}
	// Process job
	// Expect: {"commit_hash": "abc", "status": "completed"}
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(api): index status endpoint for async indexing visibility"
```

---

## Workstream C: Code Intelligence Engine (GotHub)

### Task 17: Persistent Entity Index Table

**Files:**
- Modify: `internal/database/database.go`
- Modify: `internal/database/sqlite.go`
- Modify: `internal/database/postgres.go`
- Modify: `internal/models/models.go`
- Test: `internal/database/sqlite_test.go`

**Context:** Replace the in-memory-only code intel index with a persistent database table. The indexer (Task 14) populates this. Code intelligence reads from it.

**Step 1: Write the failing test**

```go
func TestEntityIndexCRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	entry := &models.EntityIndexEntry{
		RepoID:     1,
		CommitHash: "abc123",
		FilePath:   "main.go",
		EntityHash: "def456",
		StableID:   "decl:function_definition::ProcessOrder",
		Kind:       "function",
		Name:       "ProcessOrder",
		StartLine:  10,
		EndLine:    25,
	}
	err := db.UpsertEntityIndexEntry(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := db.ListEntityIndex(ctx, 1, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "ProcessOrder" {
		t.Fatalf("unexpected: %+v", entries)
	}
}
```

Add to `models.go`:
```go
type EntityIndexEntry struct {
	RepoID     int64  `json:"repo_id"`
	CommitHash string `json:"commit_hash"`
	FilePath   string `json:"file_path"`
	EntityHash string `json:"entity_hash"`
	StableID   string `json:"stable_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Signature  string `json:"signature,omitempty"`
	DocComment string `json:"doc_comment,omitempty"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(codeintel): persistent entity_index table with composite indexes"
```

---

### Task 18: Full-Text Symbol Search

**Files:**
- Modify: `internal/database/sqlite.go` (FTS5 virtual table)
- Modify: `internal/database/postgres.go` (tsvector + GIN index)
- Modify: `internal/database/database.go`
- Test: `internal/database/sqlite_test.go`

**Context:** Enable fast symbol search across entity names, signatures, and doc comments.

**Step 1: Write the failing test**

```go
func TestSymbolSearch(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Insert several entities
	entities := []models.EntityIndexEntry{
		{RepoID: 1, CommitHash: "abc", Name: "ProcessOrder", Kind: "function", StableID: "a"},
		{RepoID: 1, CommitHash: "abc", Name: "ValidateOrder", Kind: "function", StableID: "b"},
		{RepoID: 1, CommitHash: "abc", Name: "OrderService", Kind: "type", StableID: "c"},
		{RepoID: 1, CommitHash: "abc", Name: "HttpClient", Kind: "type", StableID: "d"},
	}
	for _, e := range entities {
		db.UpsertEntityIndexEntry(ctx, &e)
	}

	// Search for "Order"
	results, err := db.SearchSymbols(ctx, 1, "abc", "Order")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results for 'Order', got %d", len(results))
	}

	// Search with type filter
	results, err = db.SearchSymbols(ctx, 1, "abc", "Order", WithKindFilter("function"))
	if len(results) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(results))
	}
}
```

**Step 2-5:** Implement, test, commit.

SQLite: `CREATE VIRTUAL TABLE entity_fts USING fts5(name, signature, doc_comment, content=entity_index, content_rowid=rowid)`
PostgreSQL: `ALTER TABLE entity_index ADD COLUMN search_vector tsvector; CREATE INDEX idx_entity_search ON entity_index USING GIN(search_vector);`

```bash
git commit -m "add(codeintel): full-text symbol search via FTS5/tsvector"
```

---

### Task 19: Symbol Search API Endpoint

**Files:**
- Modify: `internal/service/codeintel.go` (refactor to use persistent index)
- Create: `internal/api/search_handlers.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/api_test.go`

**Context:** `GET /api/v1/repos/{owner}/{repo}/search?q=ProcessOrder&type=function&lang=go`

**Step 1-5:** TDD as above.

```bash
git commit -m "add(api): symbol search endpoint with fuzzy matching and type filters"
```

---

### Task 20: Bloom Filters for Entity Lookup

**Files:**
- Create: `internal/service/bloom.go`
- Test: `internal/service/bloom_test.go`
- Modify: `internal/database/database.go`
- Modify: `internal/database/sqlite.go`
- Modify: `internal/database/postgres.go`
- Modify: `internal/models/models.go`

**Context:** Per-commit bloom filter for O(1) "does this commit contain entity X?" checks. Stored as binary blob in `commit_bloom` table.

**Step 1: Write the failing test**

```go
// internal/service/bloom_test.go
package service

import "testing"

func TestBloomFilter(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01) // 1000 items, 1% FPR
	bf.Add("decl:function_definition::ProcessOrder")
	bf.Add("decl:function_definition::ValidateOrder")

	if !bf.MayContain("decl:function_definition::ProcessOrder") {
		t.Error("false negative for ProcessOrder")
	}
	if !bf.MayContain("decl:function_definition::ValidateOrder") {
		t.Error("false negative for ValidateOrder")
	}

	// Test serialization round-trip
	data := bf.Marshal()
	bf2, err := UnmarshalBloomFilter(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bf2.MayContain("decl:function_definition::ProcessOrder") {
		t.Error("false negative after round-trip")
	}
}

func TestBloomFilterFPR(t *testing.T) {
	bf := NewBloomFilter(10000, 0.01)
	for i := 0; i < 10000; i++ {
		bf.Add(fmt.Sprintf("entity-%d", i))
	}
	falsePositives := 0
	tests := 100000
	for i := 0; i < tests; i++ {
		if bf.MayContain(fmt.Sprintf("nonexistent-%d", i)) {
			falsePositives++
		}
	}
	fpr := float64(falsePositives) / float64(tests)
	if fpr > 0.02 { // Allow 2x the target FPR
		t.Errorf("FPR too high: %.4f (target 0.01)", fpr)
	}
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(codeintel): bloom filters for O(1) entity containment checks"
```

---

### Task 21: Cross-Reference Graph

**Files:**
- Modify: `internal/database/database.go`
- Modify: `internal/database/sqlite.go`
- Modify: `internal/database/postgres.go`
- Modify: `internal/models/models.go`
- Modify: `internal/service/indexer.go` (populate xref during indexing)
- Test: `internal/database/sqlite_test.go`

**Context:** Persistent xref table for call graph, find-references, impact analysis. Populated during async indexing via tree-sitter identifier resolution.

**Step 1: Write the failing test**

```go
func TestXRefCRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	ref := &models.XRef{
		RepoID:         1,
		CommitHash:     "abc",
		SourceEntityID: "decl:function::Caller",
		TargetEntityID: "decl:function::ProcessOrder",
		Kind:           "call",
		SourceFile:     "handler.go",
		SourceLine:     42,
	}
	err := db.InsertXRef(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}

	// Find callers of ProcessOrder
	callers, err := db.FindReferences(ctx, 1, "abc", "decl:function::ProcessOrder", "call")
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0].SourceEntityID != "decl:function::Caller" {
		t.Fatalf("unexpected callers: %+v", callers)
	}
}
```

Add to `models.go`:
```go
type XRef struct {
	RepoID         int64  `json:"repo_id"`
	CommitHash     string `json:"commit_hash"`
	SourceEntityID string `json:"source_entity_id"`
	TargetEntityID string `json:"target_entity_id"`
	Kind           string `json:"kind"` // "call", "type_ref", "import"
	SourceFile     string `json:"source_file"`
	SourceLine     int    `json:"source_line"`
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(codeintel): cross-reference graph with call/type_ref/import tracking"
```

---

### Task 22: Impact Analysis API

**Files:**
- Create: `internal/api/impact_handlers.go`
- Modify: `internal/service/codeintel.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/api_test.go`

**Context:** `GET /api/v1/repos/{owner}/{repo}/impact/{entity_id}?ref=main&depth=3` — returns the transitive set of entities affected if the given entity changes. Uses BFS over the xref graph.

**Step 1-5:** TDD as above.

```bash
git commit -m "add(api): impact analysis endpoint via xref graph traversal"
```

---

### Task 23: Semantic Diff Classification

**Files:**
- Modify: `internal/service/diff.go`
- Test: `internal/service/diff_test.go`
- Modify: `internal/api/diff_handlers.go`

**Context:** Classify entity changes as signature/body/doc/visibility changes. Detect breaking changes. `GET /api/v1/repos/{owner}/{repo}/diff/{base}...{head}/semantic`

**Step 1: Write the failing test**

```go
func TestSemanticDiffClassification(t *testing.T) {
	// Two versions of a function: signature changed (parameter added)
	// Expect: ChangeKind = "signature", Breaking = true
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(diff): semantic change classification with breaking change detection"
```

---

## Workstream D: Merge Performance

### Task 24: Generation Numbers for Commits

**Files:**
- Modify: `internal/database/database.go`
- Modify: `internal/database/sqlite.go`
- Modify: `internal/database/postgres.go`
- Modify: `internal/models/models.go`
- Modify: `internal/service/indexer.go` (compute generation on index)
- Test: `internal/database/sqlite_test.go`

**Context:** Store topological generation numbers per commit. Generation = 1 + max(parent generations). Root commits have generation 1.

**Step 1: Write the failing test**

```go
func TestCommitMetaGenerationNumber(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Root commit: generation 1
	db.UpsertCommitMeta(ctx, &models.CommitMeta{RepoID: 1, CommitHash: "root", Generation: 1})

	// Child: generation 2
	db.UpsertCommitMeta(ctx, &models.CommitMeta{RepoID: 1, CommitHash: "child", Generation: 2})

	meta, _ := db.GetCommitMeta(ctx, 1, "child")
	if meta.Generation != 2 {
		t.Fatalf("expected generation 2, got %d", meta.Generation)
	}
}
```

Add to `models.go`:
```go
type CommitMeta struct {
	RepoID     int64  `json:"repo_id"`
	CommitHash string `json:"commit_hash"`
	Generation int64  `json:"generation"`
	Timestamp  int64  `json:"timestamp"`
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(merge): commit generation numbers for LCA pruning"
```

---

### Task 25: LCA with Generation-Number Pruning

**Files:**
- Modify: `internal/service/pr.go` (replace naive BFS with generation-pruned search)
- Test: `internal/service/pr_test.go`

**Context:** Use generation numbers to prune the merge base search. Never explore a commit whose generation > min(gen_a, gen_b). Reduces O(n) to O(k).

**Step 1: Write the failing test**

```go
func TestMergeBaseWithGenerationPruning(t *testing.T) {
	// Create a DAG with 1000 commits on main, branch at commit 500
	// Verify merge base found is commit 500
	// Verify it visits far fewer than 1000 commits
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "refactor(merge): LCA with generation-number pruning for O(k) merge base"
```

---

### Task 26: Merge Base Cache

**Files:**
- Modify: `internal/database/database.go`
- Modify: `internal/database/sqlite.go`
- Modify: `internal/database/postgres.go`
- Modify: `internal/service/pr.go`
- Test: `internal/database/sqlite_test.go`

**Context:** Cache merge base results. Invalidate when refs update.

Table: `merge_base_cache(repo_id, commit_a, commit_b, base_hash, computed_at)`

Lookup: normalize order (lexicographic sort of commit_a, commit_b) for consistent cache keys.

**Step 1-5:** TDD as above.

```bash
git commit -m "add(merge): merge base cache with ref-based invalidation"
```

---

### Task 27: Merge Preview Cache

**Files:**
- Modify: `internal/database/database.go`
- Modify: `internal/database/sqlite.go`
- Modify: `internal/database/postgres.go`
- Modify: `internal/models/models.go`
- Modify: `internal/service/pr.go`
- Test: `internal/database/sqlite_test.go`

**Context:** Cache full merge preview results per PR. Invalidated when source or target branch moves.

Table: `merge_preview_cache(pr_id, src_hash, tgt_hash, result_json, computed_at)`

**Step 1-5:** TDD as above.

```bash
git commit -m "add(merge): merge preview cache keyed by src/tgt commit hashes"
```

---

### Task 28: Parallel File Merging

**Files:**
- Modify: `internal/service/pr.go` (parallelize three-way merge across files)
- Test: `internal/service/pr_test.go`

**Context:** The three-way merge of individual files within a PR is embarrassingly parallel. Use a bounded goroutine pool.

**Step 1: Write the failing test**

```go
func TestParallelMerge(t *testing.T) {
	// Create PR with 100 changed files
	// Verify merge completes correctly
	// Verify it's faster than sequential (use benchmark)
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "refactor(merge): parallel three-way file merging with bounded worker pool"
```

---

## Workstream E: Observability

### Task 29: OpenTelemetry Tracing Foundation

**Files:**
- Modify: `go.mod` (add `go.opentelemetry.io/otel` dependencies)
- Create: `internal/observability/tracing.go`
- Modify: `cmd/gothub/main.go` (initialize tracer)
- Test: `internal/observability/tracing_test.go`

**Context:** Set up OTel with configurable exporter (stdout JSON for dev, OTLP for production). Instrument the push path first.

**Step 1: Write the failing test**

```go
func TestTracerInitialization(t *testing.T) {
	tp, err := NewTracerProvider("stdout", "gothub", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer tp.Shutdown(context.Background())
	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(obs): OpenTelemetry tracing foundation with configurable exporter"
```

---

### Task 30: Instrument Hot Paths

**Files:**
- Modify: `internal/gitinterop/protocol.go` (add spans to push path)
- Modify: `internal/service/codeintel.go` (add spans to index/search)
- Modify: `internal/service/pr.go` (add spans to merge)

**Context:** Add tracing spans to push, indexing, code intelligence, and merge operations.

**Step 1-5:** Add spans, verify with test that spans are emitted, commit.

```bash
git commit -m "add(obs): tracing spans for push, indexing, code intel, and merge paths"
```

---

### Task 31: Prometheus Metrics

**Files:**
- Modify: `go.mod` (add `github.com/prometheus/client_golang`)
- Create: `internal/observability/metrics.go`
- Modify: `internal/api/router.go` (add `/metrics` endpoint)
- Test: `internal/observability/metrics_test.go`

**Context:** Define all metrics from the design doc. Expose `/metrics` for Prometheus scraping.

**Step 1: Write the failing test**

```go
func TestMetricsRegistration(t *testing.T) {
	m := NewMetrics()
	m.PushDuration.Observe(0.5)
	m.IndexDuration.WithLabelValues("extract").Observe(1.2)
	m.CacheHitRatio.WithLabelValues("codeintel").Set(0.85)
	// Verify metrics are scrapable
}
```

**Step 2-5:** Implement, test, commit.

```bash
git commit -m "add(obs): Prometheus metrics for push, indexing, cache, merge operations"
```

---

### Task 32: pprof and Health Endpoint

**Files:**
- Modify: `internal/api/router.go` (register `/debug/pprof/` and `/admin/health`)
- Create: `internal/api/health_handlers.go`
- Test: `internal/api/api_test.go`

**Context:** Enable pprof in dev/admin mode. Health endpoint returns JSON with queue depth, cache stats, worker count.

**Step 1-5:** TDD as above.

```bash
git commit -m "add(obs): pprof profiling and /admin/health endpoint"
```

---

### Task 33: Got Benchmark Suites

**Files:**
- Create: `pkg/object/bench_test.go`
- Create: `pkg/merge/bench_test.go`
- Create: `pkg/entity/bench_test.go`
- Create: `pkg/diff3/bench_test.go`

**Context:** Comprehensive benchmarks for all performance-critical paths in Got.

```go
// pkg/object/bench_test.go
func BenchmarkPackWrite(b *testing.B) { ... }
func BenchmarkPackRead(b *testing.B) { ... }
func BenchmarkDeltaCompress(b *testing.B) { ... }
func BenchmarkStoreWrite(b *testing.B) { ... }
func BenchmarkStoreRead(b *testing.B) { ... }

// pkg/entity/bench_test.go
func BenchmarkEntityExtractGo(b *testing.B) { ... }
func BenchmarkEntityExtractPython(b *testing.B) { ... }
func BenchmarkEntityExtractTypeScript(b *testing.B) { ... }

// pkg/merge/bench_test.go
func BenchmarkThreeWayMerge100Lines(b *testing.B) { ... }
func BenchmarkThreeWayMerge1KLines(b *testing.B) { ... }
func BenchmarkThreeWayMerge10KLines(b *testing.B) { ... }

// pkg/diff3/bench_test.go
func BenchmarkMyersDiff(b *testing.B) { ... }
func BenchmarkThreeWayChunkMerge(b *testing.B) { ... }
```

**Step 1-5:** Write benchmarks, run to establish baselines, commit.

Run: `cd /home/draco/work/got && go test -bench=. -benchmem ./...`

```bash
git commit -m "add(bench): comprehensive benchmark suites for pack, entity, merge, diff"
```

---

### Task 34: GotHub Benchmark Suites

**Files:**
- Create: `internal/service/bench_test.go`
- Create: `internal/database/bench_test.go`

**Context:** Benchmarks for GotHub service layer operations.

```go
// internal/service/bench_test.go
func BenchmarkCodeIntelBuildIndex(b *testing.B) { ... }
func BenchmarkSymbolSearch(b *testing.B) { ... }
func BenchmarkMergePreview(b *testing.B) { ... }
func BenchmarkEntityLineageWalk(b *testing.B) { ... }
func BenchmarkBloomFilterLookup(b *testing.B) { ... }

// internal/database/bench_test.go
func BenchmarkEntityIndexInsert(b *testing.B) { ... }
func BenchmarkEntityIndexQuery(b *testing.B) { ... }
func BenchmarkFTSSearch(b *testing.B) { ... }
func BenchmarkXRefQuery(b *testing.B) { ... }
```

**Step 1-5:** Write benchmarks, run, commit.

```bash
git commit -m "add(bench): service and database benchmark suites"
```

---

## Workstream F: PR Intelligence UX (GotHub)

### Task 40: PR Impact Summary Card

**Status:** Complete (`2930c83`)

**Scope delivered:**
- Added PR impact summary card in the PR detail view (`Files changed` and `Merge preview` tabs).
- Aggregates structural diff data into file/entity/add/modify/remove counts.

### Task 41: SemVer Recommendation Card in PR Merge View

**Status:** Complete (`2930c83`)

**Scope delivered:**
- Wired PR merge view to semver endpoint (`/api/v1/repos/{owner}/{repo}/semver/{base...head}`).
- Added SemVer recommendation panel with `major|minor|patch|none` and detail lists (`breaking_changes`, `features`, `fixes`).

### Task 42: Entity Owner Approval Checklist in Merge Gate UI

**Status:** Complete (`2930c83`)

**Scope delivered:**
- Extended merge-gate payload with structured entity owner approval data (`entity_owner_approvals`).
- Added checklist UI showing entity, required owners, approved users, missing owners, unresolved teams, and pass/fail state.

---

## Execution Order

Tasks can be partially parallelized across workstreams:

**Week 1-2:** Tasks 1-5 (Pack basics) + Tasks 12-13 (DB schema + worker pool) in parallel
**Week 3-4:** Tasks 6-8 (Delta + trailer) + Tasks 14-16 (Incremental indexer + push refactor) in parallel
**Week 5-6:** Tasks 9-11 (GC + verify + store integration) + Tasks 17-19 (Entity index + FTS + search API) in parallel
**Week 7-8:** Tasks 20-23 (Bloom + xref + impact + semantic diff) + Tasks 24-26 (Generation numbers + LCA + merge cache) in parallel
**Week 9:** Tasks 27-28 (Preview cache + parallel merge) + Tasks 29-32 (Observability) in parallel
**Week 10:** Tasks 33-34 (Benchmarks) + integration testing + performance validation against design targets

---

## Queued Post-Phase-1 Workstream (Cloud Multi-Tenancy)

These tasks are queued and intentionally excluded from Phase 1 scope.

### Task 35: Tenant Key Propagation in Schema

**Goal:** add `tenant_id BIGINT NOT NULL` to all tenant-scoped tables (`users`, `orgs`, `repositories`, and dependent tables via FK propagation), including migration strategy for existing rows.

### Task 36: Postgres RLS Enforcement

**Goal:** enable RLS on all tenant-scoped tables and add policies enforcing:

```sql
tenant_id = current_setting('app.tenant_id')::bigint
```

### Task 37: Tenant Context in Request + DB Layer

**Goal:** set `app.tenant_id` on every request/connection/transaction boundary from authenticated request context middleware.

### Task 38: Tenant-Aware Storage Paths

**Goal:** update object storage layout to:

```text
{root}/{tenant_id}/{repo_id}/
```

### Task 39: Multi-Tenant Security Verification

**Goal:** add integration tests for cross-tenant data isolation (API and direct SQL path) to prove RLS defense-in-depth.

---

## Structural Differentiation Workstream (CLI + UX)

These items come from the latest capability audit and are being executed as post-core slices.

### Task 43: Structural Blame (`got blame --entity`)

**Status:** Complete (`5178221`)

**Goal:** attribute entity-level ownership in blame output (`func ProcessOrder last touched by <author> in <commit>`), not line-level only.

### Task 44: Entity-Level Log (`got log --entity`)

**Status:** Complete (`b655d12`)

**Goal:** filter commit history to only commits that changed a selected entity key/path.

### Task 45: Rename-Aware Entity Tracking

**Status:** Complete (`fad365d`)

**Goal:** connect entity identity across rename/move operations so blame/log history survives refactors.

### Task 46: Entity Cherry-Pick (`got cherry-pick --entity`)

**Status:** Complete (`b7302c5`)

**Goal:** apply a single entity delta from another commit/branch without cherry-picking whole commits.

### Task 47: Structural Blame Panel in GotHub Code View

**Status:** Complete (`2614e72`)

**Scope delivered:**
- Added structural blame panel in code view with per-entity selection and attribution details.
- Added `GET /api/v1/repos/{owner}/{repo}/entity-blame/{ref}` route and handler integration.

### Task 48: Visual Call Graph in GotHub

**Status:** Complete (`a4a6969`)

**Goal:** replace callgraph table-only presentation with interactive graph rendering and click-through navigation.

### Task 49: Inline Code Intelligence in Blob Viewer

**Status:** Complete (`5d2e39c`)

**Scope delivered:**
- Added inline intelligence panel in blob view with hover-to-preview and click-to-pin interactions.
- Wired definitions, references, and entity history lookups for the selected symbol token.

### Task 50: Onboarding Demo Repository

**Status:** Complete (`72cb54f`)

**Scope delivered:**
- Added first-run "Try Demo" onboarding card in dashboard with a guided 4-step walkthrough.
- Demo path covers PR impact, SemVer recommendation, call graph navigation, and structural blame entry points.
