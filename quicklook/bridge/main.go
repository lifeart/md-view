// Command bridge is not a program: built with -buildmode=c-archive it becomes
// libmdvpreview.a, the static library the Quick Look extension links against so
// the preview is rendered by exactly the same pipeline as the app
// (internal/render -> internal/quicklook). Two markdown renderers that could
// disagree would be worse than no preview at all.
//
// Built by scripts/build-quicklook.sh; see quicklook/MDvQuickLook for the
// Swift side.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"md-view/internal/quicklook"
)

// MDvRenderPreview renders the markdown file at path into one self-contained
// HTML document. The returned string is C-allocated and the caller must release
// it with MDvFreePreview. It never returns NULL for a readable path: an
// unreadable one yields a styled error page rather than an empty panel.
//
//export MDvRenderPreview
func MDvRenderPreview(path *C.char, dark C.int) *C.char {
	return C.CString(quicklook.Render(C.GoString(path), dark != 0))
}

// MDvPreviewPrefersDark reports whether the reader's persisted MDv theme is
// dark, so a preview does not contradict the window they are about to open.
//
//export MDvPreviewPrefersDark
func MDvPreviewPrefersDark() C.int {
	if quicklook.DefaultDark() {
		return 1
	}
	return 0
}

// MDvFreePreview releases a string returned by MDvRenderPreview.
//
//export MDvFreePreview
func MDvFreePreview(p *C.char) {
	C.free(unsafe.Pointer(p))
}

func main() {}
