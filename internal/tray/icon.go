package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"github.com/exodia/netpulse/internal/state"
)

const (
	iconSize  = 22
	iconColor = 9 // circle radius in px
)

var severityColors = map[state.Severity]color.RGBA{
	state.SevOk:       {0x4c, 0xaf, 0x50, 0xff},
	state.SevDegraded: {0xff, 0x98, 0x00, 0xff},
	state.SevDown:     {0xf4, 0x43, 0x36, 0xff},
}

// IconPNG renders a 22px dot in the severity color, PNG-encoded.
// Unknown severities fall back to the down color.
func IconPNG(sev state.Severity) []byte {
	c, ok := severityColors[sev]
	if !ok {
		c = severityColors[state.SevDown]
	}
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	center := float64(iconSize) / 2
	for y := range iconSize {
		for x := range iconSize {
			dx := float64(x) + 0.5 - center
			dy := float64(y) + 0.5 - center
			if dx*dx+dy*dy < iconColor*iconColor {
				img.Set(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	// png.Encode on a fixed RGBA never fails.
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
