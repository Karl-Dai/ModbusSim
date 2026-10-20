//go:build !darwin

package nativedialog

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func openFile(ctx context.Context, filters []Filter) (string, error) {
	runtimeFilters := make([]runtime.FileFilter, 0, len(filters))
	for _, filter := range filters {
		patterns := make([]string, 0, len(filter.Extensions))
		for _, ext := range filter.Extensions {
			patterns = append(patterns, "*."+strings.TrimPrefix(ext, "."))
		}
		runtimeFilters = append(runtimeFilters, runtime.FileFilter{
			DisplayName: filter.Name,
			Pattern:     strings.Join(patterns, ";"),
		})
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{Filters: runtimeFilters})
}
