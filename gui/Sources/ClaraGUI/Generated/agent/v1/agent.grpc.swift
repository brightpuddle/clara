// DO NOT EDIT.
// swift-format-ignore-file
// swiftlint:disable all
//
// Hand-written gRPC Swift client stubs for agent/v1/agent.proto
// Source: agent/v1/agent.proto

import GRPCCore
import GRPCProtobuf

// MARK: - agent.v1.AgentService

@available(macOS 15.0, iOS 18.0, watchOS 11.0, tvOS 18.0, visionOS 2.0, *)
internal enum Agent_V1_AgentService: Sendable {
    internal static let descriptor = GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService")

    internal enum Method: Sendable {
        internal enum ListArtifacts: Sendable {
            internal typealias Input = Agent_V1_ListArtifactsRequest
            internal typealias Output = Agent_V1_ListArtifactsResponse
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "ListArtifacts"
            )
        }
        internal enum GetArtifact: Sendable {
            internal typealias Input = Agent_V1_GetArtifactRequest
            internal typealias Output = Agent_V1_GetArtifactResponse
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "GetArtifact"
            )
        }
        internal enum MarkDone: Sendable {
            internal typealias Input = Agent_V1_MarkDoneRequest
            internal typealias Output = Agent_V1_MarkDoneResponse
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "MarkDone"
            )
        }
        internal enum Search: Sendable {
            internal typealias Input = Agent_V1_SearchRequest
            internal typealias Output = Agent_V1_SearchResponse
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "Search"
            )
        }
        internal enum Subscribe: Sendable {
            internal typealias Input = Agent_V1_SubscribeRequest
            internal typealias Output = Agent_V1_ArtifactEvent
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "Subscribe"
            )
        }
        internal enum GetSystemTheme: Sendable {
            internal typealias Input = Agent_V1_GetSystemThemeRequest
            internal typealias Output = Agent_V1_GetSystemThemeResponse
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "GetSystemTheme"
            )
        }
        internal enum GetStatus: Sendable {
            internal typealias Input = Agent_V1_GetStatusRequest
            internal typealias Output = Agent_V1_GetStatusResponse
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "GetStatus"
            )
        }
        internal enum UpdateReminder: Sendable {
            internal typealias Input = Agent_V1_UpdateReminderRequest
            internal typealias Output = Agent_V1_UpdateReminderResponse
            internal static let descriptor = GRPCCore.MethodDescriptor(
                service: GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService"),
                method: "UpdateReminder"
            )
        }
        internal static let descriptors: [GRPCCore.MethodDescriptor] = [
            ListArtifacts.descriptor,
            GetArtifact.descriptor,
            MarkDone.descriptor,
            Search.descriptor,
            Subscribe.descriptor,
            GetSystemTheme.descriptor,
            GetStatus.descriptor,
            UpdateReminder.descriptor,
        ]
    }
}

@available(macOS 15.0, iOS 18.0, watchOS 11.0, tvOS 18.0, visionOS 2.0, *)
extension GRPCCore.ServiceDescriptor {
    internal static let agent_v1_AgentService = GRPCCore.ServiceDescriptor(fullyQualifiedService: "agent.v1.AgentService")
}

// MARK: - agent.v1.AgentService (client)

