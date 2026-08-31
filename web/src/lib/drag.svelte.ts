import type { Region } from "./ordering/region";

export type DropZone = "before" | "after" | "reparent";

/**
 * What an ACCEPTED plan tells the affordance: the sentence it will carry out,
 * and the list it will carry it out in. The two travel together because a
 * refusal has neither, so nothing can end up showing one without the other.
 */
export interface AcceptedDrop {
  label: string;
  region: Region;
}

export class DragState {
  /** IDs of the nibs being dragged */
  draggedIds: string[] = $state([]);
  isDragging: boolean = $derived(this.draggedIds.length > 0);

  /** Current drop target */
  dropTargetId: string | null = $state(null);
  dropZone: DropZone | null = $state(null);
  dropValid: boolean = $state(false);
  /** The accepted plan's own sentence, or null while nothing would happen. */
  dropLabel: string | null = $state(null);
  /** The list the accepted drop writes in, so the indicator can say which axis. */
  dropRegion: Region | null = $state(null);

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
    this.dropLabel = accepted?.label ?? null;
    this.dropRegion = accepted?.region ?? null;
  }

  clearDropTarget(): void {
    this.dropTargetId = null;
    this.dropZone = null;
    this.dropValid = false;
    this.dropLabel = null;
    this.dropRegion = null;
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
