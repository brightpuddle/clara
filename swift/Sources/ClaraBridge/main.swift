// ClaraBridge — Swift native macOS gRPC plugin for Clara.

import AppKit
@preconcurrency import EventKit
import Foundation
import GRPC
import NIO
import NIOHTTP1
@preconcurrency import Photos
import SwiftProtobuf
@preconcurrency import UserNotifications

// --- Main Entry Point ---

@MainActor
func main() {
    let env = ProcessInfo.processInfo.environment
    guard env["CLARA_PLUGIN_MAGIC_COOKIE"] == "hello_clara" else {
        FileHandle.standardError.write(Data("Missing or invalid CLARA_PLUGIN_MAGIC_COOKIE\n".utf8))
        exit(1)
    }

    let app = NSApplication.shared
    let delegate = AppDelegate()
    app.delegate = delegate
    app.setActivationPolicy(.accessory)
    app.run()
}

main()

// --- Implementation ---

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var server: GRPC.Server?
    private var group: MultiThreadedEventLoopGroup?
    private let logger = ClaraLogger()

    func applicationDidFinishLaunching(_: Notification) {
        startGRPCServer()
    }

    private func startGRPCServer() {
        let group = MultiThreadedEventLoopGroup(numberOfThreads: 1)
        self.group = group

        let eventManager = EventManager()
        let bridgeTools = BridgeTools(eventManager: eventManager)
        let provider = ClaraBridgeGRPCProvider(bridgeTools: bridgeTools, eventManager: eventManager)

        do {
            let server = try Server.start(
                configuration: .default(
                    target: .hostAndPort("127.0.0.1", 0),
                    eventLoopGroup: group,
                    serviceProviders: [provider]
                )
            ).wait()
            
            self.server = server
            guard let port = server.channel.localAddress?.port else {
                self.logger.log("Failed to determine local port")
                exit(1)
            }

            print("1|1|tcp|127.0.0.1:\(port)|grpc")
            fflush(stdout)
            
            self.logger.log("ClaraBridge gRPC server listening on 127.0.0.1:\(port)")
        } catch {
            self.logger.log("Failed to start gRPC server: \(error)")
            exit(1)
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        try? server?.close().wait()
        try? group?.syncShutdownGracefully()
    }
}

final class EventManager: @unchecked Sendable {
    private let lock = NSLock()
    private var streams: [UUID: GRPCAsyncResponseStreamWriter<Clara_Plugin_V1_Event>] = [:]

    func addStream(_ writer: GRPCAsyncResponseStreamWriter<Clara_Plugin_V1_Event>) -> UUID {
        let id = UUID()
        lock.lock()
        defer { lock.unlock() }
        streams[id] = writer
        return id
    }

    func removeStream(id: UUID) {
        lock.lock()
        defer { lock.unlock() }
        streams.removeValue(forKey: id)
    }

    func emit(name: String, data: [String: Any]) {
        lock.lock()
        let currentStreams = Array(streams.values)
        lock.unlock()

        var ev = Clara_Plugin_V1_Event()
        ev.name = name
        if let jsonData = try? JSONSerialization.data(withJSONObject: data) {
            ev.data = jsonData
        }
        
        for stream in currentStreams {
            // Fire and forget since we can't await here
            Task {
                try? await stream.send(ev)
            }
        }
    }
}

final class ClaraBridgeGRPCProvider: Clara_Plugin_V1_IntegrationAsyncProvider {
    let bridgeTools: BridgeTools
    let eventManager: EventManager
    
    init(bridgeTools: BridgeTools, eventManager: EventManager) {
        self.bridgeTools = bridgeTools
        self.eventManager = eventManager
    }
    
    func configure(request: Clara_Plugin_V1_ConfigureRequest, context: GRPCAsyncServerCallContext) async throws -> Clara_Plugin_V1_ConfigureResponse {
        return Clara_Plugin_V1_ConfigureResponse()
    }
    
    func description(request: Clara_Plugin_V1_DescriptionRequest, context: GRPCAsyncServerCallContext) async throws -> Clara_Plugin_V1_DescriptionResponse {
        var resp = Clara_Plugin_V1_DescriptionResponse()
                resp.description_p = "Native macOS integration for reminders, calendar, and notifications."
        return resp
    }
    
    func tools(request: Clara_Plugin_V1_ToolsRequest, context: GRPCAsyncServerCallContext) async throws -> Clara_Plugin_V1_ToolsResponse {
        var resp = Clara_Plugin_V1_ToolsResponse()
        resp.tools = try await bridgeTools.listToolsSerialized()
        return resp
    }

    func callTool(request: Clara_Plugin_V1_CallToolRequest, context: GRPCAsyncServerCallContext) async throws -> Clara_Plugin_V1_CallToolResponse {
        let data = try await self.bridgeTools.callToolSerialized(name: request.name, argumentsData: request.args)
        var resp = Clara_Plugin_V1_CallToolResponse()
        resp.result = data
        return resp
    }

    func streamEvents(request: Clara_Plugin_V1_StreamEventsRequest, responseStream: GRPCAsyncResponseStreamWriter<Clara_Plugin_V1_Event>, context: GRPCAsyncServerCallContext) async throws {
        let id = eventManager.addStream(responseStream)
        defer { eventManager.removeStream(id: id) }

        while !Task.isCancelled {
            try await Task.sleep(nanoseconds: 1_000_000_000)
        }
    }
}

final class ClaraLogger: Sendable {
    private let logFile: URL?
    private let fileHandle: FileHandle?
    private let queue = DispatchQueue(label: "com.brightpuddle.clara.logger", qos: .background)

    init() {
        let env = ProcessInfo.processInfo.environment
        if let path = env["CLARA_PLUGIN_LOG_FILE"] {
            let url = URL(fileURLWithPath: path)
            self.logFile = url
            if !FileManager.default.fileExists(atPath: url.path) {
                FileManager.default.createFile(atPath: url.path, contents: nil)
            }
            self.fileHandle = try? FileHandle(forWritingTo: url)
            self.fileHandle?.seekToEndOfFile()
        } else {
            self.logFile = nil
            self.fileHandle = nil
        }
    }

    func log(_ message: String) {
        let timestamp = ISO8601.dateString(from: Date())
        let line = "[\(timestamp)] \(message)\n"
        guard let data = line.data(using: .utf8) else { return }

        queue.async { [weak self] in
            self?.fileHandle?.write(data)
            self?.fileHandle?.synchronizeFile()
        }
    }
}

@MainActor
final class BridgeTools: NSObject, UNUserNotificationCenterDelegate, @unchecked Sendable {
    @MainActor
    func listToolsSerialized() throws -> Data {
        return try JSONSerialization.data(withJSONObject: listTools())
    }

    func callToolSerialized(name: String, argumentsData: Data) async throws -> Data {
        let args = try JSONSerialization.jsonObject(with: argumentsData) as? [String: Any] ?? [:]
        let result = try await callTool(name: name, arguments: args)
        return try JSONSerialization.data(withJSONObject: result)
    }



    private let eventStore = EKEventStore()
    private var remindersAuthorized = false
    private var eventsAuthorized = false
    private var photosAuthorized = false
    private var notificationsAuthorized = false
    private var eventStoreObserver: NSObjectProtocol?
    private var themeObserver: NSObjectProtocol?
    private var pendingInteractiveNotifications: [String: PendingInteractiveNotification] = [:]
    private var notificationCategories: [String: UNNotificationCategory] = [:]
    private var eventStoreChangeTask: Task<Void, Never>?

    // Proactive push baselines — nil until first successful snapshot.
    private var reminderPushBaseline: [String: StoreSnapshotItem]?
    private var eventPushBaseline: [String: StoreSnapshotItem]?


    private let eventManager: EventManager
    
