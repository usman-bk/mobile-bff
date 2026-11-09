package upstream

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Fake is an in process Catalogue that returns records the size the real ones
// are.
//
// The sizes are the point. A fixture with a 20 character description and one
// image makes any budget look comfortable and proves nothing. These records
// carry a seller paragraph, a few hundred words of description, twenty
// attributes and five images in five variants each, because that is what the
// web endpoint actually returns and the gap between that and what a phone
// needs is the entire reason this service exists.
type Fake struct {
	mu    sync.Mutex
	items []Listing
	// Fail, when set, is returned by every call.
	Fail error
	// Delay is added to every call, for the timeout tests.
	Delay time.Duration
	calls int
}

func NewFake(n int) *Fake {
	return &Fake{items: Generate(n, 1)}
}

func (f *Fake) Calls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func (f *Fake) Search(ctx context.Context, query string, limit int) ([]Listing, error) {
	f.mu.Lock()
	f.calls++
	fail, delay, items := f.Fail, f.Delay, f.items
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if fail != nil {
		return nil, fail
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit > len(items) {
		limit = len(items)
	}
	return items[:limit], nil
}

func (f *Fake) Get(ctx context.Context, id string) (Listing, error) {
	f.mu.Lock()
	f.calls++
	fail, delay, items := f.Fail, f.Delay, f.items
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return Listing{}, ctx.Err()
		}
	}
	if fail != nil {
		return Listing{}, fail
	}
	for _, l := range items {
		if l.ID == id {
			return l, nil
		}
	}
	return Listing{}, ErrNotFound
}

var (
	models  = []string{"iPhone 13 Pro", "iPhone 12", "Galaxy S22 Ultra", "Pixel 7 Pro", "MacBook Air M1", "iPad Air", "Galaxy Tab S8"}
	colours = []string{"Graphite", "Sierra Blue", "Midnight Green", "Phantom Black", "Silver", "Starlight"}
	grades  = []string{"excellent", "good", "fair"}
	storage = []string{"128 GB", "256 GB", "512 GB", "1 TB"}
)

// Generate builds n listings deterministically from a seed, so a size
// assertion in a test is stable between runs.
func Generate(n int, seed int64) []Listing {
	rng := rand.New(rand.NewSource(seed))
	out := make([]Listing, 0, n)
	for i := 0; i < n; i++ {
		model := models[rng.Intn(len(models))]
		colour := colours[rng.Intn(len(colours))]
		cap := storage[rng.Intn(len(storage))]
		grade := grades[rng.Intn(len(grades))]
		price := int64(18000 + rng.Intn(90000))

		l := Listing{
			ID:               fmt.Sprintf("lst_%09d", 100000+i),
			SKU:              fmt.Sprintf("REF-%s-%s-%d", strings.ToUpper(strings.ReplaceAll(model, " ", "")), strings.ToUpper(colour[:3]), i),
			Title:            fmt.Sprintf("%s %s %s", model, cap, colour),
			Subtitle:         fmt.Sprintf("Refurbished, %s condition, 12 month warranty", grade),
			Description:      description(model, grade, rng),
			Grade:            grade,
			GradeExplanation: gradeText(grade),
			PriceCents:       price,
			OriginalCents:    price + int64(10000+rng.Intn(40000)),
			Currency:         "EUR",
			InStock:          rng.Intn(10) > 0,
			StockCount:       rng.Intn(3) + 1,
			Images:           imagesFor(i, 5),
			Attributes:       attributes(model, cap, colour, grade),
			Seller:           seller(rng),
			CategoryPath:     []string{"Electronics", "Phones and Tablets", "Smartphones", model},
			BatteryHealthPct: 80 + rng.Intn(21),
			WarrantyMonths:   12,
			CO2SavedGrams:    40000 + rng.Intn(30000),
			UpdatedAt:        time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour),
		}
		out = append(out, l)
	}
	return out
}

func description(model, grade string, rng *rand.Rand) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This %s has been fully tested and refurbished by a certified partner. ", model)
	b.WriteString("Every unit passes a 40 point inspection covering the screen, the battery, the cameras, the speakers, the buttons and every port. ")
	fmt.Fprintf(&b, "Cosmetically it is graded %s, which describes the casing and the screen rather than how it works: the device is fully functional whatever its grade. ", grade)
	b.WriteString("The battery has been checked and replaced where it fell below the threshold, and the charge cycle count is recorded against the serial number. ")
	b.WriteString("It arrives unlocked and reset to factory settings, with a charging cable. A mains adapter is not included, which is deliberate: most people already own several and the ones that end up unused are a large part of the waste this is meant to avoid. ")
	b.WriteString("Covered by a 12 month warranty and 30 days to change your mind. ")
	b.WriteString("Buying refurbished rather than new avoids most of the emissions of manufacturing a device, which is where the great majority of a phone's lifetime footprint sits.")
	return b.String()
}

