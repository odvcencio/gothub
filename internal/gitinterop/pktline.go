package gitinterop

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// pkt-line format: 4-byte hex length prefix + payload. "0000" is flush packet.

func pktLine(data string) []byte {
	l := len(data) + 4
	return []byte(fmt.Sprintf("%04x%s", l, data))
}

func pktLineBytes(payload []byte) []byte {
	l := len(payload) + 4
	header := []byte(fmt.Sprintf("%04x", l))
	out := make([]byte, 0, len(header)+len(payload))
	out = append(out, header...)
	out = append(out, payload...)
	return out
}

func pktFlush() []byte {
	return []byte("0000")
}

const sidebandMaxChunk = 65520

func sidebandPacket(channel byte, payload []byte) []byte {
	frame := make([]byte, 1+len(payload))
	frame[0] = channel
	copy(frame[1:], payload)
	return pktLineBytes(frame)
}

func writeSideband(w io.Writer, channel byte, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	for len(payload) > 0 {
		chunkSize := sidebandMaxChunk
		if len(payload) < chunkSize {
			chunkSize = len(payload)
		}
		chunk := payload[:chunkSize]
		if _, err := w.Write(sidebandPacket(channel, chunk)); err != nil {
			return err
		}
		payload = payload[chunkSize:]
	}
	return nil
}

func splitPktPayloadAndCapabilities(line string) (string, map[string]bool) {
	payload := line
	caps := map[string]bool{}
	if idx := strings.IndexByte(line, 0); idx >= 0 {
		payload = line[:idx]
		rawCaps := line[idx+1:]
		for _, capName := range bytes.Fields([]byte(rawCaps)) {
			if len(capName) == 0 {
				continue
			}
			caps[string(capName)] = true
		}
	}
	return payload, caps
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
