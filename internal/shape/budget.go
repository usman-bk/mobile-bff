package shape

import "fmt"

// Budget is the most a screen's response may weigh, uncompressed.
//
// These are not aspirations. There is a test that builds a realistic page and
// fails if the encoded response goes over, which is the only reason a number
// like this survives contact with six months of feature requests. Without it
// the payload grows one harmless field at a time and nobody is ever the
// person who made it slow.
//
// Uncompressed, deliberately. Measuring after gzip hides a field that
// compresses well and still costs the phone the time to decode it, and the
// transfer saving from compression is not something this service controls.
type Budget struct {
	Screen  Screen
	Bytes   int
	PerItem int // the list screen only, so the limit scales with page size
}

// The numbers, and how they were chosen.
//
// A page of twenty from the web endpoint measures 270 KB, because it carries
// fifteen image variants, the full seller record and the whole description
// for each of twenty items. The same page as cards measures 3.4 KB.
//
// The budget is 12 KB for twenty, not 270 and not 3.4. A budget set at the
// current size fails on the next legitimate field and gets raised until
// nobody reads it. A budget set far above it never fails at all, which is the
// more common mistake and the worse one: it looks like a control and is
// decoration. 600 bytes an item is roughly three times what a card costs
// today, so a field or two is free, and putting the seller record or the
// description back on the card is not. That second case is the regression
// this is here to catch, and there is a test that performs it.
var budgets = map[Screen]Budget{
	ScreenList:   {Screen: ScreenList, Bytes: 12 * 1024, PerItem: 600},
	ScreenDetail: {Screen: ScreenDetail, Bytes: 6 * 1024},
}

func BudgetFor(s Screen) (Budget, bool) {
	b, ok := budgets[s]
	return b, ok
}

// Allowed is the ceiling for a response of this many items.
func (b Budget) Allowed(items int) int {
	if b.PerItem == 0 {
		return b.Bytes
	}
	byItem := b.PerItem * items
	if byItem < b.Bytes {
		return byItem
	}
	return b.Bytes
}

// ErrOverBudget is what a response too large for its screen produces.
type ErrOverBudget struct {
	Screen  Screen
	Size    int
	Allowed int
	Items   int
}

func (e ErrOverBudget) Error() string {
	return fmt.Sprintf("%s response is %d bytes for %d items, over the %d byte budget",
		e.Screen, e.Size, e.Items, e.Allowed)
}

// Check reports whether an encoded response fits.
//
// In production this is a log line and not a refused request: a customer
// whose page is 2 KB over should see their page. The build is where it is
// fatal, through the test, because that is where somebody can still do
// something about it.
func Check(s Screen, size, items int) error {
	b, ok := budgets[s]
	if !ok {
		return fmt.Errorf("no budget defined for screen %q", s)
	}
	allowed := b.Allowed(items)
	if size > allowed {
		return ErrOverBudget{Screen: s, Size: size, Allowed: allowed, Items: items}
	}
	return nil
}
