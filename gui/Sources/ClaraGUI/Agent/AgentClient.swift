import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2

/// Thread-safe gRPC client for the Clara agent.
actor AgentClient {
    private var grpcClient: GRPCClient<HTTP2ClientTransport.Posix>?
    private let socketPath: String

    init(socketPath: String) {
        self.socketPath = socketPath
    }

    func connect() async throws {
        let transport = try HTTP2ClientTransport.Posix(
            target: .unixDomainSocket(path: socketPath),
            transportSecurity: .plaintext
        )
        let client = GRPCClient(transport: transport)
        self.grpcClient = client
        Task {
            try? await client.runConnections()
        }
    }

    private func agentClient() throws -> Agent_V1_AgentService.Client<HTTP2ClientTransport.Posix> {
        guard let c = grpcClient else { throw AgentError.notConnected }
        return Agent_V1_AgentService.Client(wrapping: c)
    }

    func listArtifacts(kinds: [Artifact_V1_ArtifactKind] = []) async throws -> [Artifact_V1_Artifact] {
        var req = Agent_V1_ListArtifactsRequest()
        req.kinds = kinds
        return try await agentClient().listArtifacts(req) { response in
            try response.message.artifacts
        }
    }

    func getArtifact(id: String) async throws -> (artifact: Artifact_V1_Artifact, related: [Artifact_V1_Artifact]) {
        var req = Agent_V1_GetArtifactRequest()
        req.id = id
        return try await agentClient().getArtifact(req) { response in
            let msg = try response.message
            return (msg.artifact, msg.related)
        }
    }

    func markDone(id: String) async throws {
        var req = Agent_V1_MarkDoneRequest()
        req.id = id
        _ = try await agentClient().markDone(req) { response in
            _ = try response.message
        }
    }

    func search(query: String, limit: Int32 = 50) async throws -> [Artifact_V1_Artifact] {
        var req = Agent_V1_SearchRequest()
        req.query = query
        req.limit = limit
        return try await agentClient().search(req) { response in
            try response.message.artifacts
        }
    }

    func subscribe() async throws -> AsyncStream<Agent_V1_ArtifactEvent> {
        let c = try agentClient()
        let (stream, continuation) = AsyncStream<Agent_V1_ArtifactEvent>.makeStream()
        Task {
            do {
                try await c.subscribe(Agent_V1_SubscribeRequest()) { response in
                    for try await event in response.messages {
                        continuation.yield(event)
                    }
                }
            } catch {
                // stream ended or errored
            }
            continuation.finish()
        }
        return stream
    }

    func getStatus() async throws -> Agent_V1_GetStatusResponse {
        return try await agentClient().getStatus(Agent_V1_GetStatusRequest()) { response in
            try response.message
        }
    }
}

enum AgentError: Error {
    case notConnected
}
