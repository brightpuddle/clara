// ClaraBridge — Swift native macOS bridge for the Clara daemon.
//
// This process listens on a Unix Domain Socket and implements the BridgeService
// gRPC server, providing access to native macOS APIs (EventKit, CoreSpotlight,
// FileSystem events) from the Go Clara daemon.
//
// Usage: ClaraBridge --socket /path/to/bridge.sock

import EventKit
import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2
import Proto

// MARK: - Entry point

@main
struct ClaraBridgeMain {
    static func main() async throws {
        let args = CommandLine.arguments
        var socketPath: String = defaultSocketPath()

        // Simple argument parsing: --socket <path>
        var i = 1
        while i < args.count {
            if args[i] == "--socket", i + 1 < args.count {
                socketPath = args[i + 1]
                i += 2
            } else {
                i += 1
            }
        }

        print("ClaraBridge starting on \(socketPath)")
        try await runServer(socketPath: socketPath)
    }

    static func defaultSocketPath() -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return "\(home)/.local/share/clara/bridge.sock"
    }
}

// MARK: - gRPC server

func runServer(socketPath: String) async throws {
    // Remove stale socket.
    try? FileManager.default.removeItem(atPath: socketPath)

    let server = GRPCServer(
        transport: .http2NIOPosix(
            address: .unixDomainSocket(path: socketPath),
            transportSecurity: .plaintext
        ),
        services: [BridgeServiceImpl()]
    )

    try await withThrowingTaskGroup(of: Void.self) { group in
        group.addTask { try await server.serve() }
        print("ClaraBridge listening on \(socketPath)")
        try await group.next()
    }
}

// MARK: - BridgeService implementation

actor BridgeServiceImpl: Clara_Bridge_V1_BridgeService.SimpleServiceProtocol {
    private let eventStore = EKEventStore()
    private var authorized = false

    // Ping — health check.
    func ping(
        request: Clara_Bridge_V1_PingRequest,
        context: ServerContext
    ) async throws -> Clara_Bridge_V1_PingResponse {
        return Clara_Bridge_V1_PingResponse.with { $0.version = "0.1.0" }
    }

    // CallTool — dispatch to the correct native capability.
    func callTool(
        request: Clara_Bridge_V1_CallToolRequest,
        context: ServerContext
    ) async throws -> Clara_Bridge_V1_CallToolResponse {
        do {
            let args = try parseArgs(request.argsJson)
            let result: Any

            switch request.name {
            case "fetch_reminders":
                result = try await fetchReminders(args: args)
            default:
                throw BridgeError.unknownTool(request.name)
            }

            let jsonData = try JSONSerialization.data(withJSONObject: result)
            let resultJSON = String(data: jsonData, encoding: .utf8) ?? "null"
            return Clara_Bridge_V1_CallToolResponse.with { $0.resultJson = resultJSON }
        } catch {
            return Clara_Bridge_V1_CallToolResponse.with { $0.error = error.localizedDescription }
        }
    }

    // MARK: - Native tools

    /// fetch_reminders — returns incomplete reminders from the default list.
    /// Supported args: list_name (optional string)
    private func fetchReminders(args: [String: Any]) async throws -> [[String: String]] {
        try await requestRemindersAccess()

        let calendars: [EKCalendar]?
        if let listName = args["list_name"] as? String {
            calendars = eventStore.calendars(for: .reminder)
                .filter { $0.title == listName }
        } else {
            calendars = nil
        }

        let predicate = eventStore.predicateForReminders(in: calendars)
        let reminders: [[String: String]] = try await withCheckedThrowingContinuation { cont in
            eventStore.fetchReminders(matching: predicate) { fetched in
                let mapped = (fetched ?? [])
                    .filter { !$0.isCompleted }
                    .map { r -> [String: String] in
                        var item: [String: String] = ["title": r.title ?? ""]
                        if let due = r.dueDateComponents?.date {
                            item["due"] = ISO8601DateFormatter().string(from: due)
                        }
                        if let notes = r.notes {
                            item["notes"] = notes
                        }
                        item["list"] = r.calendar.title
                        return item
                    }
                cont.resume(returning: mapped)
            }
        }

        return reminders
    }

    private func requestRemindersAccess() async throws {
        guard !authorized else { return }
        let granted = try await eventStore.requestFullAccessToReminders()
        guard granted else {
            throw BridgeError.accessDenied("Reminders")
        }
        authorized = true
    }
}

// MARK: - Helpers

enum BridgeError: LocalizedError {
    case unknownTool(String)
    case accessDenied(String)
    case invalidArgs(String)

    var errorDescription: String? {
        switch self {
        case .unknownTool(let name): return "unknown tool: \(name)"
        case .accessDenied(let resource): return "access denied to \(resource)"
        case .invalidArgs(let msg): return "invalid args: \(msg)"
        }
    }
}

func parseArgs(_ json: String) throws -> [String: Any] {
    guard !json.isEmpty else { return [:] }
    let data = Data(json.utf8)
    let obj = try JSONSerialization.jsonObject(with: data)
    guard let dict = obj as? [String: Any] else {
        throw BridgeError.invalidArgs("args must be a JSON object")
    }
    return dict
}