    init(eventManager: EventManager) {
        self.eventManager = eventManager
        super.init()
        eventStoreObserver = NotificationCenter.default.addObserver(
            forName: .EKEventStoreChanged,
            object: eventStore,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                await self?.handleEventStoreChanged()
            }
        }
        themeObserver = DistributedNotificationCenter.default().addObserver(
            forName: NSNotification.Name("AppleInterfaceThemeChangedNotification"),
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.handleThemeChange()
            }
        }
        // Eagerly request EventKit access so that proactive push notifications
        // work immediately — even when no tool has been called yet.
        Task { @MainActor [weak self] in
            await self?.requestAccessEagerly()
        }

    }

    @MainActor
    private func requestAccessEagerly() async {
        try? await ensureRemindersAccess()
        try? await ensureEventsAccess()
        try? await ensureNotificationsAccess()
        try? await ensurePhotosAccess()
    }

    @MainActor
    func listTools() -> [[String: Any]] {
        [
            tool(
                name: "clara_list_events",
                description: "List the available event triggers emitted by this server for use in Starlark 'task(on_event, trigger=...)' declarations.",
                properties: []
            ),
            tool(
                name: "reminders_create",
                description: "Create a reminder with EventKit fields.",
                properties: [
                    stringProperty("title", "Reminder title."),
                    stringProperty("notes", "Optional notes."),
                    stringProperty("due_date", "Optional due date in ISO-8601 format."),
                    boolProperty("completed", "Optional completion status."),
                    numberProperty("priority", "Optional EventKit priority."),
                    stringProperty("list_name", "Optional reminder list name."),
                    stringProperty("location", "Optional structured location title."),
                    alarmsProperty(),
                ],
                required: ["title"]
            ),
            tool(
                name: "reminders_get",
                description: "Fetch a reminder by identifier.",
                properties: [stringProperty("identifier", "Reminder identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "reminders_update",
                description: "Update an existing reminder.",
                properties: [
                    stringProperty("identifier", "Reminder identifier."),
                    stringProperty("title", "Updated title."),
                    stringProperty("notes", "Updated notes."),
                    stringProperty("due_date", "Updated due date."),
                    boolProperty("completed", "Updated completion status."),
                    numberProperty("priority", "Updated EventKit priority."),
                    stringProperty("list_name", "Updated reminder list name."),
                    stringProperty("location", "Updated structured location title."),
                    alarmsProperty(),
                ],
                required: ["identifier"]
            ),
            tool(
                name: "reminders_delete",
                description: "Delete a reminder.",
                properties: [stringProperty("identifier", "Reminder identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "reminders_list",
                description: "List reminders with filters.",
                properties: [
                    stringProperty("list_name", "Optional list name filter."),
                    boolProperty("completed", "Optional completion filter."),
                    stringProperty("start_date", "Optional start due date."),
                    stringProperty("end_date", "Optional end due date."),
                    stringProperty("updated_after", "Optional updated_at filter."),
                ]
            ),
            tool(
                name: "reminders_default_list",
                description: "Return the default Reminder list name.",
                properties: []
            ),
            tool(
                name: "events_create",
                description: "Create a calendar event.",
                properties: [
                    stringProperty("title", "Event title."),
                    stringProperty("notes", "Optional notes."),
                    stringProperty("start_date", "Start date."),
                    stringProperty("end_date", "End date."),
                    boolProperty("all_day", "Whether the event is all-day."),
                    stringProperty("calendar_name", "Optional calendar name."),
                    stringProperty("location", "Optional location."),
                    alarmsProperty(),
                ],
                required: ["title", "start_date", "end_date"]
            ),
            tool(
                name: "events_get",
                description: "Fetch a calendar event by identifier.",
                properties: [stringProperty("identifier", "Event identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "events_update",
                description: "Update a calendar event.",
                properties: [
                    stringProperty("identifier", "Event identifier."),
                    stringProperty("title", "Updated title."),
                    stringProperty("notes", "Updated notes."),
                    stringProperty("start_date", "Updated start date."),
                    stringProperty("end_date", "Updated end date."),
                    boolProperty("all_day", "Updated all-day flag."),
                    stringProperty("calendar_name", "Updated calendar name."),
                    stringProperty("location", "Updated location."),
                    alarmsProperty(),
                ],
                required: ["identifier"]
            ),
            tool(
                name: "events_delete",
                description: "Delete a calendar event.",
                properties: [stringProperty("identifier", "Event identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "events_list",
                description: "List calendar events with filters.",
                properties: [
                    stringProperty("calendar_name", "Optional calendar name."),
                    stringProperty("start_date", "Optional start date."),
                    stringProperty("end_date", "Optional end date."),
                ]
            ),
            tool(
                name: "notify_send",
                description: "Send a standard macOS user notification banner.",
                properties: [
                    stringProperty("title", "Notification title."),
                    stringProperty("body", "Notification body."),
                    stringProperty("subtitle", "Optional subtitle."),
                    stringProperty("url", "Optional URL to open on click."),
                ],
                required: ["title", "body"]
            ),
            tool(
                name: "notify_send_interactive",
                description: "Send an interactive macOS notification.",
                properties: [
                    stringProperty("title", "Notification title."),
                    stringProperty("body", "Notification body."),
                    stringProperty("subtitle", "Optional subtitle."),
                    stringProperty("url", "Optional URL to open on click."),
                    actionsProperty(),
                    boolProperty("wait_for_response", "Block until response."),
                    numberProperty("timeout_seconds", "Response timeout."),
                    stringProperty("notification_id", "Optional identifier."),
                ],
                required: ["title", "body", "actions"]
            ),
            tool(
                name: "photos_album_assets",
                description: "List photo assets in an album.",
                properties: [
                    stringProperty("album_name", "Album name."),
                    numberProperty("limit", "Optional limit."),
                ],
                required: ["album_name"]
            ),
            tool(
                name: "photos_export_assets",
                description: "Export photo assets to a directory.",
                properties: [
                    stringArrayProperty("asset_ids", "Array of asset IDs."),
                    stringProperty("destination_dir", "Destination directory."),
                ],
                required: ["asset_ids", "destination_dir"]
            ),
            tool(
                name: "photos_album_remove_assets",
                description: "Remove assets from an album.",
                properties: [
                    stringProperty("album_name", "Album name."),
                    stringArrayProperty("asset_ids", "Array of asset IDs."),
                ],
                required: ["album_name", "asset_ids"]
            ),
            tool(
                name: "photos_album_add_assets",
                description: "Add assets to an album.",
                properties: [
                    stringProperty("album_name", "Album name."),
                    stringArrayProperty("asset_ids", "Array of asset IDs."),
                ],
                required: ["album_name", "asset_ids"]
            ),
            tool(
                name: "theme_get",
                description: "Return the current macOS interface theme (dark or light).",
                properties: []
            ),
            tool(
                name: "mail_list_inbox",
                description: "List recent messages in the Mail.app Inbox.",
                properties: [
                    numberProperty("limit", "Maximum number of messages to return (default: 10)."),
                    stringProperty("account_name", "Optional account name filter."),
                    boolProperty("unread", "If true, only return unread messages.")
                ]
            ),
            tool(
                name: "mail_get_message",
                description: "Fetch full content and metadata for a specific Mail message.",
                properties: [
                    stringProperty("message_id", "The unique message ID.")
                ],
                required: ["message_id"]
            ),
            tool(
                name: "mail_move",
                description: "Move a message to a specific mailbox.",
                properties: [
                    stringProperty("message_id", "The unique message ID."),
                    stringProperty("target_mailbox", "Name of the target mailbox."),
                    stringProperty("account_name", "Account containing the target mailbox.")
                ],
                required: ["message_id", "target_mailbox", "account_name"]
            ),
            tool(
                name: "mail_flag",
                description: "Set or clear the flagged status of a message.",
                properties: [
                    stringProperty("message_id", "The unique message ID."),
                    boolProperty("flagged", "Whether the message should be flagged.")
                ],
                required: ["message_id", "flagged"]
            ),
            tool(
                name: "mail_mark_read",
                description: "Mark a message as read or unread.",
                properties: [
                    stringProperty("message_id", "The unique message ID."),
                    boolProperty("read", "Whether the message should be marked as read.")
                ],
                required: ["message_id", "read"]
            ),
            tool(
                name: "mail_create_draft",
                description: "Create a new draft message in Mail.app.",
                properties: [
                    stringProperty("subject", "Email subject."),
                    stringProperty("content", "Email body content."),
                    stringProperty("to", "Recipient email address."),
                    stringProperty("account_name", "Account to create the draft in.")
                ],
                required: ["subject", "content", "to"]
            ),
            tool(
                name: "mail_send",
                description: "Send an email message immediately via Mail.app.",
                properties: [
                    stringProperty("subject", "Email subject."),
                    stringProperty("content", "Email body content."),
                    stringProperty("to", "Recipient email address."),
                    stringProperty("account_name", "Account to send from.")
                ],
                required: ["subject", "content", "to"]
            ),
            tool(
                name: "mail_delete",
                description: "Move a message to the Trash mailbox.",
                properties: [
                    stringProperty("message_id", "The unique message ID.")
                ],
                required: ["message_id"]
            ),
            tool(
                name: "mail_get_mailboxes",
                description: "List all available mailboxes for an account.",
                properties: [
                    stringProperty("account_name", "The account name.")
                ],
                required: ["account_name"]
            ),
        ]
    }

    @MainActor
    func callTool(name: String, arguments: [String: Any]) async throws -> Any {
        print("ClaraBridge: calling tool \(name) with args \(arguments)")
        switch name {
        case "clara_list_events":
            return try toolResult([
                [
                    "name": "reminders_on_change",
                    "description": "Emitted when a reminder is created, updated, or deleted.",
                    "params": ["resource": "reminder", "change_type": "create|update|delete", "item": "object"],
                ],
                [
                    "name": "events_on_change",
                    "description": "Emitted when a calendar event is created, updated, or deleted.",
                    "params": ["resource": "event", "change_type": "create|update|delete", "item": "object"],
                ],
                [
                    "name": "events_on_event",
                    "description": "Alias for events_on_change, emitted when a calendar event is created, updated, or deleted.",
                    "params": ["resource": "event", "change_type": "create|update|delete", "item": "object"],
                ],
                [
                    "name": "theme_on_change",
                    "description": "Emitted when the macOS interface theme changes between light and dark.",
                    "params": ["theme": "dark|light"],
                ],
                [
                    "name": "notify_on_response",
                    "description": "Emitted when a user responds to an interactive notification.",
                    "params": ["notification_id": "string", "action_id": "string", "status": "responded"],
                ],
            ])
        case "reminders_create":
            let reminder = try await createReminder(arguments)
            return try toolResult(serializeReminder(reminder))
        case "reminders_get":
            let id = try requiredString(arguments, "identifier")
            let reminder = try await reminder(identifier: id)
            return try toolResult(serializeReminder(reminder))
        case "reminders_update":
            let reminder = try await updateReminder(arguments)
            return try toolResult(serializeReminder(reminder))
        case "reminders_delete":
            try await deleteReminder(arguments)
            return try toolResult(["status": "deleted"])
        case "reminders_list":
            let items = try await listReminders(arguments)
            return try toolResult(items)
        case "reminders_default_list":
            let result = try await defaultReminderList()
            return try toolResult(result)
        case "events_create":
            let event = try await createEvent(arguments)
            return try toolResult(serializeEvent(event))
        case "events_get":
            let id = try requiredString(arguments, "identifier")
            let event = try await event(identifier: id)
            return try toolResult(serializeEvent(event))
        case "events_update":
            let event = try await updateEvent(arguments)
            return try toolResult(serializeEvent(event))
        case "events_delete":
            try await deleteEvent(arguments)
            return try toolResult(["status": "deleted"])
        case "events_list":
            let items = try await listEvents(arguments)
            return try toolResult(items.map(serializeEvent))
        case "notify_send":
            try await sendNotification(arguments)
            return try toolResult(["status": "sent"])
        case "notify_send_interactive":
            try await sendInteractiveNotification(arguments)
            return try toolResult(["status": "sent"])
        case "photos_album_assets":
            let items = try await listPhotoAlbumAssets(arguments)
            return try toolResult(items)
        case "photos_export_assets":
            let items = try await exportPhotoAssets(arguments)
            return try toolResult(items)
        case "photos_album_remove_assets":
            try await removePhotoAlbumAssets(arguments)
            return try toolResult(["status": "removed"])
        case "photos_album_add_assets":
            try await addPhotoAlbumAssets(arguments)
            return try toolResult(["status": "added"])
        case "theme_get":
            return try toolResult(["theme": getTheme()])
        case "theme_get_appearance": // Backward compatibility
            return try toolResult(["theme": getTheme()])
        case "mail_list_inbox":
            let items = try await mailListInbox(arguments)
            return try toolResult(items)
        case "mail_get_message":
            let result = try await mailGetMessage(arguments)
            return try toolResult(result)
        case "mail_move":
            try await mailMove(arguments)
            return try toolResult(["status": "moved"])
        case "mail_flag":
            try await mailFlag(arguments)
            return try toolResult(["status": "flagged"])
        case "mail_mark_read":
            try await mailMarkRead(arguments)
            return try toolResult(["status": "marked"])
        case "mail_create_draft":
            let result = try await mailCreateDraft(arguments)
            return try toolResult(result)
        case "mail_send":
            try await mailSend(arguments)
            return try toolResult(["status": "sent"])
        case "mail_delete":
            try await mailDelete(arguments)
            return try toolResult(["status": "deleted"])
        case "mail_get_mailboxes":
            let items = try await mailGetMailboxes(arguments)
            return try toolResult(items)
        default:
            throw MCPProtocolError.methodNotFound("unknown tool: \(name)")
        }
    }

    nonisolated func userNotificationCenter(
        _: UNUserNotificationCenter,
        willPresent _: UNNotification
    ) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    nonisolated func userNotificationCenter(
        _: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        let identifier = response.notification.request.identifier
        let actionID = BridgeTools.normalizeNotificationAction(response.actionIdentifier)
        Task { @MainActor [weak self] in
            self?.handleNotificationResponse(notificationID: identifier, actionID: actionID)
        }
    }

    @MainActor
    private func handleEventStoreChanged() {
        // Debounce EventKit changes to avoid I/O storms when many items change at once
        eventStoreChangeTask?.cancel()
        eventStoreChangeTask = Task {
            try? await Task.sleep(nanoseconds: 500 * 1_000_000)
            guard !Task.isCancelled else { return }
            await pushReminderNotifications()
            await pushEventNotifications()
        }
    }

    /// Proactively diff the reminder store and emit clara/reminders_changed for
    /// every detected change. On the first call, we just establish the baseline
    /// without emitting — there is nothing to diff against yet.
    @MainActor
    private func pushReminderNotifications() async {
        guard remindersAuthorized else { return }
        do {
            let current = try await reminderSnapshot(listName: nil)
            guard let baseline = reminderPushBaseline else {
                reminderPushBaseline = current
                return
            }
            let changes = diffSnapshots(old: baseline, new: current)
            reminderPushBaseline = current
            for change in changes {
                let payload = waitPayload(
                    resource: "reminder",
                    changeType: change.type,
                    filterKey: "list_name",
                    filterValue: change.item["list_name"] as? String,
                    item: change.item
                )
                eventManager.emit(name: "reminders_on_change", data: payload)
            }
        } catch {
            // Silently ignore snapshot errors — permissions may not be granted yet.
        }
    }

    /// Proactively diff the calendar store and emit clara/events_on_change for
    /// every detected change. Uses a rolling ±1 year window centred on now.
    @MainActor
    private func pushEventNotifications() async {
        guard eventsAuthorized else { return }
        let startDate = Calendar.current.date(byAdding: .year, value: -1, to: Date())!
        let endDate = Calendar.current.date(byAdding: .year, value: 1, to: Date())!
        do {
            let current = try await eventSnapshot(calendarName: nil, startDate: startDate, endDate: endDate)
            guard let baseline = eventPushBaseline else {
                eventPushBaseline = current
                return
            }
            let changes = diffSnapshots(old: baseline, new: current)
            eventPushBaseline = current
            for change in changes {
                let payload = waitPayload(
                    resource: "event",
                    changeType: change.type,
                    filterKey: "calendar_name",
                    filterValue: change.item["calendar_name"] as? String,
                    item: change.item
                )
                eventManager.emit(name: "events_on_change", data: payload)
                eventManager.emit(name: "events_on_event", data: payload)
            }
        } catch {
            // Silently ignore snapshot errors.
        }
    }

    @MainActor
    private func handleNotificationResponse(notificationID: String, actionID: String) {
        let payload: [String: Any] = [
            "notification_id": notificationID,
            "action_id": actionID,
            "status": "responded",
            "responded_at": ISO8601.dateString(from: Date()),
        ]

        if let pending = pendingInteractiveNotifications.removeValue(forKey: notificationID) {
            pending.timeoutTask?.cancel()

            if actionID == "default", let urlString = pending.url, let url = URL(string: urlString) {
                NSWorkspace.shared.open(url)
            }

            if let continuation = pending.continuation {
                continuation.resume(returning: payload)
                return
            }
        }

        eventManager.emit(name: "notify_on_response", data: payload)
    }

    @MainActor
    private func handleThemeChange() {
        let theme = getTheme()
        eventManager.emit(name: "theme_on_change", data: ["theme": theme])
    }

    @MainActor
    private func getTheme() -> String {
        if #available(macOS 10.15, *) {
            let appearance = NSApp.effectiveAppearance
            if appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua {
                return "dark"
            }
        } else {
            let style = UserDefaults.standard.string(forKey: "AppleInterfaceStyle")
            if style == "Dark" {
                return "dark"
            }
        }
        return "light"
    }

    private func createReminder(_ args: [String: Any]) async throws -> EKReminder {
        try await ensureRemindersAccess()
        let reminder = EKReminder(eventStore: eventStore)
        try applyReminderFields(reminder, args: args, requireTitle: true)
        try eventStore.save(reminder, commit: true)
        return reminder
    }

    private func updateReminder(_ args: [String: Any]) async throws -> EKReminder {
        let reminder = try await reminder(identifier: requiredString(args, "identifier"))
        try applyReminderFields(reminder, args: args, requireTitle: false)
        try eventStore.save(reminder, commit: true)
        return reminder
    }

    private func deleteReminder(_ args: [String: Any]) async throws -> [String: Any] {
        let reminder = try await reminder(identifier: requiredString(args, "identifier"))
        try eventStore.remove(reminder, commit: true)
        return ["identifier": reminder.calendarItemIdentifier, "deleted": true]
    }

    private func listReminders(_ args: [String: Any]) async throws -> [[String: Any]] {
        try await ensureRemindersAccess()
        let calendars = try reminderCalendars(named: optionalString(args, "list_name"))
        let predicate = eventStore.predicateForReminders(in: calendars)
        let completedFilter = optionalBool(args, "completed")
        let startDate = try optionalDate(args, "start_date")
        let endDate = try optionalDate(args, "end_date")
        let updatedAfter = try optionalDate(args, "updated_after")

        let encoded = try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Data, Error>) in
            eventStore.fetchReminders(matching: predicate) { fetched in
                Task { @MainActor in
                    do {
                        let reminders = (fetched ?? []).filter { reminder in
                            if let completedFilter, reminder.isCompleted != completedFilter {
                                return false
                            }
                            if let startDate, let dueDate = reminder.dueDateComponents?.date, dueDate < startDate {
                                return false
                            }
                            if startDate != nil, reminder.dueDateComponents?.date == nil {
                                return false
                            }
                            if let endDate, let dueDate = reminder.dueDateComponents?.date, dueDate > endDate {
                                return false
                            }
                            if endDate != nil, reminder.dueDateComponents?.date == nil {
                                return false
                            }
                            if let updatedAfter, let updatedAt = reminder.lastModifiedDate, updatedAt < updatedAfter {
                                return false
                            }
                            if updatedAfter != nil, reminder.lastModifiedDate == nil {
                                return false
                            }
                            return true
                        }
                        let serialized = reminders.map(self.serializeReminder)
                        try continuation.resume(returning: JSONSerialization.data(withJSONObject: serialized))
                    } catch {
                        continuation.resume(throwing: error)
                    }
                }
            }
        }

        guard let decoded = try JSONSerialization.jsonObject(with: encoded) as? [[String: Any]] else {
            throw MCPToolError("failed to decode reminder results")
        }
        return decoded
    }

    private func reminder(identifier: String) async throws -> EKReminder {
        try await ensureRemindersAccess()
        guard let item = eventStore.calendarItem(withIdentifier: identifier) as? EKReminder else {
            throw MCPToolError("reminder \(identifier) not found")
        }
        return item
    }

    private func createEvent(_ args: [String: Any]) async throws -> EKEvent {
        try await ensureEventsAccess()
        let event = EKEvent(eventStore: eventStore)
        try applyEventFields(event, args: args, requireSchedule: true, requireTitle: true)
        try eventStore.save(event, span: .thisEvent, commit: true)
        return event
    }

    private func updateEvent(_ args: [String: Any]) async throws -> EKEvent {
        let event = try await self.event(identifier: requiredString(args, "identifier"))
        try applyEventFields(event, args: args, requireSchedule: false, requireTitle: false)
        try eventStore.save(event, span: .thisEvent, commit: true)
        return event
    }

    private func deleteEvent(_ args: [String: Any]) async throws -> [String: Any] {
        let event = try await self.event(identifier: requiredString(args, "identifier"))
        try eventStore.remove(event, span: .thisEvent, commit: true)
        return ["identifier": event.calendarItemIdentifier, "deleted": true]
    }

    private func listEvents(_ args: [String: Any]) async throws -> [EKEvent] {
        try await ensureEventsAccess()
        let startDate = try optionalDate(args, "start_date") ?? Calendar.current.date(byAdding: .year, value: -1, to: Date())!
        let endDate = try optionalDate(args, "end_date") ?? Calendar.current.date(byAdding: .year, value: 1, to: Date())!
        let calendars = try eventCalendars(named: optionalString(args, "calendar_name"))
        let predicate = eventStore.predicateForEvents(withStart: startDate, end: endDate, calendars: calendars)
        return eventStore.events(matching: predicate)
    }

    private func event(identifier: String) async throws -> EKEvent {
        try await ensureEventsAccess()
        guard let item = eventStore.event(withIdentifier: identifier) else {
            throw MCPToolError("event \(identifier) not found")
        }
        return item
    }

    private func sendNotification(_ args: [String: Any]) async throws -> [String: Any] {
        try await ensureNotificationsAccess()
        let identifier = optionalString(args, "notification_id") ?? UUID().uuidString
        let url = optionalString(args, "url")
        let content = UNMutableNotificationContent()
        content.title = try requiredString(args, "title")
        content.body = try requiredString(args, "body")
        if let subtitle = optionalString(args, "subtitle") {
            content.subtitle = subtitle
        }
        content.sound = .default

        try await scheduleNotification(identifier: identifier, content: content)

        if let url = url {
            pendingInteractiveNotifications[identifier] = PendingInteractiveNotification(
                continuation: nil,
                timeoutTask: nil,
                url: url
            )
        }

        return ["notification_id": identifier, "status": "sent"]
    }

    private func sendInteractiveNotification(_ args: [String: Any]) async throws -> [String: Any] {
        try await ensureNotificationsAccess()
        let identifier = optionalString(args, "notification_id") ?? UUID().uuidString
        let url = optionalString(args, "url")
        let waitForResponse = optionalBool(args, "wait_for_response") ?? false
        let timeoutSeconds = optionalDouble(args, "timeout_seconds") ?? 60
        let actionSpecs = try parseInteractiveActions(args)
        let categoryIdentifier = "clara.interactive.\(identifier)"
        let categoryActions = actionSpecs.map(makeNotificationAction)
        try await registerCategory(identifier: categoryIdentifier, actions: categoryActions)

        let content = UNMutableNotificationContent()
        content.title = try requiredString(args, "title")
        content.body = try requiredString(args, "body")
        content.categoryIdentifier = categoryIdentifier
        content.sound = .default
        if let subtitle = optionalString(args, "subtitle") {
            content.subtitle = subtitle
        }

        if !waitForResponse {
            try await scheduleNotification(identifier: identifier, content: content)
            pendingInteractiveNotifications[identifier] = PendingInteractiveNotification(
                continuation: nil,
                timeoutTask: nil,
                url: url
            )
            return ["notification_id": identifier, "status": "sent"]
        }

        try await scheduleNotification(identifier: identifier, content: content)
        return await withCheckedContinuation { continuation in
            let timeoutTask = Task { [weak self] in
                try? await Task.sleep(nanoseconds: UInt64(max(timeoutSeconds, 1) * 1_000_000_000))
                await MainActor.run {
                    guard let self, let pending = self.pendingInteractiveNotifications.removeValue(forKey: identifier) else {
                        return
                    }
                    pending.continuation?.resume(returning: [
                        "notification_id": identifier,
                        "status": "timed_out",
                    ])
                }
            }
            pendingInteractiveNotifications[identifier] = PendingInteractiveNotification(
                continuation: continuation,
                timeoutTask: timeoutTask,
                url: url
            )
        }
    }

    private func listPhotoAlbumAssets(_ args: [String: Any]) async throws -> [[String: Any]] {
        try await ensurePhotosAccess()
        let albumName = try requiredString(args, "album_name")
        let limit = optionalInt(args, "limit")
        let album = try photoAlbum(named: albumName)
        let options = PHFetchOptions()
        options.sortDescriptors = [NSSortDescriptor(key: "creationDate", ascending: false)]
        let result = PHAsset.fetchAssets(in: album, options: options)

        var assets: [[String: Any]] = []
        result.enumerateObjects { asset, _, stop in
            assets.append(self.serializePhotoAsset(asset))
            if let limit, assets.count >= limit {
                stop.pointee = true
            }
        }
        return assets
    }

    private func exportPhotoAssets(_ args: [String: Any]) async throws -> [[String: Any]] {
        try await ensurePhotosAccess()
        let assetIDs = try requiredStringArray(args, "asset_ids")
        let destinationDir = try expandPath(requiredString(args, "destination_dir"))
        try FileManager.default.createDirectory(
            at: URL(fileURLWithPath: destinationDir),
            withIntermediateDirectories: true
        )

        var exported: [[String: Any]] = []
        for assetID in assetIDs {
            let asset = try photoAsset(identifier: assetID)
            let item = try await exportPhotoAsset(asset, destinationDir: destinationDir)
            exported.append(item)
        }
        return exported
    }

    private func removePhotoAlbumAssets(_ args: [String: Any]) async throws -> [String: Any] {
        try await ensurePhotosAccess()
        let albumName = try requiredString(args, "album_name")
        let assetIDs = try requiredStringArray(args, "asset_ids")
        let album = try photoAlbum(named: albumName)
        let fetchResult = PHAsset.fetchAssets(withLocalIdentifiers: assetIDs, options: nil)

        try await performPhotoLibraryChanges {
            if let changeRequest = PHAssetCollectionChangeRequest(for: album) {
                changeRequest.removeAssets(fetchResult)
            }
        }

        return [
            "album_name": albumName,
            "removed": assetIDs.count,
        ]
    }

    private func addPhotoAlbumAssets(_ args: [String: Any]) async throws -> [String: Any] {
        try await ensurePhotosAccess()
        let albumName = try requiredString(args, "album_name")
        let assetIDs = try requiredStringArray(args, "asset_ids")
        let album = try photoAlbum(named: albumName)
        let fetchResult = PHAsset.fetchAssets(withLocalIdentifiers: assetIDs, options: nil)

        try await performPhotoLibraryChanges {
            if let changeRequest = PHAssetCollectionChangeRequest(for: album) {
                changeRequest.addAssets(fetchResult)
            }
        }

        return [
            "album_name": albumName,
            "added": assetIDs.count,
        ]
    }

    private func defaultReminderList() async throws -> [String: Any] {
        try await ensureRemindersAccess()
        let calendar = try reminderCalendar(named: nil)
        return [
            "identifier": calendar.calendarIdentifier,
            "list_name": calendar.title,
        ]
    }

    private func reminderSnapshot(listName: String?) async throws -> [String: StoreSnapshotItem] {
        let reminders = try await listReminders(["list_name": listName as Any])
        return try snapshotMap(items: reminders)
    }

    private func eventSnapshot(calendarName: String?, startDate: Date, endDate: Date) async throws -> [String: StoreSnapshotItem] {
        let events = try await listEvents([
            "calendar_name": calendarName as Any,
            "start_date": ISO8601.dateString(from: startDate),
            "end_date": ISO8601.dateString(from: endDate),
        ]).map(serializeEvent)
        return try snapshotMap(items: events)
    }

    private func snapshotMap(items: [[String: Any]]) throws -> [String: StoreSnapshotItem] {
        var snapshot: [String: StoreSnapshotItem] = [:]
        for item in items {
            guard let identifier = item["identifier"] as? String, !identifier.isEmpty else {
                continue
            }
            let fingerprintData = try JSONSerialization.data(withJSONObject: item, options: [.sortedKeys])
            let fingerprint = String(data: fingerprintData, encoding: .utf8) ?? identifier
            snapshot[identifier] = StoreSnapshotItem(item: item, fingerprint: fingerprint)
        }
        return snapshot
    }

    private func diffSnapshots(
        old: [String: StoreSnapshotItem],
        new: [String: StoreSnapshotItem]
    ) -> [StoreChange] {
        var changes: [StoreChange] = []

        for (identifier, item) in new where old[identifier] == nil {
            changes.append(StoreChange(type: "create", item: item.item))
        }
        for (identifier, oldItem) in old {
            guard let newItem = new[identifier] else {
                changes.append(StoreChange(type: "delete", item: oldItem.item))
                continue
            }
            if oldItem.fingerprint != newItem.fingerprint {
                changes.append(StoreChange(type: "update", item: newItem.item))
            }
        }

        return changes
    }

    private func firstMatchingChange(changes: [StoreChange], allowedTypes: Set<String>) -> StoreChange? {
        changes.first { allowedTypes.contains($0.type) }
    }

    private func waitPayload(
        resource: String,
        changeType: String,
        filterKey: String,
        filterValue: String?,
        item: [String: Any]
    ) -> [String: Any] {
        var payload: [String: Any] = [
            "status": "matched",
            "resource": resource,
            "change_type": changeType,
            "changed_at": ISO8601.dateString(from: Date()),
            "item": item,
        ]
        if let filterValue, !filterValue.isEmpty {
            payload[filterKey] = filterValue
        }
        return payload
    }

    private func parseChangeTypes(_ args: [String: Any]) throws -> Set<String> {
        guard let values = optionalStringArray(args, "change_types") else {
            return ["create", "update", "delete"]
        }
        if values.isEmpty {
            throw MCPToolError("change_types must not be empty")
        }
        var result: Set<String> = []
        for value in values {
            let normalized = value.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            switch normalized {
            case "create", "update", "delete":
                result.insert(normalized)
            default:
                throw MCPToolError("unsupported change_types entry: \(value)")
            }
        }
        return result
    }

    @MainActor
    private func notificationCenter() -> UNUserNotificationCenter {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        return center
    }

    private func ensureRemindersAccess() async throws {
        if remindersAuthorized {
            return
        }
        let granted = try await eventStore.requestFullAccessToReminders()
        guard granted else {
            throw MCPToolError("access denied to Reminders")
        }
        remindersAuthorized = true
    }

    private func ensureEventsAccess() async throws {
        if eventsAuthorized {
            return
        }
        let granted = try await eventStore.requestFullAccessToEvents()
        guard granted else {
            throw MCPToolError("access denied to Calendar")
        }
        eventsAuthorized = true
    }

    private func ensurePhotosAccess() async throws {
        if photosAuthorized {
            return
        }

        let status = await PHPhotoLibrary.requestAuthorization(for: .readWrite)
        switch status {
        case .authorized, .limited:
            photosAuthorized = true
        default:
            throw MCPToolError("access denied to Photos")
        }
    }

    private func ensureNotificationsAccess() async throws {
        if notificationsAuthorized {
            return
        }
        try ensureNotificationRuntimeAvailable()
        let granted = try await notificationCenter().requestAuthorization(options: [.alert, .badge, .sound])
        guard granted else {
            throw MCPToolError("access denied to UserNotifications")
        }
        notificationsAuthorized = true
    }

    private func ensureMailAccess() throws {
        // Mail access via AppleScript is prompted by macOS on first use.
        // We can check if Mail is running or just try to execute a simple script.
        let script = "tell application \"Mail\" to get name"
        var error: NSDictionary?
        if let appleScript = NSAppleScript(source: script) {
            appleScript.executeAndReturnError(&error)
            if let error = error {
                throw MCPToolError("Apple Mail access error: \(error[NSAppleScript.errorMessage] ?? "unknown error")")
            }
        }
    }

    private func mailListInbox(_ args: [String: Any]) async throws -> [[String: Any]] {
        try ensureMailAccess()
        let limit = optionalInt(args, "limit") ?? 10
        let accountName = optionalString(args, "account_name")
        let unreadOnly = optionalBool(args, "unread") ?? false
        
        var script = "tell application \"Mail\"\n"
        if let accountName = accountName, !accountName.isEmpty {
            script += "    set theInbox to mailbox \"INBOX\" of account \"\(accountName)\"\n"
        } else {
            script += "    set theInbox to inbox\n"
        }
        if unreadOnly {
            script += "    set theMessages to (every message of theInbox whose read status is false)\n"
        } else {
            script += "    set theMessages to messages of theInbox\n"
        }
        script += "    set msgCount to count of theMessages\n"
        script += "    set startIndex to msgCount - \(limit - 1)\n"
        script += "    if startIndex < 1 then set startIndex to 1\n"
        script += "    set results to {}\n"
        script += "    if msgCount > 0 then\n"
        script += "        repeat with i from msgCount to startIndex by -1\n"
        script += "            set theMsg to item i of theMessages\n"
        script += "            set end of results to {id:id of theMsg, subject:subject of theMsg, sender:sender of theMsg, date_received:date received of theMsg as string, read_status:read status of theMsg}\n"
        script += "        end repeat\n"
        script += "    end if\n"
        script += "    return results\n"
        script += "end tell"

        return try executeAppleScript(script, timeout: 120) as? [[String: Any]] ?? []
    }

    private func mailGetMessage(_ args: [String: Any]) async throws -> [String: Any] {
        try ensureMailAccess()
        let messageID = try requiredString(args, "message_id")
        let script = """
        tell application "Mail"
            set theMessage to (first message of every mailbox of every account whose id is \(messageID))
            return {id:id of theMessage, subject:subject of theMessage, sender:sender of theMessage, content:content of theMessage, date_received:date received of theMessage as string, read_status:read status of theMessage}
        end tell
        """
        return try executeAppleScript(script) as? [String: Any] ?? [:]
    }

    private func mailMove(_ args: [String: Any]) async throws -> [String: Any] {
        try ensureMailAccess()
        let messageID = try requiredString(args, "message_id")
        let targetMailboxName = try requiredString(args, "target_mailbox")
        let accountName = try requiredString(args, "account_name")
        let script = """
        tell application "Mail"
            set theMessage to (first message of every mailbox of every account whose id is \(messageID))
            set targetMailbox to mailbox "\(targetMailboxName)" of account "\(accountName)"
            move theMessage to targetMailbox
            return {id:id of theMessage, moved:true, target:targetMailboxName}
        end tell
        """
        return try executeAppleScript(script) as? [String: Any] ?? [:]
    }

    private func mailFlag(_ args: [String: Any]) async throws -> [String: Any] {
        try ensureMailAccess()
        let messageID = try requiredString(args, "message_id")
        let flagged = optionalBool(args, "flagged") ?? false
        let script = """
        tell application "Mail"
            set theMessage to (first message of every mailbox of every account whose id is \(messageID))
            set flagged status of theMessage to \(flagged)
            return {id:id of theMessage, flagged:\(flagged)}
        end tell
        """
        return try executeAppleScript(script) as? [String: Any] ?? [:]
    }

    private func mailMarkRead(_ args: [String: Any]) async throws -> [String: Any] {
        try ensureMailAccess()
        let messageID = try requiredString(args, "message_id")
        let read = optionalBool(args, "read") ?? false
        let script = """
        tell application "Mail"
            set theMessage to (first message of every mailbox of every account whose id is \(messageID))
            set read status of theMessage to \(read)
            return {id:id of theMessage, read:\(read)}
        end tell
        """
        return try executeAppleScript(script) as? [String: Any] ?? [:]
    }

    private func mailCreateDraft(_ args: [String: Any]) async throws -> [String: Any] {
        try ensureMailAccess()
        let subject = try requiredString(args, "subject")
        let content = try requiredString(args, "content")
        let to = try requiredString(args, "to")
        let accountName = optionalString(args, "account_name")
        
        var script = "tell application \"Mail\"\n"
        if let accountName = accountName, !accountName.isEmpty {
            script += "    set newMessage to make new outgoing message with properties {subject:\"\(subject)\", content:\"\(content)\", visible:true} at end of outgoing messages of account \"\(accountName)\"\n"
        } else {
            script += "    set newMessage to make new outgoing message with properties {subject:\"\(subject)\", content:\"\(content)\", visible:true}\n"
        }
        script += """
            tell newMessage
                make new to recipient at end of to recipients with properties {address:"\(to)"}
            end tell
            return {status:"draft created", subject:"\(subject)"}
        end tell
        """
        return try executeAppleScript(script) as? [String: Any] ?? [:]
    }

    private func mailSend(_ args: [String: Any]) async throws -> [String: Any] {
        try ensureMailAccess()
        let subject = try requiredString(args, "subject")
        let content = try requiredString(args, "content")
        let to = try requiredString(args, "to")
        let accountName = optionalString(args, "account_name")
        
        var script = "tell application \"Mail\"\n"
        if let accountName = accountName, !accountName.isEmpty {
            script += "    set newMessage to make new outgoing message with properties {subject:\"\(subject)\", content:\"\(content)\", visible:true} at end of outgoing messages of account \"\(accountName)\"\n"
        } else {
            script += "    set newMessage to make new outgoing message with properties {subject:\"\(subject)\", content:\"\(content)\", visible:true}\n"
        }
        script += """
            tell newMessage
                make new to recipient at end of to recipients with properties {address:"\(to)"}
                send
            end tell
            return {status:"sent", subject:"\(subject)"}
        end tell
        """
        return try executeAppleScript(script) as? [String: Any] ?? [:]
    }

    private func mailDelete(_ args: [String: Any]) async throws -> [String: Any] {
        try ensureMailAccess()
        let messageID = try requiredString(args, "message_id")
        let script = """
        tell application "Mail"
            set theMessage to (first message of every mailbox of every account whose id is \(messageID))
            delete theMessage
            return {id:id of theMessage, deleted:true}
        end tell
        """
        return try executeAppleScript(script) as? [String: Any] ?? [:]
    }

    private func mailGetMailboxes(_ args: [String: Any]) async throws -> [[String: Any]] {
        try ensureMailAccess()
        let accountName = try requiredString(args, "account_name")
        let script = """
        tell application "Mail"
            set theAccount to account "\(accountName)"
            set mailboxList to every mailbox of theAccount
            set results to {}
            repeat with theMailbox in mailboxList
                set end of results to {name:name of theMailbox}
            end repeat
            return results
        end tell
        """
        return try executeAppleScript(script, timeout: 120) as? [[String: Any]] ?? []
    }

    private func executeAppleScript(_ source: String, timeout: Int = 120) throws -> Any? {
        var error: NSDictionary?
        
        let wrappedSource = "with timeout of \(timeout) seconds\n\(source)\nend timeout"
        
        guard let appleScript = NSAppleScript(source: wrappedSource) else {
            throw MCPToolError("failed to create NSAppleScript")
        }
        let descriptor = appleScript.executeAndReturnError(&error)
        if let error = error {
            throw MCPToolError("AppleScript error: \(error[NSAppleScript.errorMessage] ?? "unknown error")")
        }
        return convertDescriptor(descriptor)
    }

    private func convertDescriptor(_ descriptor: NSAppleEventDescriptor) -> Any? {
        switch descriptor.descriptorType {
        case typeNull, typeType:
            return nil
        case typeBoolean:
            return descriptor.booleanValue
        case typeSInt16, typeSInt32, typeSInt64, typeUInt32, typeIEEE32BitFloatingPoint, typeIEEE64BitFloatingPoint:
            return descriptor.int32Value // Should ideally be more precise
        case typeUnicodeText, typeText, typeUTF8Text:
            return descriptor.stringValue
        case typeAEList:
            var result: [Any] = []
            guard descriptor.numberOfItems > 0 else { return result }
            for i in 1...descriptor.numberOfItems {
                if let item = descriptor.atIndex(i) {
                    result.append(convertDescriptor(item) ?? NSNull())
                }
            }
            return result
        case typeAERecord:
            var result: [String: Any] = [:]
            guard descriptor.numberOfItems > 0 else { return result }
            for i in 1...descriptor.numberOfItems {
                let keyword = descriptor.keywordForDescriptor(at: i)
                if let item = descriptor.atIndex(i) {
                    let key = keywordToString(keyword)
                    result[key] = convertDescriptor(item) ?? NSNull()
                }
            }
            return result
        default:
            return descriptor.stringValue
        }
    }

    private func keywordToString(_ keyword: AEKeyword) -> String {
        // Common Mail.app record keywords
        switch keyword {
        case 0x70696420: return "id" // 'id  '
        case 0x7375626a: return "subject" // 'subj'
        case 0x736e6472: return "sender" // 'sndr'
        case 0x63746e74: return "content" // 'ctnt'
        case 0x64747263: return "date_received" // 'dtrc'
        case 0x72656164: return "read_status" // 'read'
        case 0x6e616d65: return "name" // 'name'
        case 0x666c6167: return "flagged" // 'flag'
        case 0x6d766564: return "moved" // 'mved'
        case 0x74726774: return "target" // 'trgt'
        case 0x73746174: return "status" // 'stat'
        default:
            // Fallback: convert FourCharCode to String
            let bytes = [
                UInt8((keyword >> 24) & 0xFF),
                UInt8((keyword >> 16) & 0xFF),
                UInt8((keyword >> 8) & 0xFF),
                UInt8(keyword & 0xFF)
            ]
            return String(bytes: bytes, encoding: .ascii)?.trimmingCharacters(in: .whitespaces) ?? String(keyword)
        }
    }

    private func ensureNotificationRuntimeAvailable() throws {
        let executableURL = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
        var candidate = executableURL.deletingLastPathComponent()
        while candidate.path != "/", !candidate.path.isEmpty {
            if candidate.pathExtension == "app" {
                let infoPlistPath = candidate.appendingPathComponent("Contents/Info.plist").path
                if FileManager.default.fileExists(atPath: infoPlistPath) {
                    return
                }
            }
            candidate.deleteLastPathComponent()
        }

        throw MCPToolError(
            "UserNotifications requires ClaraBridge to run from an app bundle executable " +
                "(for example /usr/local/libexec/ClaraBridge.app/Contents/MacOS/ClaraBridge)."
        )
    }

    private func reminderCalendars(named listName: String?) throws -> [EKCalendar]? {
        if let listName, !listName.isEmpty {
            let calendars = eventStore.calendars(for: .reminder).filter { $0.title == listName }
            guard !calendars.isEmpty else {
                throw MCPToolError("reminder list \(listName) not found")
            }
            return calendars
        }
        return nil
    }

    private func eventCalendars(named calendarName: String?) throws -> [EKCalendar]? {
        if let calendarName, !calendarName.isEmpty {
            let calendars = eventStore.calendars(for: .event).filter { $0.title == calendarName }
            guard !calendars.isEmpty else {
                throw MCPToolError("calendar \(calendarName) not found")
            }
            return calendars
        }
        return nil
    }

    private func reminderCalendar(named listName: String?) throws -> EKCalendar {
        if let listName, !listName.isEmpty {
            guard let calendar = eventStore.calendars(for: .reminder).first(where: { $0.title == listName }) else {
                throw MCPToolError("reminder list \(listName) not found")
            }
            return calendar
        }
        guard let calendar = eventStore.defaultCalendarForNewReminders() else {
            throw MCPToolError("default reminder list is unavailable")
        }
        return calendar
    }

    private func eventCalendar(named calendarName: String?) throws -> EKCalendar {
        if let calendarName, !calendarName.isEmpty {
            guard let calendar = eventStore.calendars(for: .event).first(where: { $0.title == calendarName }) else {
                throw MCPToolError("calendar \(calendarName) not found")
            }
            return calendar
        }
        guard let calendar = eventStore.defaultCalendarForNewEvents else {
            throw MCPToolError("default event calendar is unavailable")
        }
        return calendar
    }

    private func photoAlbum(named albumName: String) throws -> PHAssetCollection {
        let options = PHFetchOptions()
        options.predicate = NSPredicate(format: "title == %@", albumName)
        let albums = PHAssetCollection.fetchAssetCollections(with: .album, subtype: .any, options: options)
        guard let album = albums.firstObject else {
            throw MCPToolError("photo album \(albumName) not found")
        }
        return album
    }

    private func photoAsset(identifier: String) throws -> PHAsset {
        let result = PHAsset.fetchAssets(withLocalIdentifiers: [identifier], options: nil)
        guard let asset = result.firstObject else {
            throw MCPToolError("photo asset \(identifier) not found")
        }
        return asset
    }

    private func applyReminderFields(_ reminder: EKReminder, args: [String: Any], requireTitle: Bool) throws {
        if requireTitle {
            reminder.title = try requiredString(args, "title")
        } else if let title = optionalString(args, "title") {
            reminder.title = title
        }
        if let notes = optionalStringPresence(args, "notes") {
            reminder.notes = notes.isEmpty ? nil : notes
        }
        if let duePresence = optionalStringPresence(args, "due_date") {
            if duePresence.isEmpty {
                reminder.dueDateComponents = nil
            } else {
                reminder.dueDateComponents = try Calendar.current.dateComponents(in: .current, from: parseDate(duePresence))
            }
        }
        if let completed = optionalBool(args, "completed") {
            reminder.isCompleted = completed
            reminder.completionDate = try completed ? (optionalDate(args, "completion_date") ?? Date()) : nil
        }
        if let priority = optionalInt(args, "priority") {
            reminder.priority = priority
        }
        if args.keys.contains("list_name") {
            reminder.calendar = try reminderCalendar(named: optionalString(args, "list_name"))
        } else if reminder.calendar == nil {
            reminder.calendar = try reminderCalendar(named: nil)
        }
        if let location = optionalStringPresence(args, "location") {
            reminder.location = location.isEmpty ? nil : location
        }
        if args.keys.contains("alarms") {
            reminder.alarms = try parseAlarms(args["alarms"])
        }
    }

    private func applyEventFields(
        _ event: EKEvent,
        args: [String: Any],
        requireSchedule: Bool,
        requireTitle: Bool
    ) throws {
        if requireTitle {
            event.title = try requiredString(args, "title")
        } else if let title = optionalString(args, "title") {
            event.title = title
        }
        if let notes = optionalStringPresence(args, "notes") {
            event.notes = notes.isEmpty ? nil : notes
        }
        if requireSchedule || args.keys.contains("start_date") {
            event.startDate = try dateValue(args, key: "start_date", required: requireSchedule)
        }
        if requireSchedule || args.keys.contains("end_date") {
            event.endDate = try dateValue(args, key: "end_date", required: requireSchedule)
        }
        if let allDay = optionalBool(args, "all_day") {
            event.isAllDay = allDay
        }
        if args.keys.contains("calendar_name") {
            event.calendar = try eventCalendar(named: optionalString(args, "calendar_name"))
        } else if event.calendar == nil {
            event.calendar = try eventCalendar(named: nil)
        }
        if let location = optionalStringPresence(args, "location") {
            event.location = location.isEmpty ? nil : location
        }
        if args.keys.contains("alarms") {
            event.alarms = try parseAlarms(args["alarms"])
        }
        guard event.startDate <= event.endDate else {
            throw MCPToolError("event start_date must be before or equal to end_date")
        }
    }

    private func parseInteractiveActions(_ args: [String: Any]) throws -> [[String: Any]] {
        guard let rawActions = args["actions"] as? [Any], !rawActions.isEmpty else {
            throw MCPToolError("actions array is required for interactive notifications")
        }
        return try rawActions.map { rawAction in
            guard let action = rawAction as? [String: Any] else {
                throw MCPToolError("interactive action entries must be objects")
            }
            _ = try requiredString(action, "id")
            _ = try requiredString(action, "title")
            return action
        }
    }

    private func registerCategory(identifier: String, actions: [UNNotificationAction]) async throws {
        notificationCategories[identifier] = UNNotificationCategory(
            identifier: identifier,
            actions: actions,
            intentIdentifiers: []
        )
        UNUserNotificationCenter.current().setNotificationCategories(Set(notificationCategories.values))
    }

    private func scheduleNotification(identifier: String, content: UNMutableNotificationContent) async throws {
        let request = UNNotificationRequest(identifier: identifier, content: content, trigger: nil)
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            UNUserNotificationCenter.current().add(request) { error in
                if let error {
                    continuation.resume(throwing: MCPToolError("failed to schedule notification: \(error.localizedDescription)"))
                } else {
                    continuation.resume(returning: ())
                }
            }
        }
    }

    private func makeNotificationAction(from spec: [String: Any]) -> UNNotificationAction {
        let actionID = (try? requiredString(spec, "id")) ?? UUID().uuidString
        let title = (try? requiredString(spec, "title")) ?? actionID
        var options: UNNotificationActionOptions = []
        if optionalBool(spec, "foreground") == true {
            options.insert(.foreground)
        }
        if optionalBool(spec, "destructive") == true {
            options.insert(.destructive)
        }
        return UNNotificationAction(identifier: actionID, title: title, options: options)
    }

    private nonisolated static func normalizeNotificationAction(_ identifier: String) -> String {
        switch identifier {
        case UNNotificationDefaultActionIdentifier:
            return "default"
        case UNNotificationDismissActionIdentifier:
            return "dismissed"
        default:
            return identifier
        }
    }

    nonisolated private func serializeReminder(_ reminder: EKReminder) -> [String: Any] {
        var result: [String: Any] = [
            "identifier": reminder.calendarItemIdentifier,
            "title": reminder.title ?? "",
            "completed": reminder.isCompleted,
            "priority": reminder.priority,
            "list_name": reminder.calendar.title,
        ]
        if let notes = reminder.notes {
            result["notes"] = notes
        }
        if let createdAt = reminder.creationDate {
            result["created_at"] = ISO8601.dateString(from: createdAt)
        }
        if let updatedAt = reminder.lastModifiedDate {
            result["updated_at"] = ISO8601.dateString(from: updatedAt)
        }
        if let dueDate = reminder.dueDateComponents?.date {
            result["due_date"] = ISO8601.dateString(from: dueDate)
        }
        if let completionDate = reminder.completionDate {
            result["completion_date"] = ISO8601.dateString(from: completionDate)
        }
        if let location = reminder.location {
            result["location"] = location
        }
        if let alarms = reminder.alarms {
            result["alarms"] = alarms.map(serializeAlarm)
        }
        return result
    }

    nonisolated private func serializeEvent(_ event: EKEvent) -> [String: Any] {
        var result: [String: Any] = [
            "identifier": event.calendarItemIdentifier,
            "title": event.title ?? "",
            "start_date": ISO8601.dateString(from: event.startDate),
            "end_date": ISO8601.dateString(from: event.endDate),
            "all_day": event.isAllDay,
            "calendar_name": event.calendar.title,
        ]
        if let notes = event.notes {
            result["notes"] = notes
        }
        if let location = event.location {
            result["location"] = location
        }
        if let alarms = event.alarms {
            result["alarms"] = alarms.map(serializeAlarm)
        }
        return result
    }

    nonisolated private func serializePhotoAsset(_ asset: PHAsset) -> [String: Any] {
        var result: [String: Any] = [
            "identifier": asset.localIdentifier,
            "media_type": assetMediaTypeName(asset.mediaType),
            "pixel_width": asset.pixelWidth,
            "pixel_height": asset.pixelHeight,
            "favorite": asset.isFavorite,
            "hidden": asset.isHidden,
        ]
        if let created = asset.creationDate {
            result["created_at"] = ISO8601.dateString(from: created)
        }
        if let updated = asset.modificationDate {
            result["updated_at"] = ISO8601.dateString(from: updated)
        }
        return result
    }

    private func exportPhotoAsset(_ asset: PHAsset, destinationDir: String) async throws -> [String: Any] {
        let resources = PHAssetResource.assetResources(for: asset)
        guard let resource = resources.first else {
            throw MCPToolError("photo asset \(asset.localIdentifier) has no exportable resource")
        }

        let baseName = resource.originalFilename.isEmpty ? asset.localIdentifier.replacingOccurrences(of: "/", with: "-") : resource.originalFilename
        let destinationURL = uniqueFileURL(in: URL(fileURLWithPath: destinationDir), preferredName: baseName)

        try await writePhotoResource(resource, toFile: destinationURL)

        return [
            "identifier": asset.localIdentifier,
            "path": destinationURL.path,
        ]
    }

    private nonisolated func performPhotoLibraryChanges(
        _ changes: @escaping @Sendable () -> Void
    ) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            PHPhotoLibrary.shared().performChanges(changes) { success, error in
                if let error {
                    continuation.resume(throwing: error)
                    return
                }
                if success {
                    continuation.resume(returning: ())
                } else {
                    continuation.resume(throwing: PhotoToolError("failed to apply Photos library change"))
                }
            }
        }
    }

    private nonisolated func writePhotoResource(_ resource: PHAssetResource, toFile destinationURL: URL) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            PHAssetResourceManager.default().writeData(for: resource, toFile: destinationURL, options: nil) { error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume(returning: ())
                }
            }
        }
    }

    private func uniqueFileURL(in directory: URL, preferredName: String) -> URL {
        let candidate = directory.appendingPathComponent(preferredName)
        if !FileManager.default.fileExists(atPath: candidate.path) {
            return candidate
        }

        let stem = candidate.deletingPathExtension().lastPathComponent
        let ext = candidate.pathExtension
        for index in 1 ... 999 {
            let fileName = ext.isEmpty ? "\(stem)-\(index)" : "\(stem)-\(index).\(ext)"
            let next = directory.appendingPathComponent(fileName)
            if !FileManager.default.fileExists(atPath: next.path) {
                return next
            }
        }
        return directory.appendingPathComponent(UUID().uuidString + "-" + preferredName)
    }

    nonisolated func assetMediaTypeName(_ mediaType: PHAssetMediaType) -> String {
        switch mediaType {
        case .image:
            return "image"
        case .video:
            return "video"
        case .audio:
            return "audio"
        default:
            return "unknown"
        }
    }

    nonisolated private func serializeAlarm(_ alarm: EKAlarm) -> [String: Any] {
        if let absoluteDate = alarm.absoluteDate {
            return ["absolute_date": ISO8601.dateString(from: absoluteDate)]
        }
        return ["relative_offset": alarm.relativeOffset]
    }

    nonisolated private func parseAlarms(_ raw: Any?) throws -> [EKAlarm]? {
        guard let raw else {
            return nil
        }
        guard let rawAlarms = raw as? [Any] else {
            throw MCPToolError("alarms must be an array")
        }
        return try rawAlarms.map { entry in
            if let dateString = entry as? String {
                return try EKAlarm(absoluteDate: parseDate(dateString))
            }
            guard let map = entry as? [String: Any] else {
                throw MCPToolError("alarm entries must be ISO-8601 strings or objects")
            }
            if let absoluteDate = map["absolute_date"] as? String {
                return try EKAlarm(absoluteDate: parseDate(absoluteDate))
            }
            if let relativeOffset = map["relative_offset"] as? Double {
                return EKAlarm(relativeOffset: relativeOffset)
            }
            throw MCPToolError("alarm entries must include absolute_date or relative_offset")
        }
    }

    private func tool(
        name: String,
        description: String,
        properties: [[String: [String: Any]]],
        required: [String] = []
    ) -> [String: Any] {
        let mergedProperties = properties.reduce(into: [String: [String: Any]]()) { partialResult, item in
            for (key, value) in item {
                partialResult[key] = value
            }
        }
        var schema: [String: Any] = [
            "type": "object",
            "properties": mergedProperties,
        ]
        if !required.isEmpty {
            schema["required"] = required
        }
        return [
            "name": name,
            "description": description,
            "inputSchema": schema,
        ]
    }

    private func stringProperty(_ name: String, _ description: String) -> [String: [String: Any]] {
        [name: ["type": "string", "description": description]]
    }

    private func boolProperty(_ name: String, _ description: String) -> [String: [String: Any]] {
        [name: ["type": "boolean", "description": description]]
    }

    private func numberProperty(_ name: String, _ description: String) -> [String: [String: Any]] {
        [name: ["type": "number", "description": description]]
    }

    private func stringArrayProperty(_ name: String, _ description: String) -> [String: [String: Any]] {
        [
            name: [
                "type": "array",
                "description": description,
                "items": ["type": "string"],
            ],
        ]
    }

    private func alarmsProperty() -> [String: [String: Any]] {
        [
            "alarms": [
                "type": "array",
                "description": "Optional list of alarms. Each entry may be an ISO-8601 string or an object with absolute_date or relative_offset.",
            ],
        ]
    }

    private func actionsProperty() -> [String: [String: Any]] {
        [
            "actions": [
                "type": "array",
                "description": "List of action objects with id, title, and optional foreground/destructive booleans.",
            ],
        ]
    }

    nonisolated private func requiredString(_ args: [String: Any], _ key: String) throws -> String {
        guard let value = args[key] as? String, !value.isEmpty else {
            throw MCPToolError("\(key) is required")
        }
        return value
    }

    nonisolated private func requiredStringArray(_ args: [String: Any], _ key: String) throws -> [String] {
        guard let values = optionalStringArray(args, key), !values.isEmpty else {
            throw MCPToolError("\(key) is required")
        }
        return values
    }

    nonisolated private func optionalString(_ args: [String: Any], _ key: String) -> String? {
        args[key] as? String
    }

    nonisolated private func optionalStringPresence(_ args: [String: Any], _ key: String) -> String? {
        guard args.keys.contains(key) else {
            return nil
        }
        return args[key] as? String ?? ""
    }

    nonisolated private func optionalStringArray(_ args: [String: Any], _ key: String) -> [String]? {
        guard let raw = args[key] as? [Any] else {
            return nil
        }
        return raw.compactMap { $0 as? String }
    }

    nonisolated private func optionalBool(_ args: [String: Any], _ key: String) -> Bool? {
        if let value = args[key] as? Bool {
            return value
        }
        if let number = args[key] as? NSNumber {
            return number.boolValue
        }
        return nil
    }

    nonisolated private func optionalDouble(_ args: [String: Any], _ key: String) -> Double? {
        if let value = args[key] as? Double {
            return value
        }
        if let value = args[key] as? Int {
            return Double(value)
        }
        return nil
    }

    nonisolated private func optionalInt(_ args: [String: Any], _ key: String) -> Int? {
        if let value = args[key] as? Int {
            return value
        }
        if let value = args[key] as? Double {
            return Int(value)
        }
        return nil
    }

    nonisolated func expandPath(_ raw: String) -> String {
        let expandedEnv = NSString(string: raw).expandingTildeInPath
        return ProcessInfo.processInfo.environment.reduce(expandedEnv) { partial, item in
            partial.replacingOccurrences(of: "${\(item.key)}", with: item.value)
        }
    }

    private func dateValue(_ args: [String: Any], key: String, required: Bool) throws -> Date {
        if required {
            guard let raw = optionalString(args, key) else {
                throw MCPToolError("\(key) is required")
            }
            return try parseDate(raw)
        }
        guard let raw = optionalString(args, key) else {
            throw MCPToolError("\(key) is required")
        }
        return try parseDate(raw)
    }

    private func optionalDate(_ args: [String: Any], _ key: String) throws -> Date? {
        guard let raw = optionalString(args, key), !raw.isEmpty else {
            return nil
        }
        return try parseDate(raw)
    }

    nonisolated private func parseDate(_ raw: String) throws -> Date {
        if let parsed = ISO8601.parse(raw) {
            return parsed
        }
        throw MCPToolError("invalid ISO-8601 date: \(raw)")
    }

    private func toolResult(_ value: Any) throws -> Any {
        return value
    }
}