@available(macOS 15.0, iOS 18.0, watchOS 11.0, tvOS 18.0, visionOS 2.0, *)
extension Agent_V1_AgentService {
    internal protocol ClientProtocol: Sendable {
        func listArtifacts<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_ListArtifactsRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_ListArtifactsRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_ListArtifactsResponse>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_ListArtifactsResponse>) async throws -> Result
        ) async throws -> Result where Result: Sendable

        func getArtifact<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_GetArtifactRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_GetArtifactRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_GetArtifactResponse>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetArtifactResponse>) async throws -> Result
        ) async throws -> Result where Result: Sendable

        func markDone<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_MarkDoneRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_MarkDoneRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_MarkDoneResponse>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_MarkDoneResponse>) async throws -> Result
        ) async throws -> Result where Result: Sendable

        func search<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_SearchRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_SearchRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_SearchResponse>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_SearchResponse>) async throws -> Result
        ) async throws -> Result where Result: Sendable

        func subscribe<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_SubscribeRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_SubscribeRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_ArtifactEvent>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.StreamingClientResponse<Agent_V1_ArtifactEvent>) async throws -> Result
        ) async throws -> Result where Result: Sendable

        func getSystemTheme<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_GetSystemThemeRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_GetSystemThemeRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_GetSystemThemeResponse>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetSystemThemeResponse>) async throws -> Result
        ) async throws -> Result where Result: Sendable

        func getStatus<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_GetStatusRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_GetStatusRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_GetStatusResponse>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetStatusResponse>) async throws -> Result
        ) async throws -> Result where Result: Sendable

        func updateReminder<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_UpdateReminderRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_UpdateReminderRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_UpdateReminderResponse>,
            options: GRPCCore.CallOptions,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_UpdateReminderResponse>) async throws -> Result
        ) async throws -> Result where Result: Sendable
    }

    internal struct Client<Transport>: ClientProtocol where Transport: GRPCCore.ClientTransport {
        private let client: GRPCCore.GRPCClient<Transport>

        internal init(wrapping client: GRPCCore.GRPCClient<Transport>) {
            self.client = client
        }

        internal func listArtifacts<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_ListArtifactsRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_ListArtifactsRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_ListArtifactsResponse>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_ListArtifactsResponse>) async throws -> Result = { response in
                try response.message
            }
        ) async throws -> Result where Result: Sendable {
            try await self.client.unary(
                request: request,
                descriptor: Agent_V1_AgentService.Method.ListArtifacts.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }

        internal func getArtifact<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_GetArtifactRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_GetArtifactRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_GetArtifactResponse>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetArtifactResponse>) async throws -> Result = { response in
                try response.message
            }
        ) async throws -> Result where Result: Sendable {
            try await self.client.unary(
                request: request,
                descriptor: Agent_V1_AgentService.Method.GetArtifact.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }

        internal func markDone<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_MarkDoneRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_MarkDoneRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_MarkDoneResponse>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_MarkDoneResponse>) async throws -> Result = { response in
                try response.message
            }
        ) async throws -> Result where Result: Sendable {
            try await self.client.unary(
                request: request,
                descriptor: Agent_V1_AgentService.Method.MarkDone.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }

        internal func search<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_SearchRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_SearchRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_SearchResponse>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_SearchResponse>) async throws -> Result = { response in
                try response.message
            }
        ) async throws -> Result where Result: Sendable {
            try await self.client.unary(
                request: request,
                descriptor: Agent_V1_AgentService.Method.Search.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }

        internal func subscribe<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_SubscribeRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_SubscribeRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_ArtifactEvent>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.StreamingClientResponse<Agent_V1_ArtifactEvent>) async throws -> Result
        ) async throws -> Result where Result: Sendable {
            try await self.client.serverStreaming(
                request: request,
                descriptor: Agent_V1_AgentService.Method.Subscribe.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }

        internal func getSystemTheme<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_GetSystemThemeRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_GetSystemThemeRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_GetSystemThemeResponse>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetSystemThemeResponse>) async throws -> Result = { response in
                try response.message
            }
        ) async throws -> Result where Result: Sendable {
            try await self.client.unary(
                request: request,
                descriptor: Agent_V1_AgentService.Method.GetSystemTheme.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }

        internal func getStatus<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_GetStatusRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_GetStatusRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_GetStatusResponse>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetStatusResponse>) async throws -> Result = { response in
                try response.message
            }
        ) async throws -> Result where Result: Sendable {
            try await self.client.unary(
                request: request,
                descriptor: Agent_V1_AgentService.Method.GetStatus.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }

        internal func updateReminder<Result>(
            request: GRPCCore.ClientRequest<Agent_V1_UpdateReminderRequest>,
            serializer: some GRPCCore.MessageSerializer<Agent_V1_UpdateReminderRequest>,
            deserializer: some GRPCCore.MessageDeserializer<Agent_V1_UpdateReminderResponse>,
            options: GRPCCore.CallOptions = .defaults,
            onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_UpdateReminderResponse>) async throws -> Result = { response in
                try response.message
            }
        ) async throws -> Result where Result: Sendable {
            try await self.client.unary(
                request: request,
                descriptor: Agent_V1_AgentService.Method.UpdateReminder.descriptor,
                serializer: serializer,
                deserializer: deserializer,
                options: options,
                onResponse: handleResponse
            )
        }
    }
}

// MARK: - Helpers providing default arguments (serializers)

