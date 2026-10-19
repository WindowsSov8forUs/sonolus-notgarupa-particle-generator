package unity

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// File is a parsed serialized file with type trees.
type File struct {
	data      []byte
	order     binary.ByteOrder
	types     []typeTree
	Objects   map[int64]Object
	Order     []int64 // path IDs in file order
	Externals []string
}

// Object is one serialized object.
type Object struct {
	PathID  int64
	ClassID int32
	offset  int
	size    int
	typ     int
}

type typeTree struct {
	classID int32
	nodes   []typeNode
}

type typeNode struct {
	level int
	typ   string
	name  string
	meta  uint32
}

const alignFlag = 0x4000

// Parse reads a serialized file (format 17–22) that embeds type trees.
func Parse(data []byte) (*File, error) {
	if len(data) < 48 {
		return nil, errors.New("serialized file too short")
	}
	h := &reader{b: data, order: binary.BigEndian}
	h.p = 8
	version := int(h.u32())
	if version < 17 || version > 22 {
		return nil, fmt.Errorf("unsupported serialized file version %d", version)
	}
	var dataOffset int
	h.p = 12
	dataOffset = int(h.u32())
	endian := data[16]
	r := &reader{b: data, order: binary.LittleEndian}
	r.p = 20
	if version >= 22 {
		r.order = binary.BigEndian
		r.p = 20
		r.u32() // metadata size
		r.u64() // file size
		dataOffset = int(r.u64())
		r.p = 48
	}
	if endian != 0 {
		r.order = binary.BigEndian
	} else {
		r.order = binary.LittleEndian
	}
	r.cstr() // engine version
	r.u32()  // target platform
	if r.u8() == 0 {
		return nil, errors.New("serialized file has no type trees")
	}
	f := &File{data: data, order: r.order, Objects: map[int64]Object{}}
	f.types = make([]typeTree, int32(r.u32()))
	for i := range f.types {
		t := &f.types[i]
		t.classID = int32(r.u32())
		r.u8() // stripped
		r.u16()
		if t.classID == 114 {
			r.bytes(16) // script hash
		}
		r.bytes(16) // type hash
		n, strLen := int(int32(r.u32())), int(int32(r.u32()))
		type rawNode struct {
			level          int
			typeOff, nmOff uint32
			meta           uint32
		}
		raw := make([]rawNode, n)
		for j := range raw {
			r.u16() // version
			raw[j].level = int(r.u8())
			r.u8() // type flags
			raw[j].typeOff = r.u32()
			raw[j].nmOff = r.u32()
			r.u32() // byte size
			r.u32() // index
			raw[j].meta = r.u32()
			if version >= 19 {
				r.u64() // ref type hash
			}
		}
		strs := r.bytes(strLen)
		str := func(off uint32) string {
			if off&0x80000000 != 0 {
				if s, ok := commonStrings[off&0x7fffffff]; ok {
					return s
				}
				return fmt.Sprintf("?%d", off&0x7fffffff)
			}
			end := int(off)
			for end < len(strs) && strs[end] != 0 {
				end++
			}
			return string(strs[off:end])
		}
		t.nodes = make([]typeNode, n)
		for j, rn := range raw {
			t.nodes[j] = typeNode{level: rn.level, typ: str(rn.typeOff), name: str(rn.nmOff), meta: rn.meta}
		}
		if version >= 21 {
			deps := int(int32(r.u32()))
			r.bytes(4 * deps)
		}
	}
	count := int(int32(r.u32()))
	for i := 0; i < count; i++ {
		r.align(4)
		o := Object{PathID: int64(r.u64())}
		if version >= 22 {
			o.offset = int(r.u64()) + dataOffset
		} else {
			o.offset = int(r.u32()) + dataOffset
		}
		o.size = int(r.u32())
		o.typ = int(int32(r.u32()))
		if o.typ < 0 || o.typ >= len(f.types) {
			return nil, fmt.Errorf("object %d has bad type index %d", o.PathID, o.typ)
		}
		o.ClassID = f.types[o.typ].classID
		f.Objects[o.PathID] = o
		f.Order = append(f.Order, o.PathID)
	}
	scripts := int(int32(r.u32()))
	for i := 0; i < scripts; i++ {
		r.u32()
		r.align(4)
		r.u64()
	}
	externals := int(int32(r.u32()))
	for i := 0; i < externals; i++ {
		r.cstr()
		r.bytes(16)
		r.u32()
		f.Externals = append(f.Externals, r.cstr())
	}
	if r.err != nil {
		return nil, r.err
	}
	return f, nil
}

