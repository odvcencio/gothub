package gitinterop

import (
	"bytes"
	"testing"
)

func TestBuildAndParsePackfile(t *testing.T) {
	// Create some test objects
	objects := []PackfileObject{
		{Type: OBJ_BLOB, Data: []byte("hello world\n")},
		{Type: OBJ_BLOB, Data: []byte("package main\n\nfunc main() {}\n")},
	}

	// Build packfile
	packData, err := BuildPackfile(objects)
	if err != nil {
		t.Fatalf("build packfile: %v", err)
	}

	// Verify magic header
	if !bytes.HasPrefix(packData, []byte("PACK")) {
		t.Fatal("packfile missing PACK header")
	}

	// Parse it back
	parsed, err := ParsePackfile(bytes.NewReader(packData))
	if err != nil {
		t.Fatalf("parse packfile: %v", err)
	}

	if len(parsed) != len(objects) {
		t.Fatalf("expected %d objects, got %d", len(objects), len(parsed))
	}

	for i, obj := range parsed {
		if obj.Type != objects[i].Type {
			t.Errorf("object %d: type mismatch: got %d, want %d", i, obj.Type, objects[i].Type)
		}
		if !bytes.Equal(obj.Data, objects[i].Data) {
			t.Errorf("object %d: data mismatch: got %q, want %q", i, obj.Data, objects[i].Data)
		}
	}
}

func TestGitHashBytes(t *testing.T) {
	// Known git hash for "hello world\n" blob
	data := []byte("hello world\n")
	h := GitHashBytes(GitTypeBlob, data)

	// "hello world\n" as a git blob: blob 12\0hello world\n
	expected := GitHash("3b18e512dba79e4c8300dd08aeb37f8e728b8dad")
	if h != expected {
		t.Errorf("hash mismatch: got %s, want %s", h, expected)
	}
}

func TestPktLine(t *testing.T) {
	line := pktLine("# service=git-upload-pack\n")
	// Length: 4 (prefix) + 26 (content) = 30 = 0x001e
	expected := "001e# service=git-upload-pack\n"
	if string(line) != expected {
		t.Errorf("pkt-line: got %q, want %q", line, expected)
	}

	flush := pktFlush()
	if string(flush) != "0000" {
		t.Errorf("flush: got %q, want %q", flush, "0000")
	}
}
