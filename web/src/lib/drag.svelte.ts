export type DropZone = "before" | "after" | "reparent";

export class DragState {
  /** IDs of the nibs being dragged */
  draggedIds: string[] = $state([]);
  isDragging: boolean = $derived(this.draggedIds.length > 0);

  /** Shared parent of dragged items (undefined = mixed parents) */
  draggedParentId: string | null | undefined = $state(undefined);

  /** Current drop target */
  dropTargetId: string | null = $state(null);
  dropZone: DropZone | null = $state(null);
  dropValid: boolean = $state(false);

  /** Cursor position (for the count badge) */
  cursorX: number = $state(0);
  cursorY: number = $state(0);

  startDrag(ids: string[], parentId?: string | null): void {
    this.draggedIds = ids;
    this.draggedParentId = parentId;
  }

  setDropTarget(nibId: string | null, zone: DropZone | null, valid: boolean): void {
    this.dropTargetId = nibId;
    this.dropZone = zone;
    this.dropValid = valid;
  }

  clearDropTarget(): void {
    this.dropTargetId = null;
    this.dropZone = null;
    this.dropValid = false;
  }

  endDrag(): void {
    this.draggedIds = [];
    this.draggedParentId = undefined;
    this.clearDropTarget();
    this.cursorX = 0;
    this.cursorY = 0;
  }

  isDraggedItem(nibId: string): boolean {
    return this.draggedIds.includes(nibId);
  }
}
