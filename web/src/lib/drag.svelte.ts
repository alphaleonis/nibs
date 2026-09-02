import type { Region } from "./ordering/region";

export type DropZone = "before" | "after" | "reparent";

/**
 * What an ACCEPTED plan tells the affordance: the sentence it will carry out,
 * and what KIND of write it is — the list for a move, nothing but the kind for
 * an assignment, which writes no position. They travel together because a
 * refusal has neither, so nothing can end up showing one without the other.
 *
 * The plan's own discriminant rather than a nullable region, so a surface
 * coloring by axis has to answer for the arm that has none instead of reading
 * `region?.axis` and taking the parent axis's colors by default.
 */
export type AcceptedDrop =
  | { readonly kind: "position"; readonly label: string; readonly region: Region }
  | { readonly kind: "assign"; readonly label: string };

export class DragState {
  /** IDs of the nibs being dragged */
  draggedIds: string[] = $state([]);
  isDragging: boolean = $derived(this.draggedIds.length > 0);

  /** Current drop target */
  dropTargetId: string | null = $state(null);
  dropZone: DropZone | null = $state(null);
  dropValid: boolean = $state(false);
  /**
   * The accepted plan, or null while nothing would happen — what the badge and
   * the row indicator are drawn from.
   */
  dropAccepted: AcceptedDrop | null = $state(null);
  /** The accepted plan's own sentence, or null while nothing would happen. */
  dropLabel: string | null = $derived(this.dropAccepted?.label ?? null);

  /** Cursor position (for the badge) */
  cursorX: number = $state(0);
  cursorY: number = $state(0);

  startDrag(ids: string[]): void {
    this.draggedIds = ids;
  }

  setDropTarget(nibId: string | null, zone: DropZone | null, valid: boolean, accepted: AcceptedDrop | null = null): void {
    this.dropTargetId = nibId;
    this.dropZone = zone;
    this.dropValid = valid;
    this.dropAccepted = accepted;
  }

  clearDropTarget(): void {
    this.dropTargetId = null;
    this.dropZone = null;
    this.dropValid = false;
    this.dropAccepted = null;
  }

  endDrag(): void {
    this.draggedIds = [];
    this.clearDropTarget();
    this.cursorX = 0;
    this.cursorY = 0;
  }

  isDraggedItem(nibId: string): boolean {
    return this.draggedIds.includes(nibId);
  }
}
