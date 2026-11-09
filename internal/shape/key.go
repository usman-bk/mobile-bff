package shape

import (
	"fmt"
	"strings"
)

// Version is bumped whenever a projection in this package changes.
//
// It is part of every cache key. Without it, a change to what a card carries
// serves the old shape to every client until the entries expire, and the bug
// that produces is a field that is missing for exactly as long as the TTL
// and then fixes itself, which is close to undebuggable.
const Version = 3

// Bucket is the class of device a response is cached against.
//
// Caching against the exact device is the obvious thing and it destroys the
// cache: widths run from 320 to 440 points across the phones in use, three
// scale factors, two format flags, and the same page is then stored a hundred
// ways and hit by almost nobody. Caching against one shape for everybody is
// the other extreme and sends a 2000 pixel image to a small phone.
//
// Three buckets, chosen so that the image a bucket resolves to is right for
// every device in it, is the middle. The bucket decides the image, so two
// devices in one bucket genuinely get the same correct bytes.
type Bucket string

const (
	BucketSmall  Bucket = "s" // compact phones, 1x and 2x
	BucketMedium Bucket = "m" // most phones at 2x
	BucketLarge  Bucket = "l" // large phones at 3x, and tablets
)

// BucketOf places a device. The boundaries are on rendered pixels rather than
// points, because pixels are what picks the image.
func BucketOf(d Device) Bucket {
	px := d.TargetPixels(FullWidth)
	switch {
	case px <= 800:
		return BucketSmall
	case px <= 1200:
		return BucketMedium
	default:
		return BucketLarge
	}
}

// Canonical returns the device every member of a bucket is shaped as.
//
// This is what makes the bucket honest. Two devices in a bucket are served
// the same bytes, so the bytes have to be generated for a single device, and
// that device is the most demanding one in the bucket. Shaping for the
// smallest member would send a soft image to the largest.
func (b Bucket) Canonical(accepts Device) Device {
	d := Device{Scale: 1, AcceptsAVIF: accepts.AcceptsAVIF, AcceptsWebP: accepts.AcceptsWebP}
	switch b {
	case BucketSmall:
		d.WidthPoints = 800
	case BucketMedium:
		d.WidthPoints = 1200
	default:
		d.WidthPoints = 1600
	}
	return d
}

// Key is the cache key for one response.
//
// The format flags are in the key because they change the URLs in the body.
// Leaving them out serves AVIF URLs to a client that cannot decode them,
// which shows up as missing images for a fraction of users and nothing in
// the logs.
func Key(screen Screen, bucket Bucket, d Device, parts ...string) string {
	formats := "j"
	switch {
	case d.AcceptsAVIF:
		formats = "a"
	case d.AcceptsWebP:
		formats = "w"
	}
	return fmt.Sprintf("bff:v%d:%s:%s:%s:%s",
		Version, screen, bucket, formats, strings.Join(parts, ":"))
}
