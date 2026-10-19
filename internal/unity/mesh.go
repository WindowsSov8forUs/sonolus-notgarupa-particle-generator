package unity

import (
	"fmt"
	"math"
	"path"
)

// Mesh is the triangle geometry of a Unity Mesh: positions, first texture
// coordinates and optional vertex colours, with triangle indices.
type Mesh struct {
	Name      string
	Positions [][3]float64
	UV        [][2]float64
	Colors    [][4]float64 // nil when the mesh has no colour channel
	Triangles [][3]int
}

// Vertex channel formats (Unity 2019+).
var vertexFormatSize = map[int]int{0: 4, 1: 2, 2: 1, 3: 1, 4: 2, 5: 2, 6: 1, 7: 1, 8: 2, 9: 2, 10: 4, 11: 4}

// DecodeMesh decodes a Mesh object (class 43). files are the bundle's files
// (for vertex data streamed to a resource file).
func DecodeMesh(f *File, files map[string][]byte, pathID int64) (*Mesh, error) {
	m, err := f.ReadFull(pathID)
	if err != nil {
		return nil, err
	}
	out := &Mesh{Name: str(m, "m_Name")}
	vd, _ := m["m_VertexData"].(map[string]any)
	count := int(num(vd, "m_VertexCount"))
	data, _ := vd["m_DataSize"].([]byte)
	if sd, ok := m["m_StreamData"].(map[string]any); ok && len(data) == 0 {
		if p, _ := sd["path"].(string); p != "" {
			res := files[path.Base(p)]
			off, _ := sd["offset"].(uint64)
			size := int(num(sd, "size"))
			if res == nil || int(off)+size > len(res) {
				return nil, fmt.Errorf("mesh %s: stream %s not in bundle", out.Name, p)
			}
			data = res[off : int(off)+size]
		}
	}
	type channel struct{ stream, offset, format, dim int }
	var channels []channel
	list, _ := vd["m_Channels"].([]any)
	for _, c := range list {
		cm, _ := c.(map[string]any)
		channels = append(channels, channel{int(num(cm, "stream")), int(num(cm, "offset")), int(num(cm, "format")), int(num(cm, "dimension")) & 0xF})
	}
	// stream layout: interleaved channels per stream, streams aligned to 16
	strides := map[int]int{}
	for _, c := range channels {
		if c.dim == 0 {
			continue
		}
		size, ok := vertexFormatSize[c.format]
		if !ok {
			return nil, fmt.Errorf("mesh %s: vertex format %d", out.Name, c.format)
		}
		strides[c.stream] = max(strides[c.stream], c.offset+size*c.dim)
	}
	starts := map[int]int{}
	pos := 0
	for s := 0; s < 4; s++ {
		if strides[s] == 0 {
			continue
		}
		starts[s] = pos
		pos += strides[s] * count
		pos = (pos + 15) &^ 15
	}
	read := func(ci, v int) ([]float64, error) {
		c := channels[ci]
		size := vertexFormatSize[c.format]
		base := starts[c.stream] + v*strides[c.stream] + c.offset
		if base+size*c.dim > len(data) {
			return nil, fmt.Errorf("mesh %s: vertex data too short", out.Name)
		}
		vals := make([]float64, c.dim)
		for k := range vals {
			b := data[base+k*size:]
			switch c.format {
			case 0:
				vals[k] = float64(math.Float32frombits(f.order.Uint32(b)))
			case 1:
				vals[k] = halfToFloat(f.order.Uint16(b))
			case 2:
				vals[k] = float64(b[0]) / 255
			case 3:
				vals[k] = math.Max(float64(int8(b[0]))/127, -1)
			case 4:
				vals[k] = float64(f.order.Uint16(b)) / 65535
			case 5:
				vals[k] = math.Max(float64(int16(f.order.Uint16(b)))/32767, -1)
			default:
				return nil, fmt.Errorf("mesh %s: unsupported vertex format %d", out.Name, c.format)
			}
		}
		return vals, nil
	}
	has := func(i int) bool { return i < len(channels) && channels[i].dim > 0 }
	if !has(0) {
		return nil, fmt.Errorf("mesh %s has no positions", out.Name)
	}
	for v := 0; v < count; v++ {
		p, err := read(0, v)
		if err != nil {
			return nil, err
		}
		out.Positions = append(out.Positions, [3]float64{p[0], p[1], p[2]})
		uv := [2]float64{}
		if has(4) {
			t, err := read(4, v)
			if err != nil {
				return nil, err
			}
			uv = [2]float64{t[0], t[1]}
		}
		out.UV = append(out.UV, uv)
		if has(3) {
			c, err := read(3, v)
			if err != nil {
				return nil, err
			}
			for len(c) < 4 {
				c = append(c, 1)
			}
			out.Colors = append(out.Colors, [4]float64{c[0], c[1], c[2], c[3]})
		}
	}
	ib, _ := m["m_IndexBuffer"].([]byte)
	wide := num(m, "m_IndexFormat") == 1
	index := func(i int) int {
		if wide {
			return int(f.order.Uint32(ib[i*4:]))
		}
		return int(f.order.Uint16(ib[i*2:]))
	}
	stride := 2
	if wide {
		stride = 4
	}
	subs, _ := m["m_SubMeshes"].([]any)
	for _, s := range subs {
		sm, _ := s.(map[string]any)
		if num(sm, "topology") != 0 {
			return nil, fmt.Errorf("mesh %s: non-triangle topology", out.Name)
		}
		first := int(num(sm, "firstByte")) / stride
		n := int(num(sm, "indexCount"))
		baseVertex := int(num(sm, "baseVertex"))
		if (first+n)*stride > len(ib) {
			return nil, fmt.Errorf("mesh %s: index buffer too short", out.Name)
		}
		for i := 0; i+2 < n; i += 3 {
			tri := [3]int{index(first+i) + baseVertex, index(first+i+1) + baseVertex, index(first+i+2) + baseVertex}
			if tri[0] >= count || tri[1] >= count || tri[2] >= count {
				return nil, fmt.Errorf("mesh %s: index out of range", out.Name)
			}
			out.Triangles = append(out.Triangles, tri)
		}
	}
	return out, nil
}

func halfToFloat(h uint16) float64 {
	sign := 1.0
	if h&0x8000 != 0 {
		sign = -1
	}
	exp := int(h>>10) & 0x1F
	frac := float64(h & 0x3FF)
	switch exp {
	case 0:
		return sign * frac * math.Pow(2, -24)
	case 31:
		return sign * math.Inf(1)
	}
	return sign * (1 + frac/1024) * math.Pow(2, float64(exp-15))
}

func num(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case uint64:
		return float64(v)
	case int64:
		return float64(v)
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
