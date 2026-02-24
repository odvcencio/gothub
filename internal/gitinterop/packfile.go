package gitinterop

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
)

// Packfile object types (as encoded in packfile headers)
const (
	OBJ_COMMIT    = 1
	OBJ_TREE      = 2
	OBJ_BLOB      = 3
	OBJ_TAG       = 4
	OBJ_OFS_DELTA = 6
	OBJ_REF_DELTA = 7
)

// PackfileObject is a single object extracted from a packfile.
type PackfileObject struct {
	Type int
	Data []byte
}

// ParsePackfile reads a git packfile and returns all objects.
// Supports whole objects (commit, tree, blob, tag) and ref-delta objects.
func ParsePackfile(r io.Reader) ([]PackfileObject, error) {
	// Read entire packfile into memory for offset-based operations
	all, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read packfile: %w", err)
	}
	buf := bytes.NewReader(all)

	// Header: "PACK" + version(4) + numObjects(4)
	var header [4]byte
	if _, err := io.ReadFull(buf, header[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if string(header[:]) != "PACK" {
		return nil, fmt.Errorf("invalid packfile magic: %s", header[:])
	}
	var version, numObjects uint32
	binary.Read(buf, binary.BigEndian, &version)
	binary.Read(buf, binary.BigEndian, &numObjects)
	if version != 2 && version != 3 {
		return nil, fmt.Errorf("unsupported packfile version: %d", version)
	}

	// Store raw objects by offset for delta resolution
	objects := make([]PackfileObject, 0, numObjects)
	objectsByOffset := make(map[int64]*PackfileObject)

	for i := uint32(0); i < numObjects; i++ {
		offset := int64(len(all)) - int64(buf.Len())
		objType, size, err := readPackfileObjHeader(buf)
		if err != nil {
			return nil, fmt.Errorf("object %d header: %w", i, err)
		}

		switch objType {
		case OBJ_COMMIT, OBJ_TREE, OBJ_BLOB, OBJ_TAG:
			data, err := readZlib(buf, size)
			if err != nil {
				return nil, fmt.Errorf("object %d data: %w", i, err)
			}
			obj := PackfileObject{Type: objType, Data: data}
			objects = append(objects, obj)
			objectsByOffset[offset] = &objects[len(objects)-1]

		case OBJ_OFS_DELTA:
			// Read negative offset to base object
			baseOffset, err := readOfsOffset(buf)
			if err != nil {
				return nil, fmt.Errorf("object %d ofs-delta offset: %w", i, err)
			}
			deltaData, err := readZlib(buf, size)
			if err != nil {
				return nil, fmt.Errorf("object %d delta data: %w", i, err)
			}
			absOffset := offset - baseOffset
			base, ok := objectsByOffset[absOffset]
			if !ok {
				return nil, fmt.Errorf("object %d: base object at offset %d not found", i, absOffset)
			}
			resolved, err := applyDelta(base.Data, deltaData)
			if err != nil {
				return nil, fmt.Errorf("object %d: apply delta: %w", i, err)
			}
			obj := PackfileObject{Type: base.Type, Data: resolved}
			objects = append(objects, obj)
			objectsByOffset[offset] = &objects[len(objects)-1]

		case OBJ_REF_DELTA:
			// Read 20-byte base object hash
			var baseHash [20]byte
			if _, err := io.ReadFull(buf, baseHash[:]); err != nil {
				return nil, fmt.Errorf("object %d ref-delta hash: %w", i, err)
			}
			deltaData, err := readZlib(buf, size)
			if err != nil {
				return nil, fmt.Errorf("object %d delta data: %w", i, err)
			}
			// Find base by hash — scan all existing objects
			baseHashHex := bytesToHex(baseHash[:])
			var base *PackfileObject
			for j := range objects {
				h := gitHashRaw(objects[j].Type, objects[j].Data)
				if h == baseHashHex {
					base = &objects[j]
					break
				}
			}
			if base == nil {
				return nil, fmt.Errorf("object %d: ref-delta base %s not found", i, baseHashHex)
			}
			resolved, err := applyDelta(base.Data, deltaData)
			if err != nil {
				return nil, fmt.Errorf("object %d: apply delta: %w", i, err)
			}
			obj := PackfileObject{Type: base.Type, Data: resolved}
			objects = append(objects, obj)
			objectsByOffset[offset] = &objects[len(objects)-1]

		default:
			return nil, fmt.Errorf("object %d: unknown type %d", i, objType)
		}
	}

	return objects, nil
}

// BuildPackfile creates a packfile from a list of git objects.
func BuildPackfile(objects []PackfileObject) ([]byte, error) {
	var buf bytes.Buffer

	// Header
	buf.WriteString("PACK")
	binary.Write(&buf, binary.BigEndian, uint32(2)) // version
	binary.Write(&buf, binary.BigEndian, uint32(len(objects)))

	for _, obj := range objects {
		writePackfileObjHeader(&buf, obj.Type, len(obj.Data))
		// Compress data
		var zbuf bytes.Buffer
		w := zlib.NewWriter(&zbuf)
		w.Write(obj.Data)
		w.Close()
		buf.Write(zbuf.Bytes())
	}

	// Trailing SHA-1 checksum
	h := sha1.Sum(buf.Bytes())
	buf.Write(h[:])

	return buf.Bytes(), nil
}

func readPackfileObjHeader(r io.ByteReader) (objType int, size int64, err error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	objType = int((b >> 4) & 0x07)
	size = int64(b & 0x0f)
	shift := uint(4)
	for b&0x80 != 0 {
		b, err = r.ReadByte()
		if err != nil {
			return 0, 0, err
		}
		size |= int64(b&0x7f) << shift
		shift += 7
	}
	return objType, size, nil
}

func writePackfileObjHeader(w *bytes.Buffer, objType int, size int) {
	b := byte((objType & 0x07) << 4)
	b |= byte(size & 0x0f)
	remaining := size >> 4
	if remaining > 0 {
		b |= 0x80
	}
	w.WriteByte(b)
	for remaining > 0 {
		b = byte(remaining & 0x7f)
		remaining >>= 7
		if remaining > 0 {
			b |= 0x80
		}
		w.WriteByte(b)
	}
}

func readOfsOffset(r io.ByteReader) (int64, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	offset := int64(b & 0x7f)
	for b&0x80 != 0 {
		b, err = r.ReadByte()
		if err != nil {
			return 0, err
		}
		offset = ((offset + 1) << 7) | int64(b&0x7f)
	}
	return offset, nil
}

func readZlib(r io.Reader, expectedSize int64) ([]byte, error) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// applyDelta applies a git delta instruction stream to a base object.
func applyDelta(base, delta []byte) ([]byte, error) {
	dr := bytes.NewReader(delta)

	// Read base size and result size (variable-length integers)
	_, err := readDeltaSize(dr)
	if err != nil {
		return nil, fmt.Errorf("read base size: %w", err)
	}
	resultSize, err := readDeltaSize(dr)
	if err != nil {
		return nil, fmt.Errorf("read result size: %w", err)
	}

	result := make([]byte, 0, resultSize)

	for dr.Len() > 0 {
		cmd, err := dr.ReadByte()
		if err != nil {
			return nil, err
		}

		if cmd&0x80 != 0 {
			// Copy from base
			var offset, size int64
			if cmd&0x01 != 0 {
				b, _ := dr.ReadByte()
				offset |= int64(b)
			}
			if cmd&0x02 != 0 {
				b, _ := dr.ReadByte()
				offset |= int64(b) << 8
			}
			if cmd&0x04 != 0 {
				b, _ := dr.ReadByte()
				offset |= int64(b) << 16
			}
			if cmd&0x08 != 0 {
				b, _ := dr.ReadByte()
				offset |= int64(b) << 24
			}
			if cmd&0x10 != 0 {
				b, _ := dr.ReadByte()
				size |= int64(b)
			}
			if cmd&0x20 != 0 {
				b, _ := dr.ReadByte()
				size |= int64(b) << 8
			}
			if cmd&0x40 != 0 {
				b, _ := dr.ReadByte()
				size |= int64(b) << 16
			}
			if size == 0 {
				size = 0x10000
			}
			if offset+size > int64(len(base)) {
				return nil, fmt.Errorf("delta copy out of bounds: offset=%d size=%d base=%d", offset, size, len(base))
			}
			result = append(result, base[offset:offset+size]...)
		} else if cmd > 0 {
			// Insert new data
			insert := make([]byte, cmd)
			if _, err := io.ReadFull(dr, insert); err != nil {
				return nil, fmt.Errorf("delta insert: %w", err)
			}
			result = append(result, insert...)
		} else {
			return nil, fmt.Errorf("invalid delta command: 0")
		}
	}

	if int64(len(result)) != resultSize {
		return nil, fmt.Errorf("delta result size mismatch: got %d, expected %d", len(result), resultSize)
	}
	return result, nil
}

func readDeltaSize(r *bytes.Reader) (int64, error) {
	var size int64
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		size |= int64(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	return size, nil
}

func gitHashRaw(objType int, data []byte) string {
	typeName := packTypeToString(objType)
	header := fmt.Sprintf("%s %d\x00", typeName, len(data))
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func packTypeToString(t int) string {
	switch t {
	case OBJ_COMMIT:
		return GitTypeCommit
	case OBJ_TREE:
		return GitTypeTree
	case OBJ_BLOB:
		return GitTypeBlob
	case OBJ_TAG:
		return GitTypeTag
	default:
		return "unknown"
	}
}

func stringToPackType(s string) int {
	switch s {
	case GitTypeCommit:
		return OBJ_COMMIT
	case GitTypeTree:
		return OBJ_TREE
	case GitTypeBlob:
		return OBJ_BLOB
	case GitTypeTag:
		return OBJ_TAG
	default:
		return 0
	}
}
