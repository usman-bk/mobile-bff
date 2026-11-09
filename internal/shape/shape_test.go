package shape_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/usmanvirk/mobile-bff/internal/shape"
	"github.com/usmanvirk/mobile-bff/internal/upstream"
)

func encode(t *testing.T, v any) int {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

const page = 20

// TestListPageFitsItsBudget is the test the whole service is built around.
// Without it the payload grows one harmless field at a time and nobody is
// ever the person who made it slow.
func TestListPageFitsItsBudget(t *testing.T) {
	in := upstream.Generate(page, 1)
	cards := shape.Cards(in, shape.DefaultDevice)
	size := encode(t, cards)

	if err := shape.Check(shape.ScreenList, size, len(cards)); err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("list page of %d is %d bytes, %d an item", page, size, size/page)
}

// TestShapingIsWorthDoing measures the thing the service claims. The upstream
// record is what the web endpoint returns, and a test that did not compare
// against it would let the saving quietly disappear.
func TestShapingIsWorthDoing(t *testing.T) {
	in := upstream.Generate(page, 1)

	raw := encode(t, in)
	shaped := encode(t, shape.Cards(in, shape.DefaultDevice))

	if shaped >= raw {
		t.Fatalf("shaping made it bigger: %d to %d", raw, shaped)
	}
	// The claim is roughly 310 KB down to under 90 KB, so about a quarter.
	// Asserting a ratio rather than an exact size, because the fixture can
	// legitimately change and the point is the order of magnitude.
	if shaped*3 > raw {
		t.Fatalf("shaping only got %d bytes down to %d, which is less than a third saved", raw, shaped)
	}
	t.Logf("web shape %d bytes, card shape %d bytes, %.1f times smaller",
		raw, shaped, float64(raw)/float64(shaped))
}

func TestDetailFitsItsBudget(t *testing.T) {
	l := upstream.Generate(1, 7)[0]
	d := shape.ToDetail(l, shape.DefaultDevice)
	size := encode(t, d)
	if err := shape.Check(shape.ScreenDetail, size, 1); err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("detail is %d bytes", size)
}

// TestBudgetActuallyRefuses. A budget that cannot fail is decoration.
func TestBudgetActuallyRefuses(t *testing.T) {
	if err := shape.Check(shape.ScreenList, 400*1024, page); err == nil {
		t.Fatal("a 400 KB list page passed the budget")
	}
	var over shape.ErrOverBudget
	err := shape.Check(shape.ScreenDetail, 1<<20, 1)
	if err == nil {
		t.Fatal("a 1 MB detail response passed the budget")
	}
	if !asOverBudget(err, &over) {
		t.Fatalf("the error is %T, which a caller cannot act on", err)
	}
	if over.Allowed == 0 || over.Size == 0 {
		t.Fatalf("the error does not say what was allowed or what arrived: %+v", over)
	}
}

func asOverBudget(err error, target *shape.ErrOverBudget) bool {
	e, ok := err.(shape.ErrOverBudget)
	if ok {
		*target = e
	}
	return ok
}

// TestSmallPageGetsASmallerBudget. Twenty items at 4.5 KB is the ceiling, so
// a page of three must not be allowed 90 KB.
func TestSmallPageGetsASmallerBudget(t *testing.T) {
	b, ok := shape.BudgetFor(shape.ScreenList)
	if !ok {
		t.Fatal("no list budget")
	}
	if b.Allowed(3) >= b.Allowed(20) {
		t.Fatalf("a page of 3 is allowed %d and a page of 20 is allowed %d", b.Allowed(3), b.Allowed(20))
	}
	if err := shape.Check(shape.ScreenList, 50*1024, 3); err == nil {
		t.Fatal("50 KB for three cards passed")
	}
}

// TestTheBudgetCatchesARealRegression performs the change somebody actually
// makes: the designer wants the seller name and the first line of the
// description on the card, and each one looks like nothing on its own.
//
// A budget is only worth having if it fails on the change that would really
// be proposed, rather than on an absurd one. This is that change.
func TestTheBudgetCatchesARealRegression(t *testing.T) {
	in := upstream.Generate(page, 1)

	type fatCard struct {
		shape.Card
		Seller      string `json:"seller"`
		Description string `json:"desc"`
	}
	cards := shape.Cards(in, shape.DefaultDevice)
	fat := make([]fatCard, 0, len(cards))
	for i, c := range cards {
		fat = append(fat, fatCard{Card: c, Seller: in[i].Seller.Name, Description: in[i].Description})
	}

	size := encode(t, fat)
	if err := shape.Check(shape.ScreenList, size, len(fat)); err == nil {
		t.Fatalf("putting the seller and the description back on the card took the page to %d bytes and the budget did not notice", size)
	}
}

func TestUnknownScreenHasNoBudget(t *testing.T) {
	if err := shape.Check(shape.Screen("invented"), 10, 1); err == nil {
		t.Fatal("a screen with no budget passed, so a new screen would ship unmeasured")
	}
}

func TestCardCarriesWhatACardNeeds(t *testing.T) {
	in := upstream.Generate(1, 3)
	c := shape.Cards(in, shape.DefaultDevice)[0]
	switch {
	case c.ID == "":
		t.Error("no id, so the card cannot be tapped")
	case c.Title == "":
		t.Error("no title")
	case c.Price == 0:
		t.Error("no price")
	case c.Currency == "":
		t.Error("no currency, so the price is a bare number")
	case c.Image == "":
		t.Error("no image")
	case c.Grade == "":
		t.Error("no grade, which is the thing that distinguishes two otherwise identical units")
	}
}

// TestCardDoesNotCarryTheSeller is the whole saving, stated as a rule. The
// seller record is about 700 bytes and nothing on a card shows it.
func TestCardDoesNotCarryTheSeller(t *testing.T) {
	in := upstream.Generate(1, 3)
	b, err := json.Marshal(shape.Cards(in, shape.DefaultDevice))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, leaked := range []string{
		in[0].Seller.Name, in[0].Seller.VATNumber, in[0].Seller.Description,
		in[0].Description, in[0].Seller.ReturnsPolicy,
	} {
		if leaked != "" && strings.Contains(s, leaked) {
			t.Errorf("the card carries something only the detail screen shows: %.40s", leaked)
		}
	}
}

func TestDescriptionIsTruncatedAndSaysSo(t *testing.T) {
	l := upstream.Generate(1, 5)[0]
	d := shape.ToDetail(l, shape.DefaultDevice)

	if len([]rune(d.Description)) > shape.MaxDescription {
		t.Fatalf("description is %d runes, over the %d limit", len([]rune(d.Description)), shape.MaxDescription)
	}
	if len([]rune(l.Description)) > shape.MaxDescription && !d.Truncated {
		t.Fatal("the description was cut and the response does not say so, so the app cannot offer a read more")
	}
	if strings.HasSuffix(d.Description, " ") {
		t.Fatal("the truncated description ends in a space")
	}
}

func TestShortDescriptionIsNotMarkedTruncated(t *testing.T) {
	l := upstream.Generate(1, 5)[0]
	l.Description = "Short."
	d := shape.ToDetail(l, shape.DefaultDevice)
	if d.Truncated {
		t.Fatal("a short description was reported as truncated")
	}
	if d.Description != "Short." {
		t.Fatalf("a short description was altered: %q", d.Description)
	}
}

func TestDetailSendsOneURLPerImage(t *testing.T) {
	l := upstream.Generate(1, 9)[0]
	d := shape.ToDetail(l, shape.DefaultDevice)
	if len(d.Images) != len(l.Images) {
		t.Fatalf("%d images out for %d in", len(d.Images), len(l.Images))
	}
	// Each upstream image has 15 variants. Sending them all is 14 URLs per
	// photo that will never be fetched.
	for _, u := range d.Images {
		if strings.Count(u, "http") != 1 {
			t.Fatalf("an image field carries more than one URL: %s", u)
		}
	}
}

func TestParseScreen(t *testing.T) {
	for _, ok := range []string{"list", "detail"} {
		if _, err := shape.ParseScreen(ok); err != nil {
			t.Errorf("%s was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "List", "search", "everything"} {
		if _, err := shape.ParseScreen(bad); err == nil {
			t.Errorf("%q was accepted as a screen", bad)
		}
	}
}

func TestEmptyPageIsNotNull(t *testing.T) {
	b, err := json.Marshal(shape.Cards(nil, shape.DefaultDevice))
	if err != nil {
		t.Fatal(err)
	}
	// null here makes a client written in almost any language fall over on
	// the empty search, which is a result people get every day.
	if string(b) != "[]" {
		t.Fatalf("an empty page encodes as %s, want []", b)
	}
}
