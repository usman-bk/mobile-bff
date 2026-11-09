package shape_test

import (
	"strings"
	"testing"

	"github.com/usmanvirk/mobile-bff/internal/shape"
	"github.com/usmanvirk/mobile-bff/internal/upstream"
)

func img(variants ...upstream.ImageVariant) []upstream.Image {
	return []upstream.Image{{ID: "i1", Position: 0, Variants: variants}}
}

func v(w int, format string) upstream.ImageVariant {
	return upstream.ImageVariant{
		URL:   strings.Join([]string{"https://img/", itoa(w), ".", format}, ""),
		Width: w, Height: w * 4 / 3, Format: format,
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestPicksTheSmallestVariantThatCoversTheSlot(t *testing.T) {
	images := img(v(180, "jpeg"), v(360, "jpeg"), v(720, "jpeg"), v(2000, "jpeg"))
	// 390pt at 2x in a 0.46 slot wants about 358 pixels.
	got := shape.PickImage(images, shape.Device{WidthPoints: 390, Scale: 2}, shape.ThumbnailWidth)
	if got != "https://img/360.jpeg" {
		t.Fatalf("picked %s, want the 360", got)
	}
}

// TestNeverUpscales. A soft image is visible and an oversized one is not, so
// the tie is broken towards bytes, but not past the point of blurring.
func TestNeverUpscalesWhenAVariantCovers(t *testing.T) {
	images := img(v(180, "jpeg"), v(360, "jpeg"))
	got := shape.PickImage(images, shape.Device{WidthPoints: 430, Scale: 3}, shape.FullWidth)
	if got != "https://img/360.jpeg" {
		t.Fatalf("picked %s, want the largest available when nothing covers the slot", got)
	}
}

func TestPrefersAVIFThenWebPWhenAccepted(t *testing.T) {
	images := img(v(360, "jpeg"), v(360, "webp"), v(360, "avif"))

	both := shape.Device{WidthPoints: 390, Scale: 2, AcceptsAVIF: true, AcceptsWebP: true}
	if got := shape.PickImage(images, both, shape.ThumbnailWidth); got != "https://img/360.avif" {
		t.Fatalf("a client accepting avif got %s", got)
	}

	webpOnly := shape.Device{WidthPoints: 390, Scale: 2, AcceptsWebP: true}
	if got := shape.PickImage(images, webpOnly, shape.ThumbnailWidth); got != "https://img/360.webp" {
		t.Fatalf("a client accepting webp but not avif got %s", got)
	}

	neither := shape.Device{WidthPoints: 390, Scale: 2}
	if got := shape.PickImage(images, neither, shape.ThumbnailWidth); got != "https://img/360.jpeg" {
		t.Fatalf("a client accepting neither got %s, which it cannot decode", got)
	}
}

// TestSizeBeatsFormat. A correctly sized JPEG is better than an AVIF that is
// too small, because the second one is visibly soft and the first is only
// larger.
func TestCoverageBeatsFormat(t *testing.T) {
	images := img(v(180, "avif"), v(360, "jpeg"))
	d := shape.Device{WidthPoints: 390, Scale: 2, AcceptsAVIF: true}
	if got := shape.PickImage(images, d, shape.ThumbnailWidth); got != "https://img/360.jpeg" {
		t.Fatalf("picked %s, want the jpeg that actually covers the slot", got)
	}
}

func TestADenserScreenGetsALargerImage(t *testing.T) {
	images := img(v(180, "jpeg"), v(360, "jpeg"), v(720, "jpeg"), v(1080, "jpeg"))
	oneX := shape.PickImage(images, shape.Device{WidthPoints: 390, Scale: 1}, shape.ThumbnailWidth)
	threeX := shape.PickImage(images, shape.Device{WidthPoints: 390, Scale: 3}, shape.ThumbnailWidth)
	if oneX == threeX {
		t.Fatalf("a 1x and a 3x screen both got %s", oneX)
	}
}

func TestNoImagesAndNoVariants(t *testing.T) {
	if got := shape.PickImage(nil, shape.DefaultDevice, shape.FullWidth); got != "" {
		t.Fatalf("a listing with no images produced %q", got)
	}
	empty := []upstream.Image{{ID: "i1"}}
	if got := shape.PickImage(empty, shape.DefaultDevice, shape.FullWidth); got != "" {
		t.Fatalf("an image with no variants produced %q", got)
	}
}

func TestPicksThePrimaryPhoto(t *testing.T) {
	images := []upstream.Image{
		{ID: "second", Position: 1, Variants: []upstream.ImageVariant{{URL: "https://img/second", Width: 400, Format: "jpeg"}}},
		{ID: "first", Position: 0, Variants: []upstream.ImageVariant{{URL: "https://img/first", Width: 400, Format: "jpeg"}}},
	}
	if got := shape.PickImage(images, shape.DefaultDevice, shape.ThumbnailWidth); got != "https://img/first" {
		t.Fatalf("picked %s, want the photo at position 0 whatever order they arrive in", got)
	}
}

func TestParseDeviceFallsBackRatherThanFailing(t *testing.T) {
	for _, tc := range []struct{ w, s, accept string }{
		{"", "", ""},
		{"not a number", "neither", ""},
		{"-40", "0", ""},
		{"99999", "99", ""},
	} {
		d := shape.ParseDevice(tc.w, tc.s, tc.accept)
		if d.WidthPoints != shape.DefaultDevice.WidthPoints || d.Scale != shape.DefaultDevice.Scale {
			t.Fatalf("input %q %q produced %v, want the default", tc.w, tc.s, d)
		}
	}

	d := shape.ParseDevice("430", "3", "image/avif,image/webp,*/*")
	if d.WidthPoints != 430 || d.Scale != 3 || !d.AcceptsAVIF || !d.AcceptsWebP {
		t.Fatalf("a good header parsed as %+v", d)
	}
}

func TestDefaultDeviceDoesNotGuessHigh(t *testing.T) {
	// Guessing a tablet for an unknown client sends the largest image to
	// somebody on a train, which is the case this service exists to avoid.
	if shape.DefaultDevice.WidthPoints > 500 || shape.DefaultDevice.Scale > 2 {
		t.Fatalf("the default device is %v, which is not a conservative guess", shape.DefaultDevice)
	}
}

func TestTargetPixels(t *testing.T) {
	d := shape.Device{WidthPoints: 400, Scale: 2}
	if got := d.TargetPixels(shape.FullWidth); got != 800 {
		t.Fatalf("full width on a 400pt 2x screen is %d, want 800", got)
	}
	if got := d.TargetPixels(shape.ThumbnailWidth); got != 368 {
		t.Fatalf("a card on a 400pt 2x screen is %d, want 368", got)
	}
}
