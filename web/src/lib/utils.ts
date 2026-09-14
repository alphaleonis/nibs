import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// `text-label` / `text-body` / `text-caption` (app.css @utility) each set
// font-size, font-weight and line-height. Unregistered, tailwind-merge keeps an
// earlier raw `text-xs font-medium` beside them, and CSS source order decides.
//
// A later semantic class drops earlier raw size/weight/leading. One-directional:
// a raw `font-bold` after `text-body` still overrides just the weight.
const twMerge = extendTailwindMerge<"text-scale">({
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
