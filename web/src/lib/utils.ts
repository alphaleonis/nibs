import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// The `text-label` / `text-body` / `text-caption` @utility classes (app.css)
// each BUNDLE font-size + font-weight + line-height into one semantic token.
// tailwind-merge doesn't know them out of the box, so a `cn()` override such as
// `cn("text-xs font-medium", "text-caption")` would keep BOTH the raw utilities
// and the semantic one, and CSS source order (font-medium after text-caption)
// would silently win — the override becomes a no-op.
//
// Register them as their own `text-scale` group and declare that a semantic
// bundle appearing later CONFLICTS WITH (i.e. drops) any earlier raw
// font-size / font-weight / line-height. This is intentionally one-directional:
// a raw `font-bold` placed AFTER `text-body` must still win (partial override of
// just the weight), so we do NOT make font-weight/size/leading drop the bundle.
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "text-scale": [{ text: ["label", "body", "caption"] }],
    },
    conflictingClassGroups: {
      "text-scale": ["font-size", "font-weight", "leading"],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export type WithoutChild<T> = T extends { child?: any } ? Omit<T, "child"> : T;
export type WithoutChildren<T> = T extends { children?: any }
  ? Omit<T, "children">
  : T;
export type WithoutChildrenOrChild<T> = WithoutChildren<WithoutChild<T>>;
export type WithElementRef<T, U extends HTMLElement = HTMLElement> = T & {
  ref?: U | null;
};
