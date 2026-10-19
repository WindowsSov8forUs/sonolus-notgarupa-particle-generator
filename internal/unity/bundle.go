// Package unity reads original-game Unity asset bundles (UnityFS) and the
// serialized files inside them, deserialising objects through their embedded
// type trees into generic maps.
package unity

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ReadBundle returns the files of a UnityFS bundle by name (the serialized
// file "CAB-…" and its ".resS" resource stream).
func ReadBundle(data []byte) (map[string][]byte, error) {
	r := &reader{b: data, order: binary.BigEndian}
	if r.cstr() != "UnityFS" {
		return nil, errors.New("not a UnityFS bundle")
	}
	format := r.u32()
	r.cstr() // engine version
	r.cstr() // engine revision
	r.u64()  // total size
	compressedInfo, uncompressedInfo, flags := int(r.u32()), int(r.u32()), r.u32()
	if format >= 7 {
		r.align(16)
	}
	var infoRaw []byte
	if flags&0x80 != 0 { // block info at the end
		infoRaw = data[len(data)-compressedInfo:]
	} else {
		infoRaw = r.bytes(compressedInfo)
	}
	info, err := decompress(infoRaw, uncompressedInfo, flags&0x3f)
	if err != nil {
		return nil, fmt.Errorf("block info: %w", err)
	}
	ir := &reader{b: info, order: binary.BigEndian}
	ir.bytes(16) // uncompressed data hash
	type block struct {
		usize, csize int
		flags        uint16
	}
	blocks := make([]block, ir.u32())
	for i := range blocks {
		blocks[i] = block{int(ir.u32()), int(ir.u32()), ir.u16()}
	}
	type node struct {
		offset, size int64
		name         string
	}
	nodes := make([]node, ir.u32())
	for i := range nodes {
		nodes[i].offset, nodes[i].size = int64(ir.u64()), int64(ir.u64())
		ir.u32()
		nodes[i].name = ir.cstr()
	}
	if flags&0x200 != 0 {
		r.align(16)
	}
	var raw bytes.Buffer
	for _, b := range blocks {
		out, err := decompress(r.bytes(b.csize), b.usize, uint32(b.flags&0x3f))
		if err != nil {
			return nil, err
		}
		raw.Write(out)
	}
	all := raw.Bytes()
	files := map[string][]byte{}
	for _, n := range nodes {
		if n.offset+n.size > int64(len(all)) {
			return nil, fmt.Errorf("node %s exceeds bundle data", n.name)
		}
		files[n.name] = all[n.offset : n.offset+n.size]
	}
	return files, nil
}

func decompress(src []byte, size int, kind uint32) ([]byte, error) {
	switch kind {
	case 0:
		return src, nil
	case 2, 3: // LZ4, LZ4HC
		return lz4Block(src, size)
	}
	return nil, fmt.Errorf("unsupported bundle compression %d", kind)
}

// lz4Block decodes one raw LZ4 block.
func lz4Block(src []byte, size int) ([]byte, error) {
	dst := make([]byte, 0, size)
	for i := 0; i < len(src); {
		token := src[i]
		i++
		n := int(token >> 4)
		if n == 15 {
			for {
				if i >= len(src) {
					return nil, errors.New("lz4: truncated literal length")
				}
				b := src[i]
				i++
				n += int(b)
				if b != 255 {
					break
				}
			}
		}
		if i+n > len(src) {
			return nil, errors.New("lz4: truncated literals")
		}
		dst = append(dst, src[i:i+n]...)
		i += n
		if i >= len(src) {
			break
		}
		if i+2 > len(src) {
			return nil, errors.New("lz4: truncated offset")
		}
		offset := int(src[i]) | int(src[i+1])<<8
		i += 2
		m := int(token & 15)
		if m == 15 {
			for {
				if i >= len(src) {
					return nil, errors.New("lz4: truncated match length")
				}
				b := src[i]
				i++
				m += int(b)
				if b != 255 {
					break
				}
			}
		}
		m += 4
		start := len(dst) - offset
		if offset == 0 || start < 0 {
			return nil, errors.New("lz4: bad offset")
		}
		for k := 0; k < m; k++ {
			dst = append(dst, dst[start+k])
		}
	}
	if len(dst) != size {
		return nil, fmt.Errorf("lz4: %d bytes, want %d", len(dst), size)
	}
	return dst, nil
}

type reader struct {
	b     []byte
	p     int
	order binary.ByteOrder
	err   error
}

func (r *reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if r.p+n > len(r.b) || n < 0 {
		r.err = fmt.Errorf("read past end at %d (+%d of %d)", r.p, n, len(r.b))
		return false
	}
	return true
}

func (r *reader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	v := r.b[r.p : r.p+n]
	r.p += n
	return v
}

func (r *reader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.p]
	r.p++
	return v
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := r.order.Uint16(r.b[r.p:])
	r.p += 2
	return v
}

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := r.order.Uint32(r.b[r.p:])
	r.p += 4
	return v
}

func (r *reader) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := r.order.Uint64(r.b[r.p:])
	r.p += 8
	return v
}

func (r *reader) cstr() string {
	if r.err != nil {
		return ""
	}
	end := bytes.IndexByte(r.b[r.p:], 0)
	if end < 0 {
		r.err = errors.New("unterminated string")
		return ""
	}
	v := string(r.b[r.p : r.p+end])
	r.p += end + 1
	return v
}

func (r *reader) align(n int) { r.p = (r.p + n - 1) / n * n }
