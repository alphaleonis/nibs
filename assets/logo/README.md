# Logo artwork

Source exports from Affinity Designer. The mark is a nib inside an orbiting
ring; the wordmark is "NIBS".

| file | contents | aspect |
| --- | --- | --- |
| `logo-only.svg` | mark alone | 1.03:1 |
| `banner-dark-text.svg` | mark + wordmark, side by side, gray-gradient wordmark | 2.16:1 |
| `banner-white-text.svg` | same, flat white wordmark | 2.16:1 |
| `logo-and-dark-text.svg` | mark above wordmark, gray-gradient wordmark | 0.79:1 |
| `logo-and-white-text.svg` | same, flat white wordmark | 0.79:1 |

## Two things to know before re-exporting

**1. The artboard crops the artwork.** Every file as originally exported had a
viewBox tighter than its own geometry, slicing the left of the orange ring and
the top of the gray swoosh flat. The paths were all present — only the viewBox
was wrong — so the files here were patched in place to the measured content
bounding box:

| file | exported viewBox | corrected |
| --- | --- | --- |
| `banner-*.svg` | `0 0 2789 984` | `-167 -203 2956 1371` |
| `logo-and-*.svg` | `0 0 1571 2325` | `-243 -295 2061 2620` |
| `logo-only.svg` | `0 0 1571 1433` | `-243 -295 2061 1996` |

A fresh export will reintroduce the crop unless the Affinity artboard is
widened to contain the whole drawing. After any re-export, check that no
geometry falls outside the viewBox before committing.

The intrinsic `width`/`height` were likewise changed from Affinity's
`100%`/`100%`, which gives an `<img>` no aspect ratio to work from, to the
viewBox dimensions in px.

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
inherit each theme's foreground. Its test pins the corrected viewBox.

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
