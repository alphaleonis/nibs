package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// getEditor returns the user's preferred editor from environment variables.
// Checks $VISUAL first, then $EDITOR (matching the POSIX convention).
// Returns empty string if no editor is configured — callers should fall back
// to non-interactive behavior (e.g., using the body template as-is).
func getEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return ""
}

// editContent opens the user's editor with initialContent pre-filled in a temp file.
// Returns the edited content after the editor exits.
// Returns the initialContent unchanged if no editor is available.
func editContent(initialContent string) (string, error) {
	editor := getEditor()
	if editor == "" {
		return initialContent, nil
	}

	// Create temp file with the initial content
	tmpFile, err := os.CreateTemp("", "nibs-*.md")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.WriteString(initialContent); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("writing temp file: %w", err)
	}
	_ = tmpFile.Close()

	// Open editor
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	// Read back edited content
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("reading edited file: %w", err)
	}
	return string(data), nil
}
