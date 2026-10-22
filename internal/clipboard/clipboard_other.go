//go:build !windows

package clipboard

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func ReadText(ctx context.Context) (string, error) { return runtime.ClipboardGetText(ctx) }

func WriteText(ctx context.Context, text string) error {
	if _, err := encodeText(text); err != nil {
		return err
	}
	return runtime.ClipboardSetText(ctx, text)
}
