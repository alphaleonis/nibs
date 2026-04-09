import DOMPurify from "dompurify";
import { marked } from "marked";

export function renderMarkdown(body: string): string {
  if (!body) return "";
  return DOMPurify.sanitize(marked.parse(body, { async: false }) as string);
}

export const TAG_REGEX = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