private struct PhotoToolError: LocalizedError {
    let message: String

    init(_ message: String) {
        self.message = message
    }

    var errorDescription: String? {
        message
    }
}

private struct PendingInteractiveNotification {
    let continuation: CheckedContinuation<[String: Any], Never>?
    let timeoutTask: Task<Void, Never>?
    let url: String?
}

private struct StoreSnapshotItem {
    let item: [String: Any]
    let fingerprint: String
}

private struct StoreChange {
    let type: String
    let item: [String: Any]
}

private struct MCPToolError: LocalizedError {
    let message: String
    let errorType: String?
    let context: String?
    let callStack: [String]

    init(_ message: String) {
        self.message = message
        errorType = nil
        context = nil
        callStack = []
    }

    init(_ message: String, errorType: String?, context: String?, callStack: [String]) {
        self.message = message
        self.errorType = errorType
        self.context = context
        self.callStack = callStack
    }

    static func wrapping(_ error: Error, context: String? = nil) -> MCPToolError {
        if let bridgeError = error as? MCPToolError {
            return bridgeError
        }
        return MCPToolError(
            error.localizedDescription,
            errorType: String(describing: type(of: error)),
            context: context,
            callStack: Thread.callStackSymbols
        )
    }

    var errorDescription: String? {
        message
    }

