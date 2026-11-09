package shape

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/usmanvirk/mobile-bff/internal/upstream"
)

// Device is what the client told us about itself.
//
// Width in points and a scale factor, which is what a phone actually knows,
// rather than a pixel width the client would have to compute and would
// compute differently on each platform.
type Device struct {
	WidthPoints int
	Scale       float64
	// AVIF and WebP support, which is a question of the OS version rather
	// than of the device.
	AcceptsAVIF bool
	AcceptsWebP bool
}

// DefaultDevice is what an unknown client gets: a mid sized phone at 2x, and
// no modern image format. Guessing high would send a tablet sized image to a
// phone on a train, which is the case this service exists to avoid.
var DefaultDevice = Device{WidthPoints: 390, Scale: 2}

// Slot is how much of the screen width an image occupies.
type Slot float64

const (
	// ThumbnailWidth is a card in a two column grid, so a little under half
	// the screen after the gutters.
	ThumbnailWidth Slot = 0.46
	// FullWidth is the detail screen gallery.
	FullWidth Slot = 1.0
)

// ParseDevice reads the client hints. Anything missing or unreadable falls
// back to the default rather than failing the request: a bad header is not a
// reason to show somebody an error screen.
func ParseDevice(widthPoints, scale, accept string) Device {
	d := DefaultDevice
	if w, err := strconv.Atoi(strings.TrimSpace(widthPoints)); err == nil && w >= 200 && w <= 2000 {
		d.WidthPoints = w
	}
	if s, err := strconv.ParseFloat(strings.TrimSpace(scale), 64); err == nil && s >= 1 && s <= 4 {
		d.Scale = s
	}
	accept = strings.ToLower(accept)
	d.AcceptsAVIF = strings.Contains(accept, "image/avif")
	d.AcceptsWebP = strings.Contains(accept, "image/webp")
	return d
}

// TargetPixels is the width the image should be rendered at.
func (d Device) TargetPixels(slot Slot) int {
	px := float64(d.WidthPoints) * d.Scale * float64(slot)
	if px < 1 {
		px = 1
	}
	return int(px)
}

// PickImage chooses one variant and returns its URL.
//
// The rule is the smallest variant at least as wide as the slot needs, and
// the widest available if none reaches it. Upscaling on the device is
// visible and sending a 2000px image to fill a 180px card is not: the second
// is the one that costs the customer their data, so the bias is deliberate.
//
// Format is preferred before size, because AVIF at the same width is
// routinely a third of the bytes of JPEG, which is a larger saving than one
// step of width.
func PickImage(images []upstream.Image, d Device, slot Slot) string {
	if len(images) == 0 {
		return ""
	}
	img := images[0]
	for _, candidate := range images {
		if candidate.Position < img.Position {
			img = candidate
		}
	}
	if len(img.Variants) == 0 {
		return ""
	}

	want := d.TargetPixels(slot)
	allowed := func(f string) bool {
		switch strings.ToLower(f) {
		case "avif":
			return d.AcceptsAVIF
		case "webp":
			return d.AcceptsWebP
		default:
			return true
		}
	}

	rank := func(f string) int {
		switch strings.ToLower(f) {
		case "avif":
			return 0
		case "webp":
			return 1
		default:
			return 2
		}
	}

	var best *upstream.ImageVariant
	for i := range img.Variants {
		v := img.Variants[i]
		if !allowed(v.Format) {
			continue
		}
		if best == nil {
			best = &img.Variants[i]
			continue
		}
		if better(v, *best, want, rank) {
			best = &img.Variants[i]
		}
	}
	if best == nil {
		return ""
	}
	return best.URL
}

// better reports whether a beats b for a slot that wants `want` pixels.
func better(a, b upstream.ImageVariant, want int, rank func(string) int) bool {
	aFits, bFits := a.Width >= want, b.Width >= want

	// A variant that covers the slot always beats one that does not, whatever
	// its format, because the alternative is a visibly soft image.
	if aFits != bFits {
		return aFits
	}

	if ra, rb := rank(a.Format), rank(b.Format); ra != rb {
		return ra < rb
	}

	if aFits {
		return a.Width < b.Width // the smallest that still covers it
	}
	return a.Width > b.Width // nothing covers it, so the largest there is
}

func (d Device) String() string {
	return fmt.Sprintf("%dpt@%.1fx", d.WidthPoints, d.Scale)
}
