import Foundation

enum SidebarSection: String, CaseIterable, Identifiable, Hashable {
    case all = "All Artifacts"
    case notes = "Notes"
    case tasks = "Tasks"
    case files = "Files"
    case related = "Related"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .all: return "tray.2"
        case .notes: return "note.text"
        case .tasks: return "checkmark.circle"
        case .files: return "doc"
        case .related: return "link"
        }
    }

    var kinds: [Artifact_V1_ArtifactKind] {
        switch self {
        case .all: return []
        case .notes: return [.note]
        case .tasks: return [.reminder, .task]
        case .files: return [.file]
        case .related: return []
        }
    }
}