// Read deserialises an object through its type tree. Numbers become float64,
// except 64-bit integers (path IDs), which stay int64 / uint64 to remain
// exact; arrays become []any and structures map[string]any; byte arrays and
// typeless data are skipped (their length is kept as {"bytes": n}).
func (f *File) Read(pathID int64) (map[string]any, error) {
	o, ok := f.Objects[pathID]
	if !ok {
		return nil, fmt.Errorf("no object %d", pathID)
	}
	nodes := f.types[o.typ].nodes
	r := &reader{b: f.data, order: f.order, p: o.offset}
	v := readNode(r, nodes, 0)
	if r.err != nil {
		return nil, fmt.Errorf("object %d: %w", pathID, r.err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("object %d is not a structure", pathID)
	}
	return m, nil
}

func children(nodes []typeNode, i int) []int {
	var out []int
	for j := i + 1; j < len(nodes) && nodes[j].level > nodes[i].level; j++ {
		if nodes[j].level == nodes[i].level+1 {
			out = append(out, j)
		}
	}
	return out
}

func readNode(r *reader, nodes []typeNode, i int) any {
	n := nodes[i]
	ch := children(nodes, i)
	if n.typ == "string" {
		size := int(int32(r.u32()))
		s := string(r.bytes(size))
		r.align(4)
		return s
	}
	if len(ch) == 0 {
		var v any
		switch n.typ {
		case "SInt8":
			v = float64(int8(r.u8()))
		case "UInt8", "char":
			v = float64(r.u8())
		case "bool":
			v = r.u8() != 0
		case "SInt16", "short":
			v = float64(int16(r.u16()))
		case "UInt16", "unsigned short":
			v = float64(r.u16())
		case "SInt32", "int":
			v = float64(int32(r.u32()))
		case "UInt32", "unsigned int", "Type*":
			v = float64(r.u32())
		case "float":
			v = float64(math.Float32frombits(r.u32()))
		case "double":
			v = math.Float64frombits(r.u64())
		case "SInt64", "long long":
			v = int64(r.u64())
		case "UInt64", "unsigned long long", "FileSize":
			v = r.u64()
		default:
			r.err = fmt.Errorf("unknown leaf type %s %s", n.typ, n.name)
			return nil
		}
		if n.meta&alignFlag != 0 {
			r.align(4)
		}
		return v
	}
	if n.typ == "TypelessData" {
		size := int(int32(r.u32()))
		r.bytes(size)
		r.align(4)
		return map[string]any{"bytes": float64(size)}
	}
	if nodes[ch[0]].typ == "Array" {
		arr := ch[0]
		ach := children(nodes, arr)
		size := int(int32(r.u32()))
		if r.err != nil || size < 0 || size > len(r.b) {
			r.err = fmt.Errorf("bad array size %d for %s", size, n.name)
			return nil
		}
		elem := ach[1]
		var out any
		if (nodes[elem].typ == "UInt8" || nodes[elem].typ == "char") && len(children(nodes, elem)) == 0 {
			r.bytes(size)
			out = map[string]any{"bytes": float64(size)}
		} else {
			list := make([]any, size)
			for k := range list {
				list[k] = readNode(r, nodes, elem)
				if r.err != nil {
					return nil
				}
			}
			out = list
		}
		if n.meta&alignFlag != 0 || nodes[arr].meta&alignFlag != 0 {
			r.align(4)
		}
		return out
	}
	m := map[string]any{}
	for _, c := range ch {
		m[nodes[c].name] = readNode(r, nodes, c)
		if r.err != nil {
			return nil
		}
	}
	if n.meta&alignFlag != 0 {
		r.align(4)
	}
	return m
}
