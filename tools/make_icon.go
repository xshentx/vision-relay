//go:build ignore

package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var letters = map[rune][]string{
	'V': {
		"10001",
		"10001",
		"10001",
		"10001",
		"01010",
		"01010",
		"00100",
	},
	'R': {
		"11110",
		"10001",
		"10001",
		"11110",
		"10100",
		"10010",
		"10001",
	},
}

func main() {
	must(os.MkdirAll(filepath.Join("backend", "internal", "server", "assets"), 0o755))
	iconSizes := []int{16, 24, 32, 48, 64, 128, 256}
	must(writeICO(filepath.Join("backend", "internal", "server", "assets", "app.ico"), iconSizes))
	must(writeICO(filepath.Join("frontend", "favicon.ico"), []int{16, 32}))
	f, err := os.Create(filepath.Join("backend", "internal", "server", "assets", "app.png"))
	must(err)
	defer f.Close()
	must(png.Encode(f, makeIcon(256)))
}

func writeICO(path string, sizes []int) error {
	type entry struct {
		size int
		data []byte
	}
	entries := make([]entry, 0, len(sizes))
	for _, size := range sizes {
		data, err := dibBytes(makeIcon(size))
		if err != nil {
			return err
		}
		entries = append(entries, entry{size: size, data: data})
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint16(0))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(len(entries)))
	offset := 6 + len(entries)*16
	for _, item := range entries {
		width := byte(item.size)
		height := byte(item.size)
		if item.size >= 256 {
			width = 0
			height = 0
		}
		out.WriteByte(width)
		out.WriteByte(height)
		out.WriteByte(0)
		out.WriteByte(0)
		binary.Write(&out, binary.LittleEndian, uint16(1))
		binary.Write(&out, binary.LittleEndian, uint16(32))
		binary.Write(&out, binary.LittleEndian, uint32(len(item.data)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(item.data)
	}
	for _, item := range entries {
		out.Write(item.data)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func dibBytes(img *image.RGBA) ([]byte, error) {
	size := img.Bounds().Dx()
	maskStride := ((size + 31) / 32) * 4
	maskBytes := maskStride * size
	pixelBytes := size * size * 4
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint32(40))
	binary.Write(&out, binary.LittleEndian, int32(size))
	binary.Write(&out, binary.LittleEndian, int32(size*2))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(32))
	binary.Write(&out, binary.LittleEndian, uint32(0))
	binary.Write(&out, binary.LittleEndian, uint32(pixelBytes+maskBytes))
	binary.Write(&out, binary.LittleEndian, int32(0))
	binary.Write(&out, binary.LittleEndian, int32(0))
	binary.Write(&out, binary.LittleEndian, uint32(0))
	binary.Write(&out, binary.LittleEndian, uint32(0))
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			out.WriteByte(byte(b >> 8))
			out.WriteByte(byte(g >> 8))
			out.WriteByte(byte(r >> 8))
			out.WriteByte(byte(a >> 8))
		}
	}
	out.Write(make([]byte, maskBytes))
	return out.Bytes(), nil
}

func makeIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.Transparent, image.Point{}, draw.Src)
	radius := float64(size) * 0.2
	for y := 0; y < size; y++ {
		t := float64(y) / math.Max(1, float64(size-1))
		c := mix(color.RGBA{23, 32, 44, 255}, color.RGBA{37, 99, 235, 255}, t)
		for x := 0; x < size; x++ {
			if inRoundRect(float64(x)+0.5, float64(y)+0.5, float64(size), float64(size), radius) {
				img.SetRGBA(x, y, c)
			}
		}
	}
	border := max(1, size/32)
	inset := max(2, size/10)
	drawRoundOutline(img, inset, inset, size-inset-1, size-inset-1, max(2, size/7), border, color.RGBA{255, 255, 255, 48})
	drawText(img, "VR", size)
	node := max(3, size/9)
	drawCircle(img, size-inset-node/2, inset+node/2, node/2, color.RGBA{15, 159, 110, 255})
	return img
}

func drawText(img *image.RGBA, text string, size int) {
	cell := max(1, size/12)
	gap := max(1, cell/2)
	width := len(text)*5*cell + (len(text)-1)*gap
	height := 7 * cell
	x := (size - width) / 2
	y := (size-height)/2 - size/32
	for _, ch := range text {
		pattern := letters[ch]
		for row, line := range pattern {
			for col, bit := range line {
				if bit == '1' {
					fillRect(img, x+col*cell, y+row*cell, cell, cell, color.RGBA{255, 255, 255, 255})
				}
			}
		}
		x += 5*cell + gap
	}
}

func fillRect(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if image.Pt(xx, yy).In(img.Bounds()) {
				img.SetRGBA(xx, yy, c)
			}
		}
	}
}

func drawRoundOutline(img *image.RGBA, x0, y0, x1, y1, radius, width int, c color.RGBA) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			outer := inRoundRect(float64(x)+0.5-float64(x0), float64(y)+0.5-float64(y0), float64(x1-x0+1), float64(y1-y0+1), float64(radius))
			inner := inRoundRect(float64(x)+0.5-float64(x0+width), float64(y)+0.5-float64(y0+width), float64(x1-x0+1-width*2), float64(y1-y0+1-width*2), float64(max(0, radius-width)))
			if outer && !inner {
				blend(img, x, y, c)
			}
		}
	}
}

func drawCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	rr := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= rr && image.Pt(x, y).In(img.Bounds()) {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func inRoundRect(x, y, w, h, r float64) bool {
	if x < 0 || y < 0 || x >= w || y >= h {
		return false
	}
	cx := math.Min(math.Max(x, r), w-r)
	cy := math.Min(math.Max(y, r), h-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 255,
	}
}

func blend(img *image.RGBA, x, y int, src color.RGBA) {
	if !image.Pt(x, y).In(img.Bounds()) {
		return
	}
	dst := img.RGBAAt(x, y)
	a := float64(src.A) / 255
	img.SetRGBA(x, y, color.RGBA{
		R: uint8(float64(src.R)*a + float64(dst.R)*(1-a)),
		G: uint8(float64(src.G)*a + float64(dst.G)*(1-a)),
		B: uint8(float64(src.B)*a + float64(dst.B)*(1-a)),
		A: 255,
	})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
