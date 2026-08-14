// Makes md-view the default handler for markdown files.
// Usage: swift scripts/set-default.swift [/path/to/md-view.app]
import AppKit
import UniformTypeIdentifiers

let appPath = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "/Applications/MDv.app"
let appURL = URL(fileURLWithPath: appPath)
guard FileManager.default.fileExists(atPath: appPath) else {
    print("error: \(appPath) not found — install the app first")
    exit(1)
}

let exts = ["md", "markdown", "mdown", "mkd"]
let ws = NSWorkspace.shared

// Extensions can share a UTI (e.g. net.daringfireball.markdown) — dedupe.
var types: [String: UTType] = [:]
for e in exts {
    if let t = UTType(filenameExtension: e) {
        types[t.identifier] = t
    } else {
        print("\(e): no UTType — skipped")
    }
}

let group = DispatchGroup()
var failed = false
for (id, t) in types {
    let before = ws.urlForApplication(toOpen: t)?.lastPathComponent ?? "none"
    group.enter()
    ws.setDefaultApplication(at: appURL, toOpen: t) { err in
        if let err = err {
            print("\(id): FAILED — \(err.localizedDescription)")
            failed = true
        } else {
            let after = ws.urlForApplication(toOpen: t)?.lastPathComponent ?? "none"
            print("\(id): \(before) -> \(after)")
        }
        group.leave()
    }
}
group.wait()
exit(failed ? 1 : 0)
