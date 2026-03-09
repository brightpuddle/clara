import Foundation
import Observation

@Observable
@MainActor
final class ArtifactStore {
    var artifacts: [Artifact_V1_Artifact] = []
    var selectedArtifactID: String? = nil
    var selectedArtifact: Artifact_V1_Artifact? = nil
    var relatedArtifacts: [Artifact_V1_Artifact] = []
    var sidebarSection: SidebarSection = .all
    var isConnected: Bool = false
    var connectionError: String? = nil
    var isLoading: Bool = false

    private let client: AgentClient
    private var subscribeTask: Task<Void, Never>?

    init(socketPath: String) {
        self.client = AgentClient(socketPath: socketPath)
    }

    func start() async {
        do {
            try await client.connect()
            isConnected = true
            connectionError = nil
            await loadArtifacts()
            startSubscription()
        } catch {
            isConnected = false
            connectionError = error.localizedDescription
        }
    }

    func loadArtifacts() async {
        isLoading = true
        defer { isLoading = false }
        do {
            artifacts = try await client.listArtifacts(kinds: sidebarSection.kinds)
            if selectedArtifactID == nil, let first = artifacts.first {
                await selectArtifact(id: first.id)
            }
        } catch {
            connectionError = error.localizedDescription
        }
    }

    func selectArtifact(id: String) async {
        selectedArtifactID = id
        do {
            let (artifact, related) = try await client.getArtifact(id: id)
            selectedArtifact = artifact
            relatedArtifacts = related
        } catch {
            connectionError = error.localizedDescription
        }
    }

    func markDone(id: String) async {
        do {
            try await client.markDone(id: id)
            artifacts.removeAll { $0.id == id }
            if selectedArtifactID == id {
                selectedArtifactID = artifacts.first?.id
                if let next = selectedArtifactID {
                    await selectArtifact(id: next)
                } else {
                    selectedArtifact = nil
                    relatedArtifacts = []
                }
            }
        } catch {
            connectionError = error.localizedDescription
        }
    }

    func setSidebarSection(_ section: SidebarSection) async {
        sidebarSection = section
        selectedArtifactID = nil
        selectedArtifact = nil
        relatedArtifacts = []
        await loadArtifacts()
    }

    private func startSubscription() {
        subscribeTask?.cancel()
        subscribeTask = Task {
            do {
                let events = try await client.subscribe()
                for await _ in events {
                    await loadArtifacts()
                }
            } catch {
                try? await Task.sleep(nanoseconds: 5_000_000_000)
                startSubscription()
            }
        }
    }

    var filteredArtifacts: [Artifact_V1_Artifact] {
        if sidebarSection == .related {
            return relatedArtifacts
        }
        let kinds = sidebarSection.kinds
        if kinds.isEmpty { return artifacts }
        return artifacts.filter { kinds.contains($0.kind) }
    }
}
