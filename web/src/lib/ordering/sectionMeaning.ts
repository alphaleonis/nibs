import type { UpdateNibInput } from "../mutations/types";
import type { Region } from "./region";

/** The keys of `T` whose value is a string once null and undefined are removed. */
type StringKeys<T> = { [K in keyof T]-?: NonNullable<T[K]> extends string ? K : never }[keyof T];

/**
 * A field a section entry may set: a string-valued key of the mutation layer's
 * `UpdateNibInput`, whose keys are bound to the generated input's in
 * `mutations/types.ts`, minus `ifMatch`.
 */
export type AssignableField = StringKeys<UpdateNibInput> & string;

/** What a drop INTO a section does. */
export type SectionEntry =
  /** Join an ordering group — a milestone queue. */
  | { readonly kind: "region"; readonly region: Region }
  /**
   * Set one scalar field on each dragged nib. Membership is the field's value,
   * so the drop writes no position. `noun` names the axis in a sentence ("the
   * web/dashboard area").
   */
  | { readonly kind: "assign"; readonly field: AssignableField; readonly value: string; readonly noun: string }
  /** The section says nothing; the row under the cursor decides by its type. */
  | { readonly kind: "byRow" }
  /** Entering is meaningless, and this is the sentence saying why. */
  | { readonly kind: "refuse"; readonly message: string };

/**
 * What a section means to a drag: the group its members are ordered in, and
 * what entering it does. A milestone section answers both with its queue; an
 * area section orders nothing and assigns a field; a type lens's section
 * answers neither.
 */
export interface SectionMeaning {
  /** The ordering group a section's rows are members of, or null to declare
   *  none — in which case each row falls back to its own resolved parent group. */
  readonly memberRegion: Region | null;
  readonly onEnter: SectionEntry;
}

/**
 * A section that governs nothing: its rows keep their own parent group, and
 * entering it means whatever the row under the cursor means. Every type lens's
 * answer, and every lens's leftover section's.
 */
export const GOVERNS_NOTHING: SectionMeaning = { memberRegion: null, onEnter: { kind: "byRow" } };