@available(macOS 15.0, iOS 18.0, watchOS 11.0, tvOS 18.0, visionOS 2.0, *)
extension Agent_V1_AgentService.ClientProtocol {
    internal func listArtifacts<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_ListArtifactsRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_ListArtifactsResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        try await self.listArtifacts(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_ListArtifactsRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_ListArtifactsResponse>(),
            options: options,
            onResponse: handleResponse
        )
    }

    internal func getArtifact<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_GetArtifactRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetArtifactResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        try await self.getArtifact(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_GetArtifactRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_GetArtifactResponse>(),
            options: options,
            onResponse: handleResponse
        )
    }

    internal func markDone<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_MarkDoneRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_MarkDoneResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        try await self.markDone(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_MarkDoneRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_MarkDoneResponse>(),
            options: options,
            onResponse: handleResponse
        )
    }

    internal func search<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_SearchRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_SearchResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        try await self.search(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_SearchRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_SearchResponse>(),
            options: options,
            onResponse: handleResponse
        )
    }

    internal func subscribe<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_SubscribeRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.StreamingClientResponse<Agent_V1_ArtifactEvent>) async throws -> Result
    ) async throws -> Result where Result: Sendable {
        try await self.subscribe(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_SubscribeRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_ArtifactEvent>(),
            options: options,
            onResponse: handleResponse
        )
    }

    internal func getSystemTheme<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_GetSystemThemeRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetSystemThemeResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        try await self.getSystemTheme(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_GetSystemThemeRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_GetSystemThemeResponse>(),
            options: options,
            onResponse: handleResponse
        )
    }

    internal func getStatus<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_GetStatusRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetStatusResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        try await self.getStatus(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_GetStatusRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_GetStatusResponse>(),
            options: options,
            onResponse: handleResponse
        )
    }

    internal func updateReminder<Result>(
        request: GRPCCore.ClientRequest<Agent_V1_UpdateReminderRequest>,
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_UpdateReminderResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        try await self.updateReminder(
            request: request,
            serializer: GRPCProtobuf.ProtobufSerializer<Agent_V1_UpdateReminderRequest>(),
            deserializer: GRPCProtobuf.ProtobufDeserializer<Agent_V1_UpdateReminderResponse>(),
            options: options,
            onResponse: handleResponse
        )
    }
}

// MARK: - Sugared API helpers

@available(macOS 15.0, iOS 18.0, watchOS 11.0, tvOS 18.0, visionOS 2.0, *)
extension Agent_V1_AgentService.ClientProtocol {
    internal func listArtifacts<Result>(
        _ message: Agent_V1_ListArtifactsRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_ListArtifactsResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_ListArtifactsRequest>(message: message, metadata: metadata)
        return try await self.listArtifacts(request: request, options: options, onResponse: handleResponse)
    }

    internal func getArtifact<Result>(
        _ message: Agent_V1_GetArtifactRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetArtifactResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_GetArtifactRequest>(message: message, metadata: metadata)
        return try await self.getArtifact(request: request, options: options, onResponse: handleResponse)
    }

    internal func markDone<Result>(
        _ message: Agent_V1_MarkDoneRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_MarkDoneResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_MarkDoneRequest>(message: message, metadata: metadata)
        return try await self.markDone(request: request, options: options, onResponse: handleResponse)
    }

    internal func search<Result>(
        _ message: Agent_V1_SearchRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_SearchResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_SearchRequest>(message: message, metadata: metadata)
        return try await self.search(request: request, options: options, onResponse: handleResponse)
    }

    internal func subscribe<Result>(
        _ message: Agent_V1_SubscribeRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.StreamingClientResponse<Agent_V1_ArtifactEvent>) async throws -> Result
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_SubscribeRequest>(message: message, metadata: metadata)
        return try await self.subscribe(request: request, options: options, onResponse: handleResponse)
    }

    internal func getSystemTheme<Result>(
        _ message: Agent_V1_GetSystemThemeRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetSystemThemeResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_GetSystemThemeRequest>(message: message, metadata: metadata)
        return try await self.getSystemTheme(request: request, options: options, onResponse: handleResponse)
    }

    internal func getStatus<Result>(
        _ message: Agent_V1_GetStatusRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_GetStatusResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_GetStatusRequest>(message: message, metadata: metadata)
        return try await self.getStatus(request: request, options: options, onResponse: handleResponse)
    }

    internal func updateReminder<Result>(
        _ message: Agent_V1_UpdateReminderRequest,
        metadata: GRPCCore.Metadata = [:],
        options: GRPCCore.CallOptions = .defaults,
        onResponse handleResponse: @Sendable @escaping (GRPCCore.ClientResponse<Agent_V1_UpdateReminderResponse>) async throws -> Result = { response in
            try response.message
        }
    ) async throws -> Result where Result: Sendable {
        let request = GRPCCore.ClientRequest<Agent_V1_UpdateReminderRequest>(message: message, metadata: metadata)
        return try await self.updateReminder(request: request, options: options, onResponse: handleResponse)
    }
}
