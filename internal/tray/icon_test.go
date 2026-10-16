package tray_test

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/exodia/netpulse/internal/state"
	"github.com/exodia/netpulse/internal/tray"
)

func TestIconDiffersBySeverity(t *testing.T) {
	a := tray.IconPNG(state.SevOk)
	b := tray.IconPNG(state.SevDown)
	if bytes.Equal(a, b) {
		t.Fatal("severities must produce different icons")
	}
	img, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 22 {
		t.Fatal("want 22px")
	}
}
