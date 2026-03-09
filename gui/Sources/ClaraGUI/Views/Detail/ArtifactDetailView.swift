import AppKit
import SwiftUI

struct ArtifactDetailView: View {
    @Bindable var store: ArtifactStore

    var body: some View {
        if let artifact = store.selectedArtifact {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    HStack {
                        ArtifactRowView(artifact: artifact)
                    }

                    Divider()

                    MetadataView(artifact: artifact)

                    Divider()

                    if !artifact.content.isEmpty {
                        Text(artifact.content)
                            .font(.body)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }

                    if !store.relatedArtifacts.isEmpty {
                        Divider()
                        RelatedItemsView(related: store.relatedArtifacts) { id in
                            Task { await store.selectArtifact(id: id) }
                        }
                    }
                }
                .padding()
            }
            .navigationTitle(artifact.title)
            .toolbar {
                ToolbarItemGroup(placement: .automatic) {
                    Button(action: { Task { await store.markDone(id: artifact.id) } }) {
                        Label("Mark Done", systemImage: "checkmark.circle")
                    }
                    .help("Mark as done and remove from list")

                    Button(action: { openNative(artifact: artifact) }) {
                        Label("Open", systemImage: "arrow.up.right.square")
                    }
                    .help("Open in native app")

                    Button(action: { openInEditor(artifact: artifact) }) {
                        Label("Edit", systemImage: "pencil")
                    }
                    .help("Open in \(editorName)")
                }
            }
        } else {
            ContentUnavailableView(
                "No Selection",
                systemImage: "tray.2",
                description: Text("Select an artifact to view details")
            )
        }
    }

    var editorName: String {
        ProcessInfo.processInfo.environment["EDITOR"] ?? "editor"
    }

    func openNative(artifact: Artifact_V1_Artifact) {
        switch artifact.sourceApp {
        case "reminders":
            if !artifact.sourcePath.isEmpty {
                let script = """
                tell application "Reminders"
                    activate
                    repeat with theList in every list
                        set matches to (reminders of theList whose id is "\(artifact.sourcePath)")
                        if (count of matches) > 0 then
                            show item 1 of matches
                            exit repeat
                        end if
                    end repeat
                end tell
                """
                let task = Process()
                task.launchPath = "/usr/bin/osascript"
                task.arguments = ["-e", script]
                try? task.run()
            }
        case "mail":
            NSWorkspace.shared.open(URL(fileURLWithPath: "/System/Applications/Mail.app"))
        default:
            if !artifact.sourcePath.isEmpty, let url = URL(string: "file://" + artifact.sourcePath) {
                NSWorkspace.shared.open(url)
            }
        }
    }

    func openInEditor(artifact: Artifact_V1_Artifact) {
        let editor = ProcessInfo.processInfo.environment["EDITOR"] ?? "vim"
        if artifact.sourcePath.isEmpty || artifact.sourceApp == "reminders" {
            guard let tmpURL = writeTempFile(artifact: artifact) else { return }
            openTerminalEditor(editor: editor, path: tmpURL.path)
        } else {
            openTerminalEditor(editor: editor, path: artifact.sourcePath)
        }
    }

    func openTerminalEditor(editor: String, path: String) {
        let script = "tell application \"Terminal\" to do script \"\(editor) '\(path)'\""
        let task = Process()
        task.launchPath = "/usr/bin/osascript"
        task.arguments = ["-e", script]
        try? task.run()
    }

    func writeTempFile(artifact: Artifact_V1_Artifact) -> URL? {
        let tmpDir = FileManager.default.temporaryDirectory
        let tmpURL = tmpDir.appendingPathComponent("clara-\(artifact.id.hashValue).md")
        let content = "# \(artifact.title)\n\n\(artifact.content)"
        try? content.write(to: tmpURL, atomically: true, encoding: .utf8)
        return tmpURL
    }
}

struct MetadataView: View {
    let artifact: Artifact_V1_Artifact

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 6) {
            GridRow {
                Text("Kind").foregroundStyle(.secondary)
                Text(kindName(artifact.kind))
            }
            GridRow {
                Text("Heat").foregroundStyle(.secondary)
                HStack {
                    HeatIndicatorView(score: artifact.heatScore)
                    Text(String(format: "%.2f", artifact.heatScore))
                }
            }
            if !artifact.sourcePath.isEmpty {
                GridRow {
                    Text("Source").foregroundStyle(.secondary)
                    Text(artifact.sourcePath)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            if artifact.hasDueAt {
                GridRow {
                    Text("Due").foregroundStyle(.secondary)
                    Text(artifact.dueAt.date.formatted(date: .abbreviated, time: .shortened))
                }
            }
            if !artifact.tags.isEmpty {
                GridRow {
                    Text("Tags").foregroundStyle(.secondary)
                    Text(artifact.tags.joined(separator: ", "))
                }
            }
        }
        .font(.callout)
    }

    func kindName(_ kind: Artifact_V1_ArtifactKind) -> String {
        switch kind {
        case .reminder: return "Reminder"
        case .note: return "Note"
        case .file: return "File"
        case .email: return "Email"
        case .bookmark: return "Bookmark"
        case .log: return "Log"
        case .task: return "Task"
        default: return "Unknown"
        }
    }
}
