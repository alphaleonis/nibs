# Logo artwork

Source exports from Affinity Designer. The mark is a nib inside an orbiting
ring; the wordmark is "NIBS".

| file | contents | aspect |
| --- | --- | --- |
| `logo-only.svg` | mark alone | 1.10:1 |
| `banner-dark-text.svg` | mark + wordmark, side by side, gray-gradient wordmark | 2.83:1 |
| `banner-white-text.svg` | same, flat white wordmark | 2.83:1 |
| `logo-and-dark-text.svg` | mark above wordmark, gray-gradient wordmark | 0.68:1 |
| `logo-and-white-text.svg` | same, flat white wordmark | 0.68:1 |

The artboards are correct: each viewBox is tight to the rendered artwork, which
touches all four edges. The only change made to the exports is the intrinsic
`width`/`height`, from Affinity's `100%`/`100%` — which gives an `<img>` no
aspect ratio to work from — to the viewBox dimensions in px.

## Two things to know before changing these

**1. Never derive a viewBox from `getBBox()`.** SVG bounding boxes here extend
well past anything the path actually draws: the ring path in the banner reports
a bbox starting 165 units left of its leftmost ink, and ~200 units above and
below the visible artwork. Padding a viewBox out to that bbox adds dead margin —
asymmetrically, since the overhang is almost entirely on the left and top — and
the result is artwork that renders visibly off-centre wherever it is centred,
and smaller than expected wherever it is sized by height.

This already happened once: all five files were "corrected" to their bboxes,
which added 166/202/0/184 units of dead margin to the banner and pushed it 3.2%
right of centre in the README, while shrinking the mark in the app header by
28%. The exports were right; the measurement was wrong.

**Measure ink, not geometry.** Rasterize the file and scan the alpha channel for
the true bounds. `getBBox()` is not a substitute, and neither is eyeballing a
render — artwork that *touches* a viewBox edge looks identical to artwork that
is *clipped* by it, and that is exactly how the mistake above was made.

**2. Neither wordmark color works on every theme.** Measured against the four
app backgrounds:

| wordmark | graphite | midnight | dracula | daylight |
| --- | --- | --- | --- | --- |
| gray gradient, light end `#919191` | 5.32 | 6.28 | 4.62 | 3.02 |
| gray gradient, dark end `#4D4D4D` | **1.98** | **2.34** | **1.72** | 8.10 |
| flat white `#FFFFFF` | 16.76 | 19.80 | 14.57 | **1.04** |

The gray gradient spans only `#919191` to `#4D4D4D` — no light end — so the
bottom of every letter sinks into the three dark backgrounds. Flat white is
invisible on Daylight at 1.04:1.

The mark does not have this problem: its gradients run `#4D4D4D` through
`#D0D0D0` and back, so part of it always has contrast whatever is behind it.

## Where these are used

The project `README.md` is the one place that loads these files directly. It
uses **both** banner variants behind a `<picture>`, because no single wordmark
color works on both of GitHub's backdrops — white is 1.00:1 on the light theme,
and the gray-gradient wordmark is 2.24:1 on the dark one. There is no gray that
clears 4.5:1 against both `#ffffff` and `#0d1117` at all; only the 3:1
large-text threshold admits one (roughly `#7a7a7a`), and that reads dull. GitHub
markdown allows no CSS, so the `currentColor` trick used in the app is not
available there.

Everything else uses a derived asset, and a geometry change here has to be
mirrored into both:

**`web/src/lib/components/NibsLogo.svelte`** — the header banner, derived from
`banner-white-text.svg`. Inline SVG so the wordmark can be `currentColor` and
inherit each theme's foreground. Its test pins the viewBox, which must stay
equal to this file's.

**`web/public/favicon.svg`** — derived from `logo-only.svg` with the two
orbiting-ring paths (the ones carrying the `0.517282` transform) removed, on a
square canvas with a 4% margin.

The ring is dropped because it does not survive small sizes: it is a thin arc
around mostly empty space, so at 16px it rasterizes to an orange smear that
swallows the checkmark. The nib and blade alone stay legible from 16px up. That
is a favicon-specific simplification, not a change to the mark — everywhere the
logo is shown larger, the ring stays.

`web/public/favicon.ico` (16/32/48) is generated from `favicon.svg` by
`task favicon` and committed. It is not built, because rasterizing needs a
browser and the normal build must not depend on one. Re-run `task favicon`
after editing `favicon.svg`.
