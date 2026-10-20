// Package nativedialog opens the platform file picker for the Wails desktop apps.
//
// On macOS the Wails runtime presents its panel as a window-attached sheet,
// which the user cannot drag. The darwin implementation here shows a regular
// modal open panel instead so the window stays movable.
package nativedialog

import (
	"context"
	"strings"
)

// Filter mirrors the filter list passed to the frontend binding.
type Filter struct {
	Name       string
	Extensions []string
}

// OpenFile shows a single-file picker and returns the selected absolute path.
// An empty string means the user cancelled.
func OpenFile(ctx context.Context, filters []Filter) (string, error) {
	return openFile(ctx, filters)
}

func openFileExtensions(filters []Filter) string {
	seen := make(map[string]bool, 4)
	extensions := make([]string, 0, 4)
	for _, filter := range filters {
		for _, ext := range filter.Extensions {
			ext = strings.TrimSpace(strings.TrimPrefix(ext, "."))
			if ext == "" || seen[ext] {
				continue
			}
			seen[ext] = true
			extensions = append(extensions, ext)
		}
	}
	return strings.Join(extensions, ";")
}

func openFileMessage(filters []Filter) string {
	for _, filter := range filters {
		if name := strings.TrimSpace(filter.Name); name != "" {
			return name
		}
	}
	return ""
}
