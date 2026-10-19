package unity

import (
	"encoding/binary"
	"fmt"
	"image"
	"path"
)

// Texture formats this package decodes.
const (
	FormatRGBA32     = 4
	FormatETCRGB4    = 34 // ETC1
	FormatETC2RGBA8  = 47 // ETC2 RGB + EAC alpha
	textureClassID   = 28
	textureMipLevel0 = 0
)

// DecodeTexture decodes the first mip level of a Texture2D object into a
// top-down RGBA image. files are the bundle's files (for the resource
// stream).
func DecodeTexture(f *File, files map[string][]byte, pathID int64) (*image.NRGBA, error) {
	t, err := f.Read(pathID)
	if err != nil {
		return nil, err
	}
	w, _ := t["m_Width"].(float64)
	h, _ := t["m_Height"].(float64)
	format, _ := t["m_TextureFormat"].(float64)
	width, height := int(w), int(h)
	stream, _ := t["m_StreamData"].(map[string]any)
	resPath, _ := stream["path"].(string)
	offset, _ := stream["offset"].(uint64)
	size, _ := stream["size"].(float64)
	if resPath == "" {
		return nil, fmt.Errorf("texture %d has no stream data", pathID)
	}
	res, ok := files[path.Base(resPath)]
	if !ok || int(offset)+int(size) > len(res) {
		return nil, fmt.Errorf("texture %d: stream %s not in bundle", pathID, resPath)
	}
	data := res[offset : int(offset)+int(size)]
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	// Unity stores rows bottom-up.
	set := func(x, y int, c [4]uint8) {
		if x < width && y < height {
			copy(img.Pix[img.PixOffset(x, height-1-y):], c[:])
		}
	}
	switch int(format) {
	case FormatRGBA32:
		if len(data) < width*height*4 {
			return nil, fmt.Errorf("texture %d: RGBA32 data too short", pathID)
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				p := (y*width + x) * 4
				set(x, y, [4]uint8{data[p], data[p+1], data[p+2], data[p+3]})
			}
		}
	case FormatETCRGB4, FormatETC2RGBA8:
		blockSize := 8
		if int(format) == FormatETC2RGBA8 {
			blockSize = 16
		}
		bw, bh := (width+3)/4, (height+3)/4
		if len(data) < bw*bh*blockSize {
			return nil, fmt.Errorf("texture %d: ETC data too short", pathID)
		}
		var px [16][4]uint8
		for by := 0; by < bh; by++ {
			for bx := 0; bx < bw; bx++ {
				block := data[(by*bw+bx)*blockSize:]
				rgb := block
				if blockSize == 16 {
					rgb = block[8:]
				}
				decodeETC2RGB(binary.BigEndian.Uint64(rgb), &px)
				if blockSize == 16 {
					decodeEACAlpha(block[:8], &px)
				} else {
					for i := range px {
						px[i][3] = 255
					}
				}
				for i := range px {
					set(bx*4+i%4, by*4+i/4, px[i])
				}
			}
		}
	default:
		return nil, fmt.Errorf("texture %d: unsupported format %d", pathID, int(format))
	}
	return img, nil
}

var etc1Modifiers = [8][2]int{{2, 8}, {5, 17}, {9, 29}, {13, 42}, {18, 60}, {24, 80}, {33, 106}, {47, 183}}
var etc2Distances = [8]int{3, 6, 11, 16, 23, 32, 41, 64}

func clamp8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func bits(v uint64, shift, n uint) int { return int(v>>shift) & (1<<n - 1) }

func ext4(x int) int { return x<<4 | x }
func ext5(x int) int { return x<<3 | x>>2 }
func ext6(x int) int { return x<<2 | x>>4 }
func ext7(x int) int { return x<<1 | x>>6 }

