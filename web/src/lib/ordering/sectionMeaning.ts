import type { UpdateNibInput } from "../mutations/types";
import type { Region } from "./region";

/**
 * The keys of `T` whose value is a string once null and undefined are removed.
 * The array- and object-valued keys drop out because neither extends `string`.
 */
type StringKeys<T> = { [K in keyof T]-?: NonNullable<T[K]> extends string ? K : never }[keyof T];

/**
 * A field a section entry may set: a string-valued key of the update input the
 * mutation layer actually SENDS, which is what makes `assignmentFor`'s computed
 * key safe. That input is bound both ways to the generated one in
 * `mutations/types.ts`, so this admits every scalar argument the server declares
 * and no key it has no argument for — while still excluding `ifMatch`, which is
 * a command-level concern there rather than a field a section could assign.
 *
 * `& string` because `keyof` admits symbols the mapped type above would carry
 * through.
 */
export type AssignableField = StringKeys<UpdateNibInput> & string;

/**
 * What a drop INTO a section does.
 *
 * A closed union rather than a nullable region, because the answers are not
 * degrees of one thing: joining an ordering group and setting a field are
 * different writes, and "the section says nothing" is not the absence of an
 * answer but one of them.
 */
export type SectionEntry =
  /** Join an ordering group — today's milestone queue. */
  | { readonly kind: "region"; readonly region: Region }
  /**
   * Set one scalar field on each dragged nib. Membership IS the field's value,
   * so the section has no order axis of its own and a drop into it writes no
   * position. `noun` names the axis in a sentence ("the web/dashboard area").
   */
  | { readonly kind: "assign"; readonly field: AssignableField; readonly value: string; readonly noun: string }
  /** The section says nothing; the row under the cursor decides by its type. */
  | { readonly kind: "byRow" }
  /** Entering is meaningless, and this is the sentence saying why. */
  | { readonly kind: "refuse"; readonly message: string };

/**
 * What a section MEANS, in the two directions a drag asks about it: the group
 * its members are ordered in, and what entering it does.
 *
 * Two members because the questions are independent. A milestone section
 * answers both with its queue; an area section orders nothing and assigns a
 * field; a type lens's section answers neither.
 */
export interface SectionMeaning {
  /** The ordering group a section's rows are members of, or null to declare
   *  none — in which case each row falls back to its own resolved parent group. */
  readonly memberRegion: Region | null;
  readonly onEnter: SectionEntry;
}

/**
 * The meaning of a section that governs nothing: its rows keep their own parent
 * group, and entering it means whatever the row under the cursor means.
 *
 * Every type lens's answer, and the leftover section's answer under every lens.
 */
export const GOVERNS_NOTHING: SectionMeaning = { memberRegion: null, onEnter: { kind: "byRow" } };
