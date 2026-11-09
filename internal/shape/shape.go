// Package shape turns what the internal services return into what a screen
// needs, and refuses to go over a size budget doing it.
package shape

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/usmanvirk/mobile-bff/internal/upstream"
)

// Screen is a view in the app. The projection is chosen by screen and not by
// the client.
//
// Letting a client ask for arbitrary fields is the obvious alternative and it
// is the wrong trade here. Every field any client ever asked for becomes
// permanently load bearing, because the server cannot tell which app version
// still asks for it; the responses stop being cacheable, because every
// caller has its own shape; and the size budget below becomes unenforceable,
// because there is no longer a fixed shape to measure. A fixed projection per
// screen is a contract small enough to reason about and to cache.
type Screen string

const (
	// ScreenList is the search and browse grid: a card the customer scrolls
	// past. It needs enough to decide whether to tap.
	ScreenList Screen = "list"

	// ScreenDetail is one listing, opened deliberately.
	ScreenDetail Screen = "detail"
)

func ParseScreen(s string) (Screen, error) {
	switch Screen(s) {
	case ScreenList:
		return ScreenList, nil
	case ScreenDetail:
		return ScreenDetail, nil
	case "":
		return "", fmt.Errorf("no screen given")
	default:
		return "", fmt.Errorf("unknown screen %q", s)
	}
}

// Card is one listing on the list screen.
//
// Short JSON names on purpose. A list response repeats its keys once per
// item, so a key is paid for twenty or fifty times rather than once, and
// "price_cents" against "p" is nine bytes an item before any value. It reads
// worse and it is the part of the payload that scales with the number of
// results.
type Card struct {
	ID       string `json:"id"`
	Title    string `json:"t"`
	Grade    string `json:"g"`
	Price    int64  `json:"p"`
	Original int64  `json:"o,omitempty"`
	Currency string `json:"c"`
	Image    string `json:"i"`
	InStock  bool   `json:"s"`
	Battery  int    `json:"b,omitempty"`
}

// Detail is one listing on its own screen. Long names here, because they are
// paid for once.
type Detail struct {
	ID               string      `json:"id"`
	Title            string      `json:"title"`
	Subtitle         string      `json:"subtitle,omitempty"`
	Description      string      `json:"description"`
	Truncated        bool        `json:"description_truncated,omitempty"`
	Grade            string      `json:"grade"`
	GradeExplanation string      `json:"grade_explanation,omitempty"`
	PriceCents       int64       `json:"price_cents"`
	OriginalCents    int64       `json:"original_cents,omitempty"`
	Currency         string      `json:"currency"`
	InStock          bool        `json:"in_stock"`
	Images           []string    `json:"images"`
	Attributes       []KV        `json:"attributes"`
	Seller           SellerBrief `json:"seller"`
	BatteryHealthPct int         `json:"battery_health_pct,omitempty"`
	WarrantyMonths   int         `json:"warranty_months,omitempty"`
}

type KV struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SellerBrief is the part of a seller a buyer looks at before buying. The VAT
// number, the legal name and the support phone belong on the seller's own
// page, and putting them here means every product view carries them.
type SellerBrief struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Rating      float64 `json:"rating"`
	RatingCount int     `json:"rating_count"`
	Country     string  `json:"country,omitempty"`
}

// Limits on the detail screen. A description is written by a seller and is
// occasionally thousands of words; a phone renders the first paragraph and a
// "read more". Cutting it here is visible to the client through Truncated,
// rather than silently.
const (
	MaxDescription = 600
	MaxAttributes  = 12
)

// Cards projects a page of search results.
func Cards(in []upstream.Listing, dev Device) []Card {
	out := make([]Card, 0, len(in))
	for _, l := range in {
		out = append(out, Card{
			ID:       l.ID,
			Title:    l.Title,
			Grade:    l.Grade,
			Price:    l.PriceCents,
			Original: l.OriginalCents,
			Currency: l.Currency,
			Image:    PickImage(l.Images, dev, ThumbnailWidth),
			InStock:  l.InStock,
			Battery:  l.BatteryHealthPct,
		})
	}
	return out
}

// Detail projects one listing.
func ToDetail(l upstream.Listing, dev Device) Detail {
	d := Detail{
		ID:               l.ID,
		Title:            l.Title,
		Subtitle:         l.Subtitle,
		Grade:            l.Grade,
		GradeExplanation: l.GradeExplanation,
		PriceCents:       l.PriceCents,
		OriginalCents:    l.OriginalCents,
		Currency:         l.Currency,
		InStock:          l.InStock,
		BatteryHealthPct: l.BatteryHealthPct,
		WarrantyMonths:   l.WarrantyMonths,
		Seller: SellerBrief{
			ID: l.Seller.ID, Name: l.Seller.Name,
			Rating: l.Seller.Rating, RatingCount: l.Seller.RatingCount,
			Country: l.Seller.Country,
		},
	}

	d.Description, d.Truncated = truncate(l.Description, MaxDescription)

	// One URL per photo, already chosen for this device, rather than every
	// variant with the choice left to the client. Five variants is five URLs
	// of which four are never fetched, and the client cannot choose better
	// than the server can: the server knows what it rendered.
	for _, img := range l.Images {
		if u := PickImage([]upstream.Image{img}, dev, FullWidth); u != "" {
			d.Images = append(d.Images, u)
		}
	}

	attrs := l.Attributes
	if len(attrs) > MaxAttributes {
		attrs = attrs[:MaxAttributes]
	}
	for _, a := range attrs {
		d.Attributes = append(d.Attributes, KV{Label: a.Label, Value: a.Value})
	}
	return d
}

// truncate cuts at a word boundary where it can, so the text does not end
// mid-word, and reports whether anything was removed.
func truncate(s string, max int) (string, bool) {
	if utf8.RuneCountInString(s) <= max {
		return s, false
	}
	runes := []rune(s)
	cut := string(runes[:max])
	if i := strings.LastIndexAny(cut, " \n\t"); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \n\t,;:"), true
}
