// Package upstream is the internal services this sits in front of, and the
// shape they return.
//
// The shape matters more than the transport here. These structs are what the
// web API already serves: everything about a listing, because a browser can
// afford it and because one endpoint serving every caller is the cheapest
// thing for the team that owns it. A phone cannot afford it, and the whole
// job of this service is the gap between the two.
package upstream

import (
	"context"
	"fmt"
	"time"
)

// ImageVariant is one rendering of one photo.
type ImageVariant struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Bytes  int    `json:"bytes"`
}

// Image is a photo in every size it was rendered into.
type Image struct {
	ID       string         `json:"id"`
	Alt      string         `json:"alt"`
	Position int            `json:"position"`
	Variants []ImageVariant `json:"variants"`
}

// Attribute is one row of the specification table.
type Attribute struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	Group    string `json:"group"`
	Position int    `json:"position"`
}

// Seller is the merchant, as the web profile page needs them.
type Seller struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	LegalName       string  `json:"legal_name"`
	Country         string  `json:"country"`
	Rating          float64 `json:"rating"`
	RatingCount     int     `json:"rating_count"`
	ReturnsPolicy   string  `json:"returns_policy"`
	WarrantyMonths  int     `json:"warranty_months"`
	DispatchDays    int     `json:"dispatch_days"`
	Description     string  `json:"description"`
	VATNumber       string  `json:"vat_number"`
	SupportEmail    string  `json:"support_email"`
	SupportPhone    string  `json:"support_phone"`
	RegisteredSince string  `json:"registered_since"`
}

// Listing is one graded unit. Every one is unique, which is what makes this
// a harder catalogue than a shelf of identical new boxes.
type Listing struct {
	ID               string      `json:"id"`
	SKU              string      `json:"sku"`
	Title            string      `json:"title"`
	Subtitle         string      `json:"subtitle"`
	Description      string      `json:"description"`
	Grade            string      `json:"grade"`
	GradeExplanation string      `json:"grade_explanation"`
	PriceCents       int64       `json:"price_cents"`
	OriginalCents    int64       `json:"original_cents"`
	Currency         string      `json:"currency"`
	InStock          bool        `json:"in_stock"`
	StockCount       int         `json:"stock_count"`
	Images           []Image     `json:"images"`
	Attributes       []Attribute `json:"attributes"`
	Seller           Seller      `json:"seller"`
	CategoryPath     []string    `json:"category_path"`
	BatteryHealthPct int         `json:"battery_health_pct"`
	WarrantyMonths   int         `json:"warranty_months"`
	CO2SavedGrams    int         `json:"co2_saved_grams"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

// Catalogue is the upstream service.
type Catalogue interface {
	Search(ctx context.Context, query string, limit int) ([]Listing, error)
	Get(ctx context.Context, id string) (Listing, error)
}

// ErrNotFound is returned for an id the catalogue does not have.
var ErrNotFound = fmt.Errorf("listing not found")
