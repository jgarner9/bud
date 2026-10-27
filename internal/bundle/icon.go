package bundle

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// robot is the README's [o.o] as pixel art: '#' is the robot, 'o' its eyes,
// '.' nothing. It's drawn centered on the icon's dark tile
var robot = strings.Fields(`
	.....######.....
	................
	.###........###.
	.#............#.
	.#...oo..oo...#.
	.#...oo..oo...#.
	.#............#.
	.#.....##.....#.
	.###........###.
	................
	....#.#####.#...
	...#..#####..#..
	..#...#####...#.
	......#####.....
	.......#.#......
	......##.##.....
`)

var (
	tile  = color.NRGBA{0x1e, 0x1e, 0x2e, 0xff} // the editor's background
	green = color.NRGBA{0x6c, 0xa3, 0x6c, 0xff} // the README's badge green
	eyes  = color.NRGBA{0xe6, 0xe9, 0xef, 0xff}
)

// iconPNG draws the icon size pixels square, in macOS's layout: a rounded
// tile filling the middle 80%, the rest transparent
func iconPNG(size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	margin := size / 10
	radius := float64(size) * 0.18
	lo, hi := float64(margin), float64(size-margin)
	for y := range size {
		for x := range size {
			if inRoundedRect(float64(x)+0.5, float64(y)+0.5, lo, hi, radius) {
				img.SetNRGBA(x, y, tile)
			}
		}
	}

	cols, rows := len(robot[0]), len(robot)
	cell := (size - 2*margin) * 6 / 10 / cols
	left := (size - cell*cols) / 2
	top := (size - cell*rows) / 2
	for r, line := range robot {
		for c, ch := range line {
			var col color.NRGBA
			switch ch {
			case '#':
				col = green
			case 'o':
				col = eyes
			default:
				continue
			}
			for y := top + r*cell; y < top+(r+1)*cell; y++ {
				for x := left + c*cell; x < left+(c+1)*cell; x++ {
					img.SetNRGBA(x, y, col)
				}
			}
		}
	}

	var buf bytes.Buffer
	// encoding an in-memory NRGBA can't fail
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func inRoundedRect(x, y, lo, hi, r float64) bool {
	if x < lo || x > hi || y < lo || y > hi {
		return false
	}
	cx := min(max(x, lo+r), hi-r)
	cy := min(max(y, lo+r), hi-r)
	return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
}

// icnsTypes are the ICNS entries that hold a PNG, by pixel size
var icnsTypes = []struct {
	kind string
	size int
}{
	{"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
	{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512},
}

// Icon returns the app icon as an .icns file: a header, then one PNG per
// size, each with its 4-byte type and big-endian length
func Icon() []byte {
	var body bytes.Buffer
	for _, t := range icnsTypes {
		data := iconPNG(t.size)
		body.WriteString(t.kind)
		binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}

	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}
