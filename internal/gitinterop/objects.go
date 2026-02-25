package gitinterop

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

// Git object types
const (
	GitTypeCommit = "commit"
	GitTypeTree   = "tree"
	GitTypeBlob   = "blob"
	GitTypeTag    = "tag"
)

// GitHash is a 40-char hex SHA-1 hash.
type GitHash string

// GitObject represents a raw git object.
type GitObject struct {
	Type string
	Data []byte
}

// GitHashBytes computes the SHA-1 hash of a git object (with header).
func GitHashBytes(objType string, data []byte) GitHash {
	header := fmt.Sprintf("%s %d\x00", objType, len(data))
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(data)
	return GitHash(fmt.Sprintf("%x", h.Sum(nil)))
}

// GotToGitBlob converts a Got blob to a git blob. Returns git hash and raw data.
func GotToGitBlob(data []byte) (GitHash, []byte) {
	return GitHashBytes(GitTypeBlob, data), data
}

// GotToGitCommit converts a Got commit to a git commit object.
// parentGitHashes should be pre-resolved git hashes of parent commits.
// treeGitHash is the pre-resolved git hash of the tree.
func GotToGitCommit(c *object.CommitObj, treeGitHash GitHash, parentGitHashes []GitHash) (GitHash, []byte) {
	var buf bytes.Buffer
	authorTZ := strings.TrimSpace(c.AuthorTimezone)
	if authorTZ == "" {
		authorTZ = "+0000"
	}
	committer := strings.TrimSpace(c.Committer)
	hasExplicitCommitter := committer != ""
	if !hasExplicitCommitter {
		committer = c.Author
	}
	committerTS := c.CommitterTimestamp
	if !hasExplicitCommitter && committerTS == 0 {
		committerTS = c.Timestamp
	}
	committerTZ := strings.TrimSpace(c.CommitterTimezone)
	if committerTZ == "" {
		if hasExplicitCommitter {
			committerTZ = "+0000"
		} else {
			committerTZ = authorTZ
		}
	}

	fmt.Fprintf(&buf, "tree %s\n", treeGitHash)
	for _, p := range parentGitHashes {
		fmt.Fprintf(&buf, "parent %s\n", p)
	}
	fmt.Fprintf(&buf, "author %s %d %s\n", c.Author, c.Timestamp, authorTZ)
	fmt.Fprintf(&buf, "committer %s %d %s\n", committer, committerTS, committerTZ)
	fmt.Fprintf(&buf, "\n%s", c.Message)
	data := buf.Bytes()
	return GitHashBytes(GitTypeCommit, data), data
}

// GotToGitTree converts a Got tree to a git tree object.
// entryHashes maps entry names to their pre-resolved git hashes.
// entryModes maps entry names to git modes (e.g. "100644", "100755", "120000", "160000", "40000").
func GotToGitTree(t *object.TreeObj, entryHashes map[string]GitHash, entryModes map[string]string) (GitHash, []byte) {
	var buf bytes.Buffer
	for _, e := range t.Entries {
		gh := entryHashes[e.Name]
		mode := entryModes[e.Name]
		if mode == "" {
			if e.IsDir {
				mode = "40000"
			} else {
				mode = "100644"
			}
		}
		fmt.Fprintf(&buf, "%s %s\x00", mode, e.Name)
		// Append raw 20-byte SHA-1
		hashBytes := hexToBytes(string(gh))
		buf.Write(hashBytes)
	}
	data := buf.Bytes()
	return GitHashBytes(GitTypeTree, data), data
}

// CompressGitObject wraps a git object in its stored format (zlib compressed with header).
func CompressGitObject(objType string, data []byte) ([]byte, error) {
	header := fmt.Sprintf("%s %d\x00", objType, len(data))
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	w.Write([]byte(header))
	w.Write(data)
	w.Close()
	return buf.Bytes(), nil
}

// DecompressGitObject decompresses and parses a raw git object.
func DecompressGitObject(compressed []byte) (*GitObject, error) {
	r, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	nul := bytes.IndexByte(data, 0)
	if nul < 0 {
		return nil, fmt.Errorf("invalid git object: no null byte")
	}
	header := string(data[:nul])
	parts := bytes.SplitN([]byte(header), []byte(" "), 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid git object header: %s", header)
	}
	return &GitObject{
		Type: string(parts[0]),
		Data: data[nul+1:],
	}, nil
}

// hexToBytes converts a 40-char hex string to 20 raw bytes.
func hexToBytes(hex string) []byte {
	b := make([]byte, 20)
	for i := 0; i < 20; i++ {
		v, _ := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		b[i] = byte(v)
	}
	return b
}

// bytesToHex converts 20 raw bytes to a 40-char hex string.
func bytesToHex(b []byte) string {
	return fmt.Sprintf("%x", b)
}