    var resultPayload: [String: Any] {
        var payload: [String: Any] = [
            "content": [["type": "text", "text": message]],
            "isError": true,
        ]
        if errorType != nil || context != nil || !callStack.isEmpty {
            var debug: [String: Any] = [:]
            if let errorType {
                debug["error_type"] = errorType
            }
            if let context {
                debug["context"] = context
            }
            if !callStack.isEmpty {
                debug["call_stack"] = callStack
            }
            payload["structuredContent"] = [
                "error": [
                    "message": message,
                    "debug": debug,
                ],
            ]
        }
        return payload
    }
}

@MainActor
final class OneShotContinuation<Output: Sendable> {
    private var resumeHandler: (@MainActor (Output) -> Void)?

    init(continuation: CheckedContinuation<Output, Never>) {
        self.resumeHandler = { value in
            continuation.resume(returning: value)
        }
    }

    init(resumeHandler: @escaping @MainActor (Output) -> Void) {
        self.resumeHandler = resumeHandler
    }

    @discardableResult
    func resume(returning value: Output) -> Bool {
        guard let handler = resumeHandler else {
            return false
        }
        self.resumeHandler = nil
        handler(value)
        return true
    }
}

private enum MCPProtocolError: Error {
    case invalidRequest(String)
    case methodNotFound(String)
    case invalidParams(String)
    case internalError(String)

    var code: Int {
        switch self {
        case .invalidRequest: return -32600
        case .methodNotFound: return -32601
        case .invalidParams: return -32602
        case .internalError: return -32603
        }
    }

    var message: String {
        switch self {
        case let .invalidRequest(message), let .methodNotFound(message), let .invalidParams(message), let .internalError(message):
            return message
        }
    }
}

private enum ISO8601 {
    nonisolated static func makeFormatter(withFractionalSeconds: Bool) -> ISO8601DateFormatter {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = withFractionalSeconds
            ? [.withInternetDateTime, .withFractionalSeconds]
            : [.withInternetDateTime]
        return formatter
    }

    nonisolated static func dateString(from date: Date) -> String {
        makeFormatter(withFractionalSeconds: true).string(from: date)
    }

    nonisolated static func parse(_ raw: String) -> Date? {
        makeFormatter(withFractionalSeconds: true).date(from: raw)
            ?? makeFormatter(withFractionalSeconds: false).date(from: raw)
    }
}
