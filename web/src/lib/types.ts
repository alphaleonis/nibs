export interface NibSummary {
  id: string;
  title: string;
  status: string;
  type: string;
  priority: string;
  estimate: string;
  tags: string[];
  updatedAt: string;
}

export interface NibFilter {
  search?: string;
  status?: string[];
  excludeStatus?: string[];
  type?: string[];
  excludeType?: string[];
  priority?: string[];
  excludePriority?: string[];
  estimate?: string[];
  excludeEstimate?: string[];
  tags?: string[];
  excludeTags?: string[];
  hasParent?: boolean;
  parentId?: string;
  hasBlocking?: boolean;
  blockingId?: string;
  isBlocked?: boolean;
  hasBlockedBy?: boolean;
  blockedById?: string;
  noParent?: boolean;
  noBlocking?: boolean;
  noBlockedBy?: boolean;
}

export interface TreeNib extends NibSummary {
  parentId: string | null;
}

export interface TreeTableNib extends TreeNib {
  blockingIds: string[];
  blockedByIds: string[];
}

export interface TreeNode<T extends TreeNib = TreeNib> {
  nib: T;
  children: TreeNode<T>[];
  depth: number;
}

export const VIEW_LEVELS = ["milestones", "epics", "backlog"] as const;
export type ViewLevel = (typeof VIEW_LEVELS)[number];

export const ALL_COLUMN_KEYS = ["id", "parent", "type", "title", "state", "effort", "tags"] as const;
export type ColumnKey = (typeof ALL_COLUMN_KEYS)[number];

export interface ColumnConfig {
  key: ColumnKey;
  label: string;
  alwaysVisible: boolean;
}

export const DEFAULT_COLUMNS: ColumnConfig[] = [
  { key: "id", label: "ID", alwaysVisible: false },
  { key: "parent", label: "Parent", alwaysVisible: false },
  { key: "type", label: "Type", alwaysVisible: false },
  { key: "title", label: "Title", alwaysVisible: true },
  { key: "state", label: "State", alwaysVisible: false },
  { key: "effort", label: "Effort", alwaysVisible: false },
  { key: "tags", label: "Tags", alwaysVisible: false },
];

export const DEFAULT_COLUMN_WIDTHS: Record<ColumnKey, number> = {
  id: 100,
  parent: 160,
  type: 80,
  title: 400,
  state: 120,
  effort: 70,
  tags: 150,
};

export const DEFAULT_DETAIL_PANEL_WIDTH = 400;
export const MIN_DETAIL_PANEL_WIDTH = 200;
export const MAX_DETAIL_PANEL_PERCENT = 75;

export type RowDensity = "compact" | "comfortable";

export interface FilterPreferences {
  filter: NibFilter;
  viewLevel: ViewLevel;
  columnVisibility?: Partial<Record<ViewLevel, ColumnKey[]>>;
  columnWidths?: Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>>;
  detailPanelWidth?: number;
  rowDensity?: RowDensity;
}
