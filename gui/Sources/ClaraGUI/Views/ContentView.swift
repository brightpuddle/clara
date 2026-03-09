import SwiftUI

struct ContentView: View {
    @State private var store = ArtifactStore(
        socketPath: FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".local/share/clara/agent.sock").path
    )
    @State private var selectedSection: SidebarSection = .all
    @State private var columnVisibility: NavigationSplitViewVisibility = .all

    var body: some View {
        NavigationSplitView(columnVisibility: $columnVisibility) {
            SidebarView(selectedSection: $selectedSection, store: store)
        } content: {
            ArtifactListView(store: store)
        } detail: {
            ArtifactDetailView(store: store)
        }
        .navigationSplitViewStyle(.balanced)
        .task {
            await store.start()
        }
        .overlay(alignment: .bottom) {
            if let error = store.connectionError {
                HStack {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.yellow)
                    Text(error)
                        .font(.caption)
                }
                .padding(8)
                .background(.ultraThinMaterial)
                .clipShape(RoundedRectangle(cornerRadius: 8))
                .padding()
            }
        }
    }
}
