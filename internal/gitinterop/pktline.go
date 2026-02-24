package gitinterop

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

// pkt-line format: 4-byte hex length prefix + payload. "0000" is flush packet.

func pktLine(data string) []byte {
	l := len(data) + 4
	return []byte(fmt.Sprintf("%04x%s", l, data))
}

func pktFlush() []byte {
	return []byte("0000")
}

func readPktLine(r *bufio.Reader) ([]byte, error) {
	hexLen := make([]byte, 4)
	if _, err := io.ReadFull(r, hexLen); err != nil {
		return nil, err
	}
	l, err := strconv.ParseInt(string(hexLen), 16, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid pkt-line length: %s", hexLen)
	}
	if l == 0 {
		return nil, nil // flush packet
	}
	if l < 4 {
		return nil, fmt.Errorf("invalid pkt-line length: %d", l)
	}
	payload := make([]byte, l-4)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
