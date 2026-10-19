// Package imgutil contains the small set of raster operations the generators
// need: decoding, cropping, tinting, scaling and PNG encoding. All images are
// handled as non-premultiplied RGBA (image.NRGBA).
package imgutil

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	xdraw "golang.org/x/image/draw"
)

func Decode(data []byte) (*image.NRGBA, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return ToNRGBA(img), nil
}

func ToNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}

func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Crop copies a rectangle given with a top-left origin.
func Crop(src *image.NRGBA, x, y, w, h int) (*image.NRGBA, error) {
	r := image.Rect(x, y, x+w, y+h)
	if !r.In(src.Rect) {
		return nil, fmt.Errorf("crop %v outside image %v", r, src.Rect)
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Rect, src, r.Min, draw.Src)
	return out, nil
}

// CropBottomLeft copies a rectangle given with Unity's bottom-left origin.
func CropBottomLeft(src *image.NRGBA, x, y, w, h int) (*image.NRGBA, error) {
	return Crop(src, x, src.Rect.Dy()-y-h, w, h)
}

// Tint multiplies every pixel by an RGBA colour (components 0..1), as Unity's
// vertex colour does on a normal-blended sprite.
func Tint(src *image.NRGBA, r, g, b, a float64) *image.NRGBA {
	out := image.NewNRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		out.Pix[i+0] = mul8(src.Pix[i+0], r)
		out.Pix[i+1] = mul8(src.Pix[i+1], g)
		out.Pix[i+2] = mul8(src.Pix[i+2], b)
		out.Pix[i+3] = mul8(src.Pix[i+3], a)
	}
	return out
}

// AdditiveToAlpha converts art meant for additive blending (colour on black)
// into white-ish colour with alpha so that normal alpha blending gives a
// similar result on dark backgrounds: alpha = max channel, rgb = rgb/alpha.
func AdditiveToAlpha(src *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		a := float64(src.Pix[i+3]) / 255
		r := float64(src.Pix[i+0]) / 255 * a
		g := float64(src.Pix[i+1]) / 255 * a
		b := float64(src.Pix[i+2]) / 255 * a
		m := math.Max(r, math.Max(g, b))
		if m <= 0 {
			continue
		}
		out.Pix[i+0] = to8(r / m)
		out.Pix[i+1] = to8(g / m)
		out.Pix[i+2] = to8(b / m)
		out.Pix[i+3] = to8(m)
	}
	return out
}

// Scale resamples to an exact size with a Catmull-Rom filter.
func Scale(src *image.NRGBA, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Rect, src, src.Rect, xdraw.Src, nil)
	return out
}

// FlipX mirrors horizontally.
func FlipX(src *image.NRGBA) *image.NRGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewNRGBA(src.Rect)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			copy(out.Pix[out.PixOffset(w-1-x, y):][:4], src.Pix[src.PixOffset(x, y):][:4])
		}
	}
	return out
}

// Solid returns a w×h image of one colour.
func Solid(w, h int, c color.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return out
}

// Over composites src over dst at (x, y) with normal alpha blending.
func Over(dst *image.NRGBA, src image.Image, x, y int) {
	draw.Draw(dst, src.Bounds().Add(image.Pt(x, y)), src, src.Bounds().Min, draw.Over)
}

func mul8(v uint8, f float64) uint8 { return to8(float64(v) / 255 * f) }

func to8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(v * 255))
}

// SRGBToLinear and LinearToSRGB convert one channel (0..1).
func SRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func LinearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}
