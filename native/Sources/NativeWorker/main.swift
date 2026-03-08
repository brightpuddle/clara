import GRPCCore
import GRPCNIOTransportHTTP2
import Foundation
import OSLog

// Simple logger wrapper using OSLog
enum Logger {
    private static let subsystem = "com.brightpuddle.clara.native"
    private static let logger = os.Logger(subsystem: subsystem, category: "NativeWorker")

    static func info(_ message: String) {
        logger.info("\(message)")
    }

    static func error(_ message: String) {
        logger.error("\(message)")
    }

    static func debug(_ message: String) {
        logger.debug("\(message)")
    }
}

@available(macOS 15.0, *)
func runServer() async throws {
    // Request EventKit access early.
    let remindersManager = RemindersManager()
    do {
        try await remindersManager.requestAccess()
        Logger.info("Reminders access granted")
    } catch {
        Logger.error("Reminders access denied: \(error.localizedDescription) — reminders will return empty results")
    }

    let spotlightManager = SpotlightManager()
    let service = NativeWorkerServiceImpl(reminders: remindersManager, spotlight: spotlightManager)

    let socketPath = nativeSocketPath()

    // Remove stale socket.
    try? FileManager.default.removeItem(atPath: socketPath)

    let transport = try HTTP2ServerTransport.Posix(
        address: .unixDomainSocket(path: socketPath),
        transportSecurity: .plaintext
    )

    Logger.info("Clara native worker listening on \(socketPath)")
    let server = GRPCServer(transport: transport, services: [service])
    try await server.serve()
}

func nativeSocketPath() -> String {
    let home = FileManager.default.homeDirectoryForCurrentUser.path
    let dir = "\(home)/.local/share/clara"
    try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
    return "\(dir)/native.sock"
}

if #available(macOS 15.0, *) {
    do {
        try await runServer()
    } catch {
        Logger.error("Server error: \(error.localizedDescription)")
        exit(1)
    }
} else {
    Logger.error("Clara native worker requires macOS 15.0 or later")
    exit(1)
}
