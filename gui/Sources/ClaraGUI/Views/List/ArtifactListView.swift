import SwiftUI

struct ArtifactListView: View {
    @Bindable var store: ArtifactStore
    @FocusState private var isFocused: Bool
    @State private var searchText = ""

    var displayedArtifacts: [Artifact_V1_Artifact] {
        if searchText.isEmpty { return store.filteredArtifacts }
        let q = searchText.lowercased()
        return store.filteredArtifacts.filter {
            $0.title.lowercased().contains(q) || $0.content.lowercased().contains(q)
        }
    }

    var body: some View {
        List(displayedArtifacts, id: \.id, selection: $store.selectedArtifactID) { artifact in
            ArtifactRowView(artifact: artifact)
                .tag(artifact.id)
        }
        .listStyle(.inset)
        .searchable(text: $searchText, prompt: "Search artifacts")
        .navigationTitle(store.sidebarSection.rawValue)
        .navigationSubtitle("\(displayedArtifacts.count) items")
        .toolbar {
            ToolbarItem(placement: .automatic) {
                if store.isLoading {
                    ProgressView().controlSize(.small)
                }
            }
        }
        .onChange(of: store.selectedArtifactID) { _, newID in
            guard let id = newID else { return }
            Task { await store.selectArtifact(id: id) }
        }
        .focused($isFocused)
        .onKeyPress(.init("j")) {
            navigateDown()
            return .handled
        }
        .onKeyPress(.init("k")) {
            navigateUp()
            return .handled
        }
        .onKeyPress(.init("G")) {
            if let last = displayedArtifacts.last {
                store.selectedArtifactID = last.id
            }
            return .handled
        }
        .onKeyPress(.space) {
            if let id = store.selectedArtifactID {
                Task { await store.markDone(id: id) }
            }
            return .handled
        }
    }

    private func navigateDown() {
        guard !displayedArtifacts.isEmpty else { return }
        if let current = store.selectedArtifactID,
           let idx = displayedArtifacts.firstIndex(where: { $0.id == current }),
           idx < displayedArtifacts.count - 1 {
            store.selectedArtifactID = displayedArtifacts[idx + 1].id
        } else if store.selectedArtifactID == nil {
            store.selectedArtifactID = displayedArtifacts.first?.id
        }
    }

    private func navigateUp() {
        guard !displayedArtifacts.isEmpty else { return }
        if let current = store.selectedArtifactID,
           let idx = displayedArtifacts.firstIndex(where: { $0.id == current }),
           idx > 0 {
            store.selectedArtifactID = displayedArtifacts[idx - 1].id
        }
    }
}
