import type { Region } from "./ordering/region";

export type DropZone = "before" | "after" | "reparent";

/**
 * What an accepted plan tells the affordance: its label and the kind of write,
 * with the region for a move. Discriminated by kind rather than a nullable
 * region, so a surface coloring by axis must handle an assignment explicitly.
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
  /** The accepted plan the badge and row indicator draw from, or null. */
  dropAccepted: AcceptedDrop | null = $state(null);
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
