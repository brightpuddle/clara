import SwiftUI

struct SidebarView: View {
    @Binding var selectedSection: SidebarSection
    let store: ArtifactStore

    var body: some View {
        List(SidebarSection.allCases, selection: $selectedSection) { section in
            Label(section.rawValue, systemImage: section.systemImage)
                .badge(badgeCount(for: section))
        }
        .listStyle(.sidebar)
        .navigationTitle("Clara")
        .onChange(of: selectedSection) { _, newValue in
            Task { await store.setSidebarSection(newValue) }
        }
    }

    private func badgeCount(for section: SidebarSection) -> Int {
        switch section {
        case .all: return store.artifacts.count
        case .related: return store.relatedArtifacts.count
        default:
            let kinds = section.kinds
            return store.artifacts.filter { kinds.contains($0.kind) }.count
        }
    }
}