// decodeETC2RGB decodes one ETC1/ETC2 RGB block into px (row-major, x + 4y).
func decodeETC2RGB(v uint64, px *[16][4]uint8) {
	index := func(x, y int) int {
		i := uint(x*4 + y)
		return bits(v, i+16, 1)<<1 | bits(v, i, 1)
	}
	paint := func(c [3]int, d int) [4]uint8 {
		return [4]uint8{clamp8(c[0] + d), clamp8(c[1] + d), clamp8(c[2] + d), 255}
	}
	individual := func(c1, c2 [3]int) {
		flip := bits(v, 32, 1) == 1
		cw := [2]int{bits(v, 37, 3), bits(v, 34, 3)}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				sub := 0
				if (!flip && x >= 2) || (flip && y >= 2) {
					sub = 1
				}
				base := c1
				if sub == 1 {
					base = c2
				}
				m := etc1Modifiers[cw[sub]]
				var d int
				switch index(x, y) {
				case 0:
					d = m[0]
				case 1:
					d = m[1]
				case 2:
					d = -m[0]
				case 3:
					d = -m[1]
				}
				px[y*4+x] = paint(base, d)
			}
		}
	}
	if bits(v, 33, 1) == 0 {
		individual(
			[3]int{ext4(bits(v, 60, 4)), ext4(bits(v, 52, 4)), ext4(bits(v, 44, 4))},
			[3]int{ext4(bits(v, 56, 4)), ext4(bits(v, 48, 4)), ext4(bits(v, 40, 4))})
		return
	}
	signed3 := func(x int) int {
		if x >= 4 {
			return x - 8
		}
		return x
	}
	r, g, b := bits(v, 59, 5), bits(v, 51, 5), bits(v, 43, 5)
	r2, g2, b2 := r+signed3(bits(v, 56, 3)), g+signed3(bits(v, 48, 3)), b+signed3(bits(v, 40, 3))
	switch {
	case r2 < 0 || r2 > 31: // T mode
		c1 := [3]int{ext4(bits(v, 59, 2)<<2 | bits(v, 56, 2)), ext4(bits(v, 52, 4)), ext4(bits(v, 48, 4))}
		c2 := [3]int{ext4(bits(v, 44, 4)), ext4(bits(v, 40, 4)), ext4(bits(v, 36, 4))}
		d := etc2Distances[bits(v, 34, 2)<<1|bits(v, 32, 1)]
		set := [4][4]uint8{paint(c1, 0), paint(c2, d), paint(c2, 0), paint(c2, -d)}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				px[y*4+x] = set[index(x, y)]
			}
		}
	case g2 < 0 || g2 > 31: // H mode
		r1, g1 := bits(v, 59, 4), bits(v, 56, 3)<<1|bits(v, 52, 1)
		b1 := bits(v, 51, 1)<<3 | bits(v, 48, 2)<<1 | bits(v, 47, 1)
		rr2, gg2, bb2 := bits(v, 43, 4), bits(v, 40, 3)<<1|bits(v, 39, 1), bits(v, 35, 4)
		di := bits(v, 34, 1)<<2 | bits(v, 32, 1)<<1
		if r1<<8|g1<<4|b1 >= rr2<<8|gg2<<4|bb2 {
			di++
		}
		d := etc2Distances[di]
		c1 := [3]int{ext4(r1), ext4(g1), ext4(b1)}
		c2 := [3]int{ext4(rr2), ext4(gg2), ext4(bb2)}
		set := [4][4]uint8{paint(c1, d), paint(c1, -d), paint(c2, d), paint(c2, -d)}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				px[y*4+x] = set[index(x, y)]
			}
		}
	case b2 < 0 || b2 > 31: // planar mode
		o := [3]int{ext6(bits(v, 57, 6)), ext7(bits(v, 56, 1)<<6 | bits(v, 49, 6)),
			ext6(bits(v, 48, 1)<<5 | bits(v, 43, 2)<<3 | bits(v, 40, 2)<<1 | bits(v, 39, 1))}
		hc := [3]int{ext6(bits(v, 34, 5)<<1 | bits(v, 32, 1)), ext7(bits(v, 25, 7)), ext6(bits(v, 24, 1)<<5 | bits(v, 19, 5))}
		vc := [3]int{ext6(bits(v, 16, 3)<<3 | bits(v, 13, 3)), ext7(bits(v, 8, 5)<<2 | bits(v, 6, 2)), ext6(bits(v, 0, 6))}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				var c [4]uint8
				for k := 0; k < 3; k++ {
					c[k] = clamp8((x*(hc[k]-o[k]) + y*(vc[k]-o[k]) + 4*o[k] + 2) >> 2)
				}
				c[3] = 255
				px[y*4+x] = c
			}
		}
	default: // differential
		individual([3]int{ext5(r), ext5(g), ext5(b)}, [3]int{ext5(r2), ext5(g2), ext5(b2)})
	}
}

var eacModifiers = [16][8]int{
	{-3, -6, -9, -15, 2, 5, 8, 14}, {-3, -7, -10, -13, 2, 6, 9, 12}, {-2, -5, -8, -13, 1, 4, 7, 12},
	{-2, -4, -6, -13, 1, 3, 5, 12}, {-3, -6, -8, -12, 2, 5, 7, 11}, {-3, -7, -9, -11, 2, 6, 8, 10},
	{-4, -7, -8, -11, 3, 6, 7, 10}, {-3, -5, -8, -11, 2, 4, 7, 10}, {-2, -6, -8, -10, 1, 5, 7, 9},
	{-2, -5, -8, -10, 1, 4, 7, 9}, {-2, -4, -8, -10, 1, 3, 7, 9}, {-2, -5, -7, -10, 1, 4, 6, 9},
	{-3, -4, -7, -10, 2, 3, 6, 9}, {-1, -2, -3, -10, 0, 1, 2, 9}, {-4, -6, -8, -9, 3, 5, 7, 8},
	{-3, -5, -7, -9, 2, 4, 6, 8},
}

// decodeEACAlpha decodes an ETC2 EAC alpha block into px's alpha channel.
func decodeEACAlpha(block []byte, px *[16][4]uint8) {
	base := int(block[0])
	mul := int(block[1] >> 4)
	table := eacModifiers[block[1]&15]
	var idx uint64
	for _, b := range block[2:8] {
		idx = idx<<8 | uint64(b)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if mul == 0 {
				px[y*4+x][3] = uint8(base)
				continue
			}
			i := uint(x*4 + y)
			px[y*4+x][3] = clamp8(base + table[(idx>>(45-3*i))&7]*mul)
		}
	}
}
