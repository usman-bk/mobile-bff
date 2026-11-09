# mobile-bff

_Built with AI assistance._

The endpoints a native app calls, instead of the web API reshaped on the
phone.

A page of twenty search results from the web endpoint measures **270 KB**. The
same page as cards measures **3.4 KB**. The difference is not compression or
cleverness, it is that the web response carries fifteen image variants, the
full seller record and the entire description for each of twenty items, none
of which a card shows.

Go 1.23, Redis, no other runtime dependency.

```
internal/shape/budget.go   the size limit, and the test that enforces it
internal/shape/shape.go    what each screen carries
internal/shape/image.go    choosing one variant for this device
internal/shape/key.go      device buckets, and the cache key
internal/cache/            Redis, and the same contract in process
internal/bff/handler.go    two endpoints
internal/upstream/fake.go  records the size the real ones are
cmd/bffd/
```

## The budget is a test, not an intention

    ScreenList:   12 KB a page, 600 bytes an item
    ScreenDetail: 6 KB

A budget set at today's size fails on the next legitimate field and gets
raised until nobody reads it. A budget set far above it never fails at all,
which is the more common mistake and the worse one, because it looks like a
control and is decoration. 600 bytes an item is about three times what a card
costs now, so a field or two is free and putting the seller record back on the
card is not.

`TestTheBudgetCatchesARealRegression` performs the change somebody would
actually propose: the designer wants the seller name and the first line of the
description on the card. Each looks like nothing. Together they take the page
over, and the test fails.

Measured uncompressed, deliberately. Measuring after gzip hides a field that
compresses well and still costs the phone the time to decode it.

In production an over budget response is a log line and not a refused request,
because a customer whose page is 2 KB over should see their page. In the build
it is fatal, because that is where somebody can still do something about it.

## One image, chosen here

The upstream record carries five photos in five widths and three formats, so
fifteen URLs per photo. The app displays one.

The rule is the smallest variant at least as wide as the slot needs, falling
back to the widest available, with AVIF preferred over WebP over JPEG when the
client accepts them. Format is preferred before size because AVIF at the same
width is routinely a third of the bytes, which beats one step of width. But
coverage beats format: a correctly sized JPEG is better than an AVIF that is
too small, because a soft image is visible and an oversized one is not.

The client sends its viewport in points and a scale factor, which is what a
phone actually knows. An unknown client gets a mid sized phone at 2x rather
than a tablet, because guessing high sends the largest image to somebody on a
train, which is the case this exists to avoid.

## Caching per device would destroy the cache

Widths run from 320 to 440 points across the phones in use, three scale
factors, two format flags. Keyed on the exact device, the same page is stored
a hundred ways and almost nothing is ever hit twice. Keyed on nothing, a small
phone gets the image chosen for a tablet.

Three buckets, and the bucket decides the image, so two devices in one bucket
genuinely receive the same correct bytes. A response is shaped for the most
demanding device in its bucket, because shaping for the smallest would send a
soft image to the largest.

    bff:v3:list:m:a:iphone:20
        │  │    │ │ └── query and limit
        │  │    │ └──── image formats the client accepts
        │  │    └────── device bucket
        │  └─────────── screen
        └────────────── shape version

The version is bumped whenever a projection changes. Without it, a change to
what a card carries serves the old shape until the entries expire, and the bug
that produces is a field that is missing for exactly as long as the TTL and
then fixes itself.

The format flags are in the key because they change the URLs in the body.
Leaving them out serves AVIF URLs to a client that cannot decode them, which
shows up as missing images for a fraction of users and nothing in the logs.

## A cache outage is not an outage

Every cache error is logged and treated as a miss. Redis being down makes the
app slow, not broken. `TestACacheOutageIsNotAnOutage` runs the handler against
a cache that fails every call and asserts a usable response.

The Redis timeouts are 150 ms, because a cache that takes half a second to
answer is worse than no cache: the request pays the wait and then does the
work anyway.

## Screens, not field masks

The projection is chosen by screen and not by the client.

Letting a client ask for arbitrary fields is the obvious alternative. It makes
every field any client ever asked for permanently load bearing, because the
server cannot tell which app version still asks for it. It makes responses
uncacheable, because every caller has its own shape. And it makes the size
budget unenforceable, because there is no longer a fixed shape to measure.

The cost is that a new screen needs a server change. That is the trade, and
for an API whose clients cannot be forced to update it is the right way round.

## Errors the app can act on

| | |
|---|---|
| 502 | the catalogue failed |
| 504 | the catalogue did not answer in 2 seconds |
| 404 | no such listing |
| 400 | a limit that is not a positive number |

A timeout is distinguished from a failure because they are different problems
and only one of them is worth an immediate retry. A `limit` above the maximum
is clamped rather than refused: the client gets 50 and a working screen
instead of an error it has no handling for.

## Running it

    REDIS_URL=redis://localhost:6379 go run ./cmd/bffd

    curl -s 'localhost:8080/v1/list?q=iphone&limit=3' \
      -H 'X-Viewport-Width: 320' -H 'X-Viewport-Scale: 1'

    [{"id":"lst_000100000","t":"Galaxy Tab S8 1 TB Phantom Black","g":"fair",
      "p":40081,"o":71399,"c":"EUR",
      "i":"https://images.example.com/img_000000_0/720.jpeg","s":true,"b":91}]

The same request from a large phone that accepts AVIF:

    -H 'X-Viewport-Width: 430' -H 'X-Viewport-Scale: 3' -H 'Accept: image/avif'

    "i":"https://images.example.com/img_000000_0/1080.avif"

The keys are one letter on the list screen and words on the detail screen.
A list repeats its keys once per item, so `p` against `price_cents` is nine
bytes an item before any value, and the detail screen pays for its keys once.

## Tests

    go test ./...                      55 cases, all in process
    REDIS_URL=... go test ./...        66, adding the Redis suite

The in process cache is held to the same conformance suite as Redis, in
`internal/cache/cachetest`, including the two cases an in process cache gets
wrong: an empty value has to be a hit and not a miss, or an empty search
result can never be cached, and a caller that writes to the slice it was given
must not corrupt the entry for everybody behind it.

The fixtures carry a seller paragraph, several hundred words of description,
twenty attributes and five images in fifteen variants each, because a fixture
with a short description makes any budget look comfortable and proves nothing.

Deleting the bucket and the format flags from the cache key was tried, to
check the tests notice. Two fail.

## What this does not do

No authentication, no personalisation, and therefore no per user caching. A
logged in price or a saved list would change the key and most of the hit rate
with it.

No pagination beyond a limit. There is no page token, so this is the first
screen and not the fifth.

The upstream here is generated rather than a real gRPC client. The interface
is the boundary, and swapping in the real one changes nothing above it.