func gradeText(grade string) string {
	switch grade {
	case "excellent":
		return "No visible marks at normal viewing distance. The screen is free of scratches."
	case "good":
		return "Light signs of use on the casing. Any marks on the screen are not visible when it is on."
	default:
		return "Visible marks on the casing and possibly the screen. Fully functional and priced accordingly."
	}
}

func imagesFor(i, n int) []Image {
	out := make([]Image, 0, n)
	for p := 0; p < n; p++ {
		id := fmt.Sprintf("img_%06d_%d", i, p)
		var vs []ImageVariant
		for _, w := range []int{180, 360, 720, 1080, 2000} {
			for _, f := range []string{"avif", "webp", "jpeg"} {
				vs = append(vs, ImageVariant{
					URL:    fmt.Sprintf("https://images.example.com/%s/%d.%s", id, w, f),
					Width:  w,
					Height: w * 4 / 3,
					Format: f,
					Bytes:  w * w / 40,
				})
			}
		}
		out = append(out, Image{
			ID:       id,
			Alt:      fmt.Sprintf("Photograph %d of the device, front and back", p+1),
			Position: p,
			Variants: vs,
		})
	}
	return out
}

func attributes(model, cap, colour, grade string) []Attribute {
	raw := [][3]string{
		{"model", "Model", model},
		{"storage", "Storage", cap},
		{"colour", "Colour", colour},
		{"grade", "Cosmetic grade", grade},
		{"network", "Network", "Unlocked"},
		{"screen_size", "Screen size", "6.1 inches"},
		{"screen_tech", "Screen", "Super Retina XDR OLED"},
		{"chipset", "Chipset", "A15 Bionic"},
		{"ram", "Memory", "6 GB"},
		{"rear_camera", "Rear camera", "12 MP wide, 12 MP ultra wide, 12 MP telephoto"},
		{"front_camera", "Front camera", "12 MP TrueDepth"},
		{"battery", "Battery", "3095 mAh"},
		{"charging", "Charging", "Lightning, MagSafe, Qi"},
		{"water", "Water resistance", "IP68"},
		{"sim", "SIM", "Nano SIM and eSIM"},
		{"os", "Operating system", "iOS, upgradable"},
		{"weight", "Weight", "204 g"},
		{"dimensions", "Dimensions", "146.7 x 71.5 x 7.65 mm"},
		{"warranty", "Warranty", "12 months"},
		{"returns", "Returns", "30 days"},
	}
	out := make([]Attribute, 0, len(raw))
	for i, r := range raw {
		group := "Technical"
		if i < 4 {
			group = "Condition"
		}
		out = append(out, Attribute{Key: r[0], Label: r[1], Value: r[2], Group: group, Position: i})
	}
	return out
}

func seller(rng *rand.Rand) Seller {
	names := []string{"Renewed Electronics GmbH", "CircuitSecond BV", "ReTech Iberia SL", "Nordic Refurb AB"}
	n := names[rng.Intn(len(names))]
	return Seller{
		ID:          fmt.Sprintf("sel_%05d", rng.Intn(5000)),
		Name:        n,
		LegalName:   n + " (Handelsregister B 118293)",
		Country:     []string{"DE", "NL", "ES", "SE"}[rng.Intn(4)],
		Rating:      4.0 + rng.Float64(),
		RatingCount: 200 + rng.Intn(20000),
		ReturnsPolicy: "Returns accepted within 30 days of delivery in the original packaging. " +
			"Refunds are issued within 14 days of the device arriving back with us and passing inspection. " +
			"Return postage is covered where the device is faulty or not as described.",
		WarrantyMonths: 12,
		DispatchDays:   1 + rng.Intn(3),
		Description: "A certified refurbishment partner operating since 2014, processing several thousand devices a month " +
			"across phones, tablets and laptops. Every device is inspected, tested, cleaned and data wiped to a documented standard " +
			"before it is listed, and each one carries a serial level record of what was replaced.",
		VATNumber:       fmt.Sprintf("DE%09d", rng.Intn(999999999)),
		SupportEmail:    "support@example.com",
		SupportPhone:    "+49 30 123456789",
		RegisteredSince: "2014-03-11",
	}
}
