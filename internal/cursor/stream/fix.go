package stream

import "github.com/lich0821/ccNexus/internal/cursor/shared"

func Fix(meta shared.RequestMeta, bundle []byte, hook func([]byte) ([]byte, error)) ([]byte, error) {
	if !meta.CursorMode || hook == nil {
		return bundle, nil
	}
	return hook(bundle)
}
