// The Quick Look preview extension: pressing Space on a markdown file in Finder
// renders it through MDv's own pipeline.
//
// This is the only path that puts a rendered document on screen in well under
// 100 ms. A cold app launch cannot — ARCHITECTURE.md measures ~158 ms of
// AppKit and WebKit startup before MDv runs a line — but the Quick Look host
// process is already running and warm, so the preview skips all of it.
//
// All rendering happens in Go, in libmdvpreview.a (see quicklook/bridge), so a
// preview and the window it precedes can never disagree about a document.

import Foundation
import QuickLookUI
import UniformTypeIdentifiers
import os.log

// A preview extension has nowhere to print: it runs in its own sandboxed
// process, spawned by Quick Look. os_log is the only way to see what it did, and
// the only way to measure it — `log show --predicate 'subsystem == "com.wails.md-view"'`.
private let previewLog = Logger(subsystem: "com.wails.md-view", category: "quicklook")

final class PreviewProvider: QLPreviewProvider, QLPreviewingController {

    func providePreview(for request: QLFilePreviewRequest) async throws -> QLPreviewReply {
        // Resolve the theme once, here, rather than inside the reply block:
        // it reads the app's settings file, which the sandbox may refuse, and
        // a refusal should mean "light" rather than a failed preview.
        let dark = MDvPreviewPrefersDark()
        let path = request.fileURL.path

        return QLPreviewReply(
            dataOfContentType: .html,
            contentSize: CGSize(width: 800, height: 900)
        ) { (_: QLPreviewReply) -> Data in
            let started = DispatchTime.now().uptimeNanoseconds
            guard let rendered = MDvRenderPreview(strdup(path), dark) else {
                previewLog.error("render returned nothing for \(path, privacy: .public)")
                throw PreviewError.renderFailed(path)
            }
            defer { MDvFreePreview(rendered) }
            guard let data = String(cString: rendered).data(using: .utf8) else {
                previewLog.error("rendered HTML was not valid UTF-8")
                throw PreviewError.renderFailed(path)
            }
            let ms = Double(DispatchTime.now().uptimeNanoseconds - started) / 1e6
            previewLog.log("rendered \(data.count, privacy: .public) bytes in \(ms, privacy: .public) ms")
            return data
        }
    }
}

enum PreviewError: LocalizedError {
    case renderFailed(String)

    var errorDescription: String? {
        switch self {
        case .renderFailed(let path):
            return "MDv could not render \((path as NSString).lastPathComponent)."
        }
    }
}
