// Package testimage produces deterministic, original synthetic codec fixtures.
package testimage

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// GIF makes distinct transparent frames. Delays use GIF's centiseconds and
// loop uses image/gif's repeat count (-1 plays once, 0 repeats indefinitely).
func GIF(width, height int, delays []int, loop int) []byte {
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	animation := &gif.GIF{Delay: delays, LoopCount: loop}
	for i := range delays {
		frame := image.NewPaletted(image.Rect(0, 0, width, height), palette)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if x > 0 || width == 1 {
					frame.SetColorIndex(x, y, uint8(1+i%3))
				}
			}
		}
		animation.Image = append(animation.Image, frame)
		animation.Disposal = append(animation.Disposal, gif.DisposalBackground)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, animation); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func PNG(width, height int, transparent bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := uint8(255)
			if transparent {
				a = uint8((x*17 + y*3) % 256)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / max(width-1, 1)), G: uint8(y * 255 / max(height-1, 1)), B: uint8((x + y) % 256), A: a})
		}
	}
	if width > 50 && height > 30 {
		draw.Draw(img, image.Rect(4, 4, 45, 22), image.NewUniform(color.NRGBA{R: 230, G: 40, B: 20, A: 200}), image.Point{}, draw.Src)
		d := font.Drawer{Dst: img, Src: image.White, Face: basicfont.Face7x13, Dot: fixed.P(5, 18)}
		d.DrawString("Image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
