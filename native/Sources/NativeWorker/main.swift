import GRPCCore
import GRPCNIOTransportHTTP2
import Foundation

@available(macOS 15.0, *)
func runServer() async throws {
    // Request EventKit access early.
    let remindersManager = RemindersManager()
    do {
        try await remindersManager.requestAccess()
        print("Reminders access granted")
    } catch {
        print("Reminders access denied: \(error) — reminders will return empty results")
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

    print("Clara native worker listening on \(socketPath)")
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
    try await runServer()
} else {
    print("Clara native worker requires macOS 15.0 or later")
    exit(1)
}
