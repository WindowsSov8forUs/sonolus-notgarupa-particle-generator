// Package atlas packs named images into one texture (skyline/shelf packing)
// with edge extrusion so bilinear sampling never bleeds between sprites.
package atlas

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/draw"
	"sort"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/internal/imgutil"
)

// Item is one image to pack. Several names may share one image.
type Item struct {
	Names []string
	Image *image.NRGBA
}

// Placement is the rect of an item inside the atlas (top-left origin), not
// including the extrusion margin.
type Placement struct {
	Names      []string
	X, Y, W, H int
}

type Result struct {
	Texture    *image.NRGBA
	Placements []Placement
	byName     map[string]Placement
}

func (r *Result) Rect(name string) (Placement, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// Pack places items into a texture whose width is at most maxWidth. Identical
// images (byte-equal pixels) are stored once. `extrude` pixels of edge colour
// surround every sprite.
func Pack(items []Item, maxWidth, extrude int) (*Result, error) {
	type entry struct {
		names []string
		img   *image.NRGBA
	}
	var entries []*entry
	seen := map[[32]byte]*entry{}
	for _, it := range items {
		if it.Image == nil {
			return nil, fmt.Errorf("atlas: nil image for %v", it.Names)
		}
		h := sha256.New()
		fmt.Fprintf(h, "%dx%d", it.Image.Rect.Dx(), it.Image.Rect.Dy())
		h.Write(it.Image.Pix)
		var key [32]byte
		copy(key[:], h.Sum(nil))
		if e, ok := seen[key]; ok {
			e.names = append(e.names, it.Names...)
			continue
		}
		e := &entry{names: append([]string(nil), it.Names...), img: it.Image}
		seen[key] = e
		entries = append(entries, e)
	}
	order := append([]*entry(nil), entries...)
	sort.SliceStable(order, func(i, j int) bool {
		hi, hj := order[i].img.Rect.Dy(), order[j].img.Rect.Dy()
		if hi != hj {
			return hi > hj
		}
		return order[i].img.Rect.Dx() > order[j].img.Rect.Dx()
	})
	pad := extrude
	width := 0
	for _, e := range order {
		if w := e.img.Rect.Dx() + 2*pad; w > width {
			width = w
		}
	}
	width = max(width, maxWidth)
	type placed struct {
		e    *entry
		x, y int
	}
	var ps []placed
	x, y, shelfH := 0, 0, 0
	for _, e := range order {
		w, h := e.img.Rect.Dx()+2*pad, e.img.Rect.Dy()+2*pad
		if x+w > width {
			x, y, shelfH = 0, y+shelfH, 0
		}
		ps = append(ps, placed{e, x + pad, y + pad})
		x += w
		shelfH = max(shelfH, h)
	}
	height := y + shelfH
	tex := image.NewNRGBA(image.Rect(0, 0, width, height))
	res := &Result{Texture: tex, byName: map[string]Placement{}}
	for _, p := range ps {
		w, h := p.e.img.Rect.Dx(), p.e.img.Rect.Dy()
		draw.Draw(tex, image.Rect(p.x, p.y, p.x+w, p.y+h), p.e.img, image.Point{}, draw.Src)
		extrudeEdges(tex, p.x, p.y, w, h, pad)
		pl := Placement{Names: p.e.names, X: p.x, Y: p.y, W: w, H: h}
		res.Placements = append(res.Placements, pl)
		for _, n := range p.e.names {
			if _, dup := res.byName[n]; dup {
				return nil, fmt.Errorf("atlas: duplicate sprite name %q", n)
			}
			res.byName[n] = pl
		}
	}
	return res, nil
}

func extrudeEdges(tex *image.NRGBA, x, y, w, h, pad int) {
	if pad <= 0 || w == 0 || h == 0 {
		return
	}
	px := func(cx, cy int) []uint8 { return tex.Pix[tex.PixOffset(cx, cy):][:4] }
	for d := 1; d <= pad; d++ {
		for i := 0; i < w; i++ {
			copy(px(x+i, y-d), px(x+i, y))
			copy(px(x+i, y+h-1+d), px(x+i, y+h-1))
		}
		for j := -pad; j < h+pad; j++ {
			cy := min(max(y+j, y-pad), y+h-1+pad)
			copy(px(x-d, cy), px(x, cy))
			copy(px(x+w-1+d, cy), px(x+w-1, cy))
		}
	}
}

// PackPOT packs into the smallest power-of-two texture (both sides ≤ maxSide)
// that the shelf packer can fill; the Sonolus specs recommend power-of-two
// skin/particle textures no larger than 2048².
func PackPOT(items []Item, maxSide, extrude int) (*Result, error) {
	var best *Result
	need := 0
	for w := 64; w <= maxSide; w *= 2 {
		r, err := Pack(items, w, extrude)
		if err != nil {
			return nil, err
		}
		if r.Texture.Rect.Dx() > w {
			continue // an item is wider than this width
		}
		h := 1
		for h < r.Texture.Rect.Dy() {
			h *= 2
		}
		need = r.Texture.Rect.Dx() * r.Texture.Rect.Dy()
		if h > maxSide {
			continue
		}
		if best == nil || w*h < best.Texture.Rect.Dx()*best.Texture.Rect.Dy() {
			best = r.resized(w, h)
		}
	}
	if best == nil {
		return nil, fmt.Errorf("atlas: content (~%d px²) does not fit a %d×%d texture", need, maxSide, maxSide)
	}
	return best, nil
}

func (r *Result) resized(w, h int) *Result {
	tex := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(tex, r.Texture.Rect, r.Texture, image.Point{}, draw.Src)
	return &Result{Texture: tex, Placements: r.Placements, byName: r.byName}
}

// EncodeTexture is a convenience wrapper.
func (r *Result) EncodeTexture() ([]byte, error) { return imgutil.EncodePNG(r.Texture) }
