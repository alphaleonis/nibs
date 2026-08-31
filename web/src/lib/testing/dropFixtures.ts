import { expect } from "vitest";

/**
 * The nib set the App-level drag suites drive, and the gesture that drives it.
 *
 * Shared rather than copied: the suites differ in what they observe, not in
 * which drags they perform, and a fixture that drifted between them would let
 * two files claim to describe the same gesture while both stayed green.
 */

const MILESTONE = {
  id: "nibs-m1",
  title: "v1.0 Launch",
  status: "in-progress",
  type: "milestone",
  priority: "critical",
  estimate: "",
  tags: [] as string[],
  updatedAt: "2026-03-20T10:00:00Z",
  parentId: null as string | null,
  milestone: "",
  milestoneOrder: "",
  blockingIds: [] as string[],
  blockedByIds: [] as string[],
  etag: "etag-m1",
};

/** Already in the queue, so it is the anchor a positioned drop would name. */
const QUEUED_EPIC = {
  ...MILESTONE,
  id: "nibs-e1",
  title: "Queued epic",
  type: "epic",
  milestone: "nibs-m1",
  milestoneOrder: "a",
  etag: "etag-e1",
};

/** In no queue, so dragging it into one is the refusal under test. */
const BACKLOG_TASK = {
  ...MILESTONE,
  id: "nibs-b1",
  title: "Backlog task",
  type: "task",
  priority: "normal",
  etag: "etag-b1",
};

/** A queue member that a container OUTSIDE the queue can hold, which is what
 *  the opposite refusal needs: a task may sit under an epic. */
const QUEUED_TASK = {
  ...MILESTONE,
  id: "nibs-q1",
  title: "Queued task",
  type: "task",
  priority: "normal",
  milestone: "nibs-m1",
  milestoneOrder: "b",
  etag: "etag-q1",
};

/** The container it would move into — in no queue, so the move crosses axes. */
const BACKLOG_EPIC = {
  ...MILESTONE,
  id: "nibs-b2",
  title: "Backlog epic",
  type: "epic",
  priority: "normal",
  etag: "etag-b2",
};

export const DROP_TEST_NIBS = [MILESTONE, QUEUED_EPIC, QUEUED_TASK, BACKLOG_TASK, BACKLOG_EPIC];

/** The milestone header row, whose middle is the queue entry a drop can aim at. */
export const MILESTONE_ID = MILESTONE.id;
/** Its title, which is how the plan's prose and its remedy label spell the queue. */
export const MILESTONE_TITLE = MILESTONE.title;
/** In no queue: dragging it onto the header is the refusal that carries a remedy. */
export const BACKLOG_TASK_ID = BACKLOG_TASK.id;
/** In the queue: dragging it out is the refusal that carries none. */
export const QUEUED_TASK_ID = QUEUED_TASK.id;
/** Outside the queue and able to hold a task, so that drag crosses axes. */
export const BACKLOG_EPIC_ID = BACKLOG_EPIC.id;

/**
 * Drag `sourceId`'s row onto `targetId`'s and release, so the plan under test is
 * the one the pointer path actually produced rather than one handed to the
 * handler directly.
 *
 * jsdom has no `document.elementFromPoint`, which is how the drag resolves the
 * row under the cursor, so it is stubbed to name the target. The stub survives
 * only for the gesture, so an unrelated hit test later is not answered by it.
 */
export function dragOnto(container: HTMLElement, sourceId: string, targetId: string) {
  const source = container.querySelector(`tr[data-nib-id="${sourceId}"]`) as HTMLElement;
  const target = container.querySelector(`tr[data-nib-id="${targetId}"]`) as HTMLElement;
  expect(source).not.toBeNull();
  expect(target).not.toBeNull();

  const saved = document.elementFromPoint;
  document.elementFromPoint = () => target;
  try {
    source.dispatchEvent(
      new PointerEvent("pointerdown", { clientX: 100, clientY: 100, bubbles: true, button: 0 }),
    );
    // One move: it crosses the 5px threshold AND lands on the target, which is
    // the same handler call that plans the drop.
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 140, clientY: 100, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
  } finally {
    document.elementFromPoint = saved;
  }
}
