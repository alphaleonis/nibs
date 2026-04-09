/**
 * Composable for managing the editor modal and type picker popover state.
 *
 * Encapsulates: editorOpen, editorMode, editorNibId, editorNibData,
 * editorDefaultType, editorDefaultParent, typePickerOpen, typePickerParentId,
 * typePickerParentType, and all related handlers.
 */

import type { Client } from "@urql/core";
import { toast } from "svelte-sonner";
import { NIB_DETAIL_QUERY } from "../queries";
import { getValidChildTypes } from "../typeHierarchy";
import type { NibData } from "../components/EditorModal.svelte";
import type { SelectionState } from "../selection.svelte";

export interface EditorOrchestrationState {
  readonly editorOpen: boolean;
  readonly editorMode: "create" | "edit";
  readonly editorNibId: string | undefined;
  readonly editorNibData: NibData | undefined;
  readonly editorDefaultType: string;
  readonly editorDefaultParent: string | undefined;
  readonly typePickerOpen: boolean;
  readonly typePickerParentId: string;
  readonly typePickerParentType: string;
  handleCreateNew: (type: string) => void;
  handleEditNib: (nibId: string) => void;
  handleAddChild: (parentId: string, parentType: string) => void;
  handleTypePickerSelect: (selectedType: string) => void;
  handleEditorClose: () => void;
  handleEditorSave: (nibId: string) => void;
  closeTypePicker: () => void;
}

export function createEditorOrchestration(opts: {
  client: Client;
  selection: SelectionState;
}): EditorOrchestrationState {
  const { client, selection } = opts;

  // Editor modal state
  let editorOpen = $state(false);
  let editorMode: "create" | "edit" = $state("create");
  let editorNibId: string | undefined = $state(undefined);
  let editorNibData: NibData | undefined = $state(undefined);
  let editorDefaultType: string = $state("task");
  let editorDefaultParent: string | undefined = $state(undefined);

  // Type picker popover state for child creation
  let typePickerOpen = $state(false);
  let typePickerParentId: string = $state("");
  let typePickerParentType: string = $state("");

  function openCreateEditor(type: string, parentId?: string) {
    editorDefaultType = type;
    editorDefaultParent = parentId;
    editorNibId = undefined;
    editorNibData = undefined;
    editorMode = "create";
    editorOpen = true;
  }

  function handleCreateNew(type: string) {
    openCreateEditor(type);
  }

  function handleEditNib(nibId: string) {
    client.query(
      NIB_DETAIL_QUERY,
      { id: nibId },
    ).toPromise().then(res => {
      if (res.error) {
        toast.error(res.error.message);
        return;
      }
      if (!res.data?.nib) {
        toast.error(`Nib ${nibId} not found`);
        return;
      }
      const nib = res.data.nib;
      editorNibId = nibId;
      editorNibData = {
        title: nib.title,
        status: nib.status,
        type: nib.type,
        priority: nib.priority ?? "",
        estimate: nib.estimate ?? "",
        tags: nib.tags ?? [],
        body: nib.body ?? "",
        etag: nib.etag,
      };
      editorMode = "edit";
      editorOpen = true;
    }).catch(err => {
      toast.error(`Failed to load nib: ${err.message}`);
    });
  }

  function handleAddChild(parentId: string, parentType: string) {
    const validTypes = getValidChildTypes(parentType);
    if (validTypes.length === 0) return;

    if (validTypes.length === 1) {
      // Single valid type -- go directly to editor
      openCreateEditor(validTypes[0], parentId);
    } else {
      // Multiple valid types -- show type picker
      typePickerParentId = parentId;
      typePickerParentType = parentType;
      typePickerOpen = true;
    }
  }

  function handleTypePickerSelect(selectedType: string) {
    typePickerOpen = false;
    openCreateEditor(selectedType, typePickerParentId);
  }

  function handleEditorClose() {
    editorOpen = false;
    editorMode = "create";
    editorNibId = undefined;
    editorNibData = undefined;
    editorDefaultType = "task";
    editorDefaultParent = undefined;
  }

  function handleEditorSave(nibId: string) {
    selection.select(nibId);
  }

  function closeTypePicker() {
    typePickerOpen = false;
  }

  return {
    get editorOpen() { return editorOpen; },
    get editorMode() { return editorMode; },
    get editorNibId() { return editorNibId; },
    get editorNibData() { return editorNibData; },
    get editorDefaultType() { return editorDefaultType; },
    get editorDefaultParent() { return editorDefaultParent; },
    get typePickerOpen() { return typePickerOpen; },
    get typePickerParentId() { return typePickerParentId; },
    get typePickerParentType() { return typePickerParentType; },
    handleCreateNew,
    handleEditNib,
    handleAddChild,
    handleTypePickerSelect,
    handleEditorClose,
    handleEditorSave,
    closeTypePicker,
  };
}
