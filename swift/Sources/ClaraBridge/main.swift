// ClaraBridge — Swift native macOS MCP server.
//
// This executable speaks MCP over stdio and exposes native macOS capabilities
// such as EventKit-backed reminders, calendar events, and user notifications.

@preconcurrency import EventKit
import Foundation
import AppKit
@preconcurrency import Photos
@preconcurrency import UserNotifications

@main
struct ClaraBridgeMain {
    static func main() {
        let server = MCPStdioServer()
        server.start()
        RunLoop.main.run()
    }
}

@MainActor
final class MCPStdioServer {
    private let input = FileHandle.standardInput
    private let output = FileHandle.standardOutput
    private let errorOutput = FileHandle.standardError
    private var buffer = Data()
    private let bridgeTools: BridgeTools

    init() {
        self.bridgeTools = BridgeTools()
        self.bridgeTools.server = self
    }

    func start() {
        input.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            Task { @MainActor [weak self] in
                self?.consumeInput(data)
            }
        }
    }

    func sendNotification(method: String, params: [String: Any]) {
        do {
            try writeEnvelope([
                "jsonrpc": "2.0",
                "method": method,
                "params": params,
            ])
        } catch {
            logError("failed to send notification \(method): \(error.localizedDescription)")
        }
    }

    private func consumeInput(_ data: Data) {
        if data.isEmpty {
            input.readabilityHandler = nil
            return
        }

        buffer.append(data)
        while let newlineIndex = buffer.firstIndex(of: 0x0A) {
            let lineData = Data(buffer[..<newlineIndex])
            buffer.removeSubrange(...newlineIndex)
            if lineData.isEmpty {
                continue
            }
            let sanitized = trimCarriageReturn(from: lineData)
            guard let line = String(data: sanitized, encoding: .utf8) else {
                do {
                    try writeError(id: nil, error: .invalidRequest("request line must be valid UTF-8"))
                } catch {
                    logError("failed to write UTF-8 error response: \(error.localizedDescription)")
                }
                continue
            }

            Task { @MainActor [weak self] in
                guard let self else { return }
                do {
                    try await self.handleLine(line)
                } catch let protocolError as MCPProtocolError {
                    try? self.writeError(id: nil, error: protocolError)
                } catch {
                    try? self.writeError(id: nil, error: .internalError(error.localizedDescription))
                }
            }
        }
    }

    private func handleLine(_ line: String) async throws {
        guard !line.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return
        }

        let data = Data(line.utf8)
        let raw = try JSONSerialization.jsonObject(with: data)
        guard let request = raw as? [String: Any] else {
            throw MCPProtocolError.invalidRequest("request must be a JSON object")
        }

        let requestID = request["id"]
        guard let method = request["method"] as? String else {
            try writeError(id: requestID, error: .invalidRequest("missing method"))
            return
        }

        switch method {
        case "initialize":
            try writeResult(id: requestID, result: initializeResult())
        case "notifications/initialized":
            return
        case "tools/list":
            try writeResult(id: requestID, result: ["tools": bridgeTools.listTools()])
        case "tools/call":
            do {
                let params = request["params"] as? [String: Any] ?? [:]
                guard let name = params["name"] as? String, !name.isEmpty else {
                    throw MCPProtocolError.invalidParams("missing tool name")
                }
                let arguments = params["arguments"] as? [String: Any] ?? [:]
                let result = try await bridgeTools.callTool(name: name, arguments: arguments)
                try writeResult(id: requestID, result: result)
            } catch let toolError as MCPToolError {
                try writeResult(id: requestID, result: toolError.resultPayload)
            } catch let protocolError as MCPProtocolError {
                try writeError(id: requestID, error: protocolError)
            } catch {
                let bridgeError = MCPToolError.wrapping(error, context: "tools/call")
                try writeResult(id: requestID, result: bridgeError.resultPayload)
            }
        default:
            if requestID != nil {
                try writeError(id: requestID, error: .methodNotFound("method not found: \(method)"))
            }
        }
    }

    private func initializeResult() -> [String: Any] {
        [
            "protocolVersion": "2025-11-25",
            "capabilities": [
                "tools": ["listChanged": false],
            ],
            "serverInfo": [
                "name": "ClaraBridge",
                "version": "0.1.0",
            ],
            "instructions": "Native macOS MCP server for reminders, calendar events, system notifications, and local callbacks.",
        ]
    }

    private func writeResult(id: Any?, result: [String: Any]) throws {
        try writeEnvelope(["jsonrpc": "2.0", "id": id ?? NSNull(), "result": result])
    }

    private func writeError(id: Any?, error: MCPProtocolError) throws {
        try writeEnvelope([
            "jsonrpc": "2.0",
            "id": id ?? NSNull(),
            "error": [
                "code": error.code,
                "message": error.message,
            ],
        ])
    }

    private func writeEnvelope(_ envelope: [String: Any]) throws {
        let json = try JSONSerialization.data(withJSONObject: envelope)
        output.write(json)
        output.write(Data([0x0A]))
    }

    private func trimCarriageReturn(from data: Data) -> Data {
        guard data.last == 0x0D else {
            return data
        }
        return data.dropLast()
    }

    func logError(_ message: String) {
        guard let data = (message + "\n").data(using: .utf8) else { return }
        errorOutput.write(data)
    }
}

@MainActor
final class BridgeTools: NSObject, UNUserNotificationCenterDelegate {
    weak var server: MCPStdioServer?

    private let eventStore = EKEventStore()
    private var remindersAuthorized = false
    private var eventsAuthorized = false
    private var photosAuthorized = false
    private var notificationsAuthorized = false
    private var eventStoreObserver: NSObjectProtocol?
    private var pendingInteractiveNotifications: [String: PendingInteractiveNotification] = [:]
    private var pendingReminderWaits: [String: PendingStoreWait] = [:]
    private var pendingEventWaits: [String: PendingStoreWait] = [:]
    private var notificationCategories: [String: UNNotificationCategory] = [:]

    // Proactive push baselines — nil until first successful snapshot.
    private var reminderPushBaseline: [String: StoreSnapshotItem]? = nil
    private var eventPushBaseline: [String: StoreSnapshotItem]? = nil

    override init() {
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
    }

    @MainActor
    func listTools() -> [[String: Any]] {
        [
            tool(
                name: "reminders_create",
                description: "Create a reminder with EventKit fields such as title, notes, due date, completion status, priority, list name, location, and alarms.",
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
                description: "Fetch a reminder by EventKit identifier.",
                properties: [stringProperty("identifier", "Reminder identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "reminders_update",
                description: "Update an existing reminder by EventKit identifier.",
                properties: [
                    stringProperty("identifier", "Reminder identifier."),
                    stringProperty("title", "Updated title."),
                    stringProperty("notes", "Updated notes. Use an empty string to clear notes."),
                    stringProperty("due_date", "Updated due date in ISO-8601 format. Use an empty string to clear the due date."),
                    boolProperty("completed", "Updated completion status."),
                    numberProperty("priority", "Updated EventKit priority."),
                    stringProperty("list_name", "Updated reminder list name."),
                    stringProperty("location", "Updated structured location title. Use an empty string to clear location."),
                    alarmsProperty(),
                ],
                required: ["identifier"]
            ),
            tool(
                name: "reminders_delete",
                description: "Delete a reminder by EventKit identifier.",
                properties: [stringProperty("identifier", "Reminder identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "reminders_list",
                description: "List reminders filtered by list, completion status, and due-date range.",
                properties: [
                    stringProperty("list_name", "Optional reminder list name filter."),
                    boolProperty("completed", "Optional completion-status filter."),
                    stringProperty("start_date", "Optional inclusive due-date lower bound in ISO-8601 format."),
                    stringProperty("end_date", "Optional inclusive due-date upper bound in ISO-8601 format."),
                    stringProperty("updated_after", "Optional lower-bound updated_at timestamp in ISO-8601 format."),
                ]
            ),
            tool(
                name: "reminders_default_list",
                description: "Return the default Reminder list used for newly created reminders.",
                properties: []
            ),
            tool(
                name: "reminders_wait_change",
                description: "Block until a reminder create, update, or delete matches the requested filters.",
                properties: [
                    stringProperty("list_name", "Optional reminder list name filter."),
                    stringArrayProperty("change_types", "Optional array containing create, update, and/or delete."),
                    numberProperty("timeout_seconds", "Optional timeout in seconds. Returns timed_out when reached."),
                ]
            ),
            tool(
                name: "events_create",
                description: "Create a calendar event with title, notes, date range, calendar, location, and alarms.",
                properties: [
                    stringProperty("title", "Event title."),
                    stringProperty("notes", "Optional notes."),
                    stringProperty("start_date", "Start date in ISO-8601 format."),
                    stringProperty("end_date", "End date in ISO-8601 format."),
                    boolProperty("all_day", "Whether the event is all-day."),
                    stringProperty("calendar_name", "Optional calendar name."),
                    stringProperty("location", "Optional location string."),
                    alarmsProperty(),
                ],
                required: ["title", "start_date", "end_date"]
            ),
            tool(
                name: "events_get",
                description: "Fetch a calendar event by EventKit identifier.",
                properties: [stringProperty("identifier", "Event identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "events_update",
                description: "Update a calendar event by EventKit identifier.",
                properties: [
                    stringProperty("identifier", "Event identifier."),
                    stringProperty("title", "Updated title."),
                    stringProperty("notes", "Updated notes. Use an empty string to clear notes."),
                    stringProperty("start_date", "Updated start date in ISO-8601 format."),
                    stringProperty("end_date", "Updated end date in ISO-8601 format."),
                    boolProperty("all_day", "Updated all-day flag."),
                    stringProperty("calendar_name", "Updated calendar name."),
                    stringProperty("location", "Updated location. Use an empty string to clear location."),
                    alarmsProperty(),
                ],
                required: ["identifier"]
            ),
            tool(
                name: "events_delete",
                description: "Delete a calendar event by EventKit identifier.",
                properties: [stringProperty("identifier", "Event identifier.")],
                required: ["identifier"]
            ),
            tool(
                name: "events_list",
                description: "List calendar events filtered by calendar name and date range.",
                properties: [
                    stringProperty("calendar_name", "Optional calendar name filter."),
                    stringProperty("start_date", "Optional start-date lower bound in ISO-8601 format."),
                    stringProperty("end_date", "Optional end-date upper bound in ISO-8601 format."),
                ]
            ),
            tool(
                name: "events_wait_change",
                description: "Block until a calendar event create, update, or delete matches the requested filters.",
                properties: [
                    stringProperty("calendar_name", "Optional calendar name filter."),
                    stringProperty("start_date", "Optional start-date lower bound in ISO-8601 format."),
                    stringProperty("end_date", "Optional end-date upper bound in ISO-8601 format."),
                    stringArrayProperty("change_types", "Optional array containing create, update, and/or delete."),
                    numberProperty("timeout_seconds", "Optional timeout in seconds. Returns timed_out when reached."),
                ]
            ),
            tool(
                name: "notify_send",
                description: "Send a standard macOS user notification banner.",
                properties: [
                    stringProperty("title", "Notification title."),
                    stringProperty("body", "Notification body."),
                    stringProperty("subtitle", "Optional subtitle."),
                    stringProperty("url", "Optional URL to open when the notification is clicked."),
                ],
                required: ["title", "body"]
            ),
            tool(
                name: "notify_send_interactive",
                description: "Send an interactive macOS notification with action buttons, optionally waiting for a response.",
                properties: [
                    stringProperty("title", "Notification title."),
                    stringProperty("body", "Notification body."),
                    stringProperty("subtitle", "Optional subtitle."),
                    stringProperty("url", "Optional URL to open when the notification (default action) is clicked."),
                    actionsProperty(),
                    boolProperty("wait_for_response", "When true, block until the user responds or the timeout elapses."),
                    numberProperty("timeout_seconds", "Timeout in seconds for wait_for_response mode. Defaults to 60."),
                    stringProperty("notification_id", "Optional stable notification identifier. Defaults to a generated UUID."),
                ],
                required: ["title", "body", "actions"]
            ),
            tool(
                name: "photos_album_assets",
                description: "List photo assets in a Photos album by album name.",
                properties: [
                    stringProperty("album_name", "Photos album name."),
                    numberProperty("limit", "Optional maximum number of assets to return, newest first."),
                ],
                required: ["album_name"]
            ),
            tool(
                name: "photos_export_assets",
                description: "Export one or more Photos assets to a local directory and return the written paths.",
                properties: [
                    stringArrayProperty("asset_ids", "Array of Photos asset local identifiers."),
                    stringProperty("destination_dir", "Local destination directory for exported files."),
                ],
                required: ["asset_ids", "destination_dir"]
            ),
            tool(
                name: "photos_album_remove_assets",
                description: "Remove one or more assets from a Photos album without deleting them from the library.",
                properties: [
                    stringProperty("album_name", "Photos album name."),
                    stringArrayProperty("asset_ids", "Array of Photos asset local identifiers to remove from the album."),
                ],
                required: ["album_name", "asset_ids"]
            ),
            tool(
                name: "photos_album_add_assets",
                description: "Add one or more assets to a Photos album.",
                properties: [
                    stringProperty("album_name", "Photos album name."),
                    stringArrayProperty("asset_ids", "Array of Photos asset local identifiers to add to the album."),
                ],
                required: ["album_name", "asset_ids"]
            ),
        ]
    }

    @MainActor
    func callTool(name: String, arguments: [String: Any]) async throws -> [String: Any] {
        switch name {
        case "reminders_create":
            return try toolResult(serializeReminder(try await createReminder(arguments)))
        case "reminders_get":
            return try toolResult(serializeReminder(try await reminder(identifier: requiredString(arguments, "identifier"))))
        case "reminders_update":
            return try toolResult(serializeReminder(try await updateReminder(arguments)))
        case "reminders_delete":
            return try toolResult(try await deleteReminder(arguments))
        case "reminders_list":
            return try toolResult(try await listReminders(arguments))
        case "reminders_default_list":
            return try toolResult(try await defaultReminderList())
        case "reminders_wait_change":
            return try toolResult(try await waitForReminderChange(arguments))
        case "events_create":
            return try toolResult(serializeEvent(try await createEvent(arguments)))
        case "events_get":
            return try toolResult(serializeEvent(try await event(identifier: requiredString(arguments, "identifier"))))
        case "events_update":
            return try toolResult(serializeEvent(try await updateEvent(arguments)))
        case "events_delete":
            return try toolResult(try await deleteEvent(arguments))
        case "events_list":
            return try toolResult(try await listEvents(arguments).map(serializeEvent))
        case "events_wait_change":
            return try toolResult(try await waitForEventChange(arguments))
        case "notify_send":
            return try toolResult(try await sendNotification(arguments))
        case "notify_send_interactive":
            return try toolResult(try await sendInteractiveNotification(arguments))
        case "photos_album_assets":
            return try toolResult(try await listPhotoAlbumAssets(arguments))
        case "photos_export_assets":
            return try toolResult(try await exportPhotoAssets(arguments))
        case "photos_album_remove_assets":
            return try toolResult(try await removePhotoAlbumAssets(arguments))
        case "photos_album_add_assets":
            return try toolResult(try await addPhotoAlbumAssets(arguments))
        default:
            throw MCPProtocolError.methodNotFound("unknown tool: \(name)")
        }
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        let identifier = response.notification.request.identifier
        let actionID = BridgeTools.normalizeNotificationAction(response.actionIdentifier)
        Task { @MainActor [weak self] in
            self?.handleNotificationResponse(notificationID: identifier, actionID: actionID)
        }
    }


    @MainActor
    private func handleEventStoreChanged() async {
        await pushReminderNotifications()
        await pushEventNotifications()
        await processPendingReminderWaits()
        await processPendingEventWaits()
    }

    // Proactively diff the reminder store and emit clara/reminders_changed for
    // every detected change. On the first call, we just establish the baseline
    // without emitting — there is nothing to diff against yet.
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
                server?.sendNotification(method: "clara/reminders_changed", params: payload)
            }
        } catch {
            // Silently ignore snapshot errors — permissions may not be granted yet.
        }
    }

    // Proactively diff the calendar store and emit clara/events_changed for
    // every detected change. Uses a rolling ±1 year window centred on now.
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
                server?.sendNotification(method: "clara/events_changed", params: payload)
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

        server?.sendNotification(method: "clara/notification_action", params: payload)
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
                            if startDate != nil && reminder.dueDateComponents?.date == nil {
                                return false
                            }
                            if let endDate, let dueDate = reminder.dueDateComponents?.date, dueDate > endDate {
                                return false
                            }
                            if endDate != nil && reminder.dueDateComponents?.date == nil {
                                return false
                            }
                            if let updatedAfter, let updatedAt = reminder.lastModifiedDate, updatedAt < updatedAfter {
                                return false
                            }
                            if updatedAfter != nil && reminder.lastModifiedDate == nil {
                                return false
                            }
                            return true
                        }
                        let serialized = reminders.map(self.serializeReminder)
                        continuation.resume(returning: try JSONSerialization.data(withJSONObject: serialized))
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
        let destinationDir = expandPath(try requiredString(args, "destination_dir"))
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

    private func waitForReminderChange(_ args: [String: Any]) async throws -> [String: Any] {
        try await ensureRemindersAccess()
        let listName = optionalString(args, "list_name")
        let changeTypes = try parseChangeTypes(args)
        let timeoutSeconds = optionalDouble(args, "timeout_seconds") ?? 0
        let waitID = UUID().uuidString
        let baseline = try await reminderSnapshot(listName: listName)

        let data = await withCheckedContinuation { continuation in
            let timeoutTask = makeStoreWaitTimeoutTask(
                waitID: waitID,
                timeoutSeconds: timeoutSeconds,
                kind: "reminders"
            )
            pendingReminderWaits[waitID] = PendingStoreWait(
                continuation: OneShotContinuation(continuation: continuation),
                timeoutTask: timeoutTask,
                changeTypes: changeTypes,
                filterName: listName,
                startDate: nil,
                endDate: nil,
                baseline: baseline
            )
        }
        return try decodeJSONObject(data)
    }

    private func waitForEventChange(_ args: [String: Any]) async throws -> [String: Any] {
        try await ensureEventsAccess()
        let calendarName = optionalString(args, "calendar_name")
        let changeTypes = try parseChangeTypes(args)
        let timeoutSeconds = optionalDouble(args, "timeout_seconds") ?? 0
        let startDate = try optionalDate(args, "start_date")
            ?? Calendar.current.date(byAdding: .year, value: -1, to: Date())!
        let endDate = try optionalDate(args, "end_date")
            ?? Calendar.current.date(byAdding: .year, value: 1, to: Date())!
        let waitID = UUID().uuidString
        let baseline = try await eventSnapshot(calendarName: calendarName, startDate: startDate, endDate: endDate)

        let data = await withCheckedContinuation { continuation in
            let timeoutTask = makeStoreWaitTimeoutTask(
                waitID: waitID,
                timeoutSeconds: timeoutSeconds,
                kind: "events"
            )
            pendingEventWaits[waitID] = PendingStoreWait(
                continuation: OneShotContinuation(continuation: continuation),
                timeoutTask: timeoutTask,
                changeTypes: changeTypes,
                filterName: calendarName,
                startDate: startDate,
                endDate: endDate,
                baseline: baseline
            )
        }
        return try decodeJSONObject(data)
    }

    private func processPendingReminderWaits() async {
        guard !pendingReminderWaits.isEmpty else {
            return
        }
        for waitID in Array(pendingReminderWaits.keys) {
            guard var wait = pendingReminderWaits[waitID] else {
                continue
            }
            do {
                let current = try await reminderSnapshot(listName: wait.filterName)
                guard let refreshedWait = pendingReminderWaits[waitID] else {
                    continue
                }
                wait = refreshedWait
                let changes = diffSnapshots(old: wait.baseline, new: current)
                if let matched = firstMatchingChange(changes: changes, allowedTypes: wait.changeTypes) {
                    let payload = waitPayload(
                        resource: "reminder",
                        changeType: matched.type,
                        filterKey: "list_name",
                        filterValue: wait.filterName,
                        item: matched.item
                    )
                    server?.sendNotification(method: "clara/reminders_changed", params: payload)
                    completePendingReminderWait(waitID: waitID, payload: payload)
                    continue
                }
                wait.baseline = current
                pendingReminderWaits[waitID] = wait
            } catch {
                let payload = errorWaitPayload(resource: "reminder", error: error, context: "reminders_wait_change")
                server?.sendNotification(method: "clara/reminders_changed", params: payload)
                completePendingReminderWait(waitID: waitID, payload: payload)
            }
        }
    }

    private func processPendingEventWaits() async {
        guard !pendingEventWaits.isEmpty else {
            return
        }
        for waitID in Array(pendingEventWaits.keys) {
            guard var wait = pendingEventWaits[waitID],
                  let startDate = wait.startDate,
                  let endDate = wait.endDate else {
                continue
            }
            do {
                let current = try await eventSnapshot(
                    calendarName: wait.filterName,
                    startDate: startDate,
                    endDate: endDate
                )
                guard let refreshedWait = pendingEventWaits[waitID] else {
                    continue
                }
                wait = refreshedWait
                let changes = diffSnapshots(old: wait.baseline, new: current)
                if let matched = firstMatchingChange(changes: changes, allowedTypes: wait.changeTypes) {
                    let payload = waitPayload(
                        resource: "event",
                        changeType: matched.type,
                        filterKey: "calendar_name",
                        filterValue: wait.filterName,
                        item: matched.item
                    )
                    server?.sendNotification(method: "clara/events_changed", params: payload)
                    completePendingEventWait(waitID: waitID, payload: payload)
                    continue
                }
                wait.baseline = current
                pendingEventWaits[waitID] = wait
            } catch {
                let payload = errorWaitPayload(resource: "event", error: error, context: "events_wait_change")
                server?.sendNotification(method: "clara/events_changed", params: payload)
                completePendingEventWait(waitID: waitID, payload: payload)
            }
        }
    }

    private func makeStoreWaitTimeoutTask(waitID: String, timeoutSeconds: Double, kind: String) -> Task<Void, Never>? {
        guard timeoutSeconds > 0 else {
            return nil
        }
        return Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(max(timeoutSeconds, 0.1) * 1_000_000_000))
            await MainActor.run {
                guard let self else { return }
                switch kind {
                case "reminders":
                    guard let pending = self.pendingReminderWaits.removeValue(forKey: waitID) else { return }
                    let payload = [
                        "status": "timed_out",
                        "resource": "reminder",
                        "changed_at": ISO8601.dateString(from: Date()),
                    ]
                    _ = self.resumePendingStoreWait(pending, payload: payload)
                case "events":
                    guard let pending = self.pendingEventWaits.removeValue(forKey: waitID) else { return }
                    let payload = [
                        "status": "timed_out",
                        "resource": "event",
                        "changed_at": ISO8601.dateString(from: Date()),
                    ]
                    _ = self.resumePendingStoreWait(pending, payload: payload)
                default:
                    return
                }
            }
        }
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

    private func ensureNotificationRuntimeAvailable() throws {
        let executableURL = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
        var candidate = executableURL.deletingLastPathComponent()
        while candidate.path != "/" && !candidate.path.isEmpty {
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
                "(for example /usr/local/libexec/ClaraBridge.app/Contents/MacOS/ClaraBridge).",
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
                reminder.dueDateComponents = Calendar.current.dateComponents(in: .current, from: try parseDate(duePresence))
            }
        }
        if let completed = optionalBool(args, "completed") {
            reminder.isCompleted = completed
            reminder.completionDate = completed ? (try optionalDate(args, "completion_date") ?? Date()) : nil
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
        notificationCenter().setNotificationCategories(Set(notificationCategories.values))
    }

    private func scheduleNotification(identifier: String, content: UNMutableNotificationContent) async throws {
        let request = UNNotificationRequest(identifier: identifier, content: content, trigger: nil)
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            notificationCenter().add(request) { error in
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

    nonisolated private static func normalizeNotificationAction(_ identifier: String) -> String {
        switch identifier {
        case UNNotificationDefaultActionIdentifier:
            return "default"
        case UNNotificationDismissActionIdentifier:
            return "dismissed"
        default:
            return identifier
        }
    }

    private func serializeReminder(_ reminder: EKReminder) -> [String: Any] {
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

    private func serializeEvent(_ event: EKEvent) -> [String: Any] {
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

    private func serializePhotoAsset(_ asset: PHAsset) -> [String: Any] {
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

    nonisolated private func performPhotoLibraryChanges(
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

    nonisolated private func writePhotoResource(_ resource: PHAssetResource, toFile destinationURL: URL) async throws {
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
        for index in 1...999 {
            let fileName = ext.isEmpty ? "\(stem)-\(index)" : "\(stem)-\(index).\(ext)"
            let next = directory.appendingPathComponent(fileName)
            if !FileManager.default.fileExists(atPath: next.path) {
                return next
            }
        }
        return directory.appendingPathComponent(UUID().uuidString + "-" + preferredName)
    }

    func assetMediaTypeName(_ mediaType: PHAssetMediaType) -> String {
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

    private func serializeAlarm(_ alarm: EKAlarm) -> [String: Any] {
        if let absoluteDate = alarm.absoluteDate {
            return ["absolute_date": ISO8601.dateString(from: absoluteDate)]
        }
        return ["relative_offset": alarm.relativeOffset]
    }

    private func parseAlarms(_ raw: Any?) throws -> [EKAlarm]? {
        guard let raw else {
            return nil
        }
        guard let rawAlarms = raw as? [Any] else {
            throw MCPToolError("alarms must be an array")
        }
        return try rawAlarms.map { entry in
            if let dateString = entry as? String {
                return EKAlarm(absoluteDate: try parseDate(dateString))
            }
            guard let map = entry as? [String: Any] else {
                throw MCPToolError("alarm entries must be ISO-8601 strings or objects")
            }
            if let absoluteDate = map["absolute_date"] as? String {
                return EKAlarm(absoluteDate: try parseDate(absoluteDate))
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
            ]
        ]
    }

    private func alarmsProperty() -> [String: [String: Any]] {
        [
            "alarms": [
                "type": "array",
                "description": "Optional list of alarms. Each entry may be an ISO-8601 string or an object with absolute_date or relative_offset.",
            ]
        ]
    }

    private func actionsProperty() -> [String: [String: Any]] {
        [
            "actions": [
                "type": "array",
                "description": "List of action objects with id, title, and optional foreground/destructive booleans.",
            ]
        ]
    }

    private func requiredString(_ args: [String: Any], _ key: String) throws -> String {
        guard let value = args[key] as? String, !value.isEmpty else {
            throw MCPToolError("\(key) is required")
        }
        return value
    }

    private func requiredStringArray(_ args: [String: Any], _ key: String) throws -> [String] {
        guard let values = optionalStringArray(args, key), !values.isEmpty else {
            throw MCPToolError("\(key) is required")
        }
        return values
    }

    private func optionalString(_ args: [String: Any], _ key: String) -> String? {
        args[key] as? String
    }

    private func optionalStringPresence(_ args: [String: Any], _ key: String) -> String? {
        guard args.keys.contains(key) else {
            return nil
        }
        return args[key] as? String ?? ""
    }

    private func optionalStringArray(_ args: [String: Any], _ key: String) -> [String]? {
        guard let raw = args[key] as? [Any] else {
            return nil
        }
        return raw.compactMap { $0 as? String }
    }

    private func optionalBool(_ args: [String: Any], _ key: String) -> Bool? {
        if let value = args[key] as? Bool {
            return value
        }
        if let number = args[key] as? NSNumber {
            return number.boolValue
        }
        return nil
    }

    private func optionalDouble(_ args: [String: Any], _ key: String) -> Double? {
        if let value = args[key] as? Double {
            return value
        }
        if let value = args[key] as? Int {
            return Double(value)
        }
        return nil
    }

    private func optionalInt(_ args: [String: Any], _ key: String) -> Int? {
        if let value = args[key] as? Int {
            return value
        }
        if let value = args[key] as? Double {
            return Int(value)
        }
        return nil
    }

    func expandPath(_ raw: String) -> String {
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

    private func parseDate(_ raw: String) throws -> Date {
        if let parsed = ISO8601.parse(raw) {
            return parsed
        }
        throw MCPToolError("invalid ISO-8601 date: \(raw)")
    }

    private func toolResult(_ value: Any) throws -> [String: Any] {
        let jsonData = try JSONSerialization.data(withJSONObject: value)
        let fallbackText = String(data: jsonData, encoding: .utf8) ?? "null"
        return [
            "content": [
                [
                    "type": "text",
                    "text": fallbackText,
                ]
            ],
            "structuredContent": value,
        ]
    }

    private func encodeJSONObject(_ value: Any) throws -> Data {
        try JSONSerialization.data(withJSONObject: value)
    }

    private func decodeJSONObject(_ data: Data) throws -> [String: Any] {
        guard let result = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw MCPToolError("failed to decode wait result")
        }
        return result
    }

    private func completePendingReminderWait(waitID: String, payload: [String: Any]) {
        guard let pending = pendingReminderWaits.removeValue(forKey: waitID) else {
            return
        }
        pending.timeoutTask?.cancel()
        _ = resumePendingStoreWait(pending, payload: payload)
    }

    private func completePendingEventWait(waitID: String, payload: [String: Any]) {
        guard let pending = pendingEventWaits.removeValue(forKey: waitID) else {
            return
        }
        pending.timeoutTask?.cancel()
        _ = resumePendingStoreWait(pending, payload: payload)
    }

    @discardableResult
    private func resumePendingStoreWait(_ pending: PendingStoreWait, payload: [String: Any]) -> Bool {
        guard let payloadData = try? encodeJSONObject(payload) else {
            return pending.continuation.resume(returning: .fallbackWaitEncodingError())
        }
        return pending.continuation.resume(returning: payloadData)
    }

    private func errorWaitPayload(resource: String, error: Error, context: String) -> [String: Any] {
        let bridgeError = MCPToolError.wrapping(error, context: context)
        var debug: [String: Any] = [:]
        if let errorType = bridgeError.errorType {
            debug["error_type"] = errorType
        }
        if let context = bridgeError.context {
            debug["context"] = context
        }
        if !bridgeError.callStack.isEmpty {
            debug["call_stack"] = bridgeError.callStack
        }
        return [
            "status": "error",
            "resource": resource,
            "message": bridgeError.message,
            "changed_at": ISO8601.dateString(from: Date()),
            "debug": debug,
        ]
    }
}

private struct PhotoToolError: LocalizedError {
    let message: String

    init(_ message: String) {
        self.message = message
    }

    var errorDescription: String? { message }
}

private struct PendingInteractiveNotification {
    let continuation: CheckedContinuation<[String: Any], Never>?
    let timeoutTask: Task<Void, Never>?
    let url: String?
}

private struct PendingStoreWait {
    let continuation: OneShotContinuation<Data>
    let timeoutTask: Task<Void, Never>?
    let changeTypes: Set<String>
    let filterName: String?
    let startDate: Date?
    let endDate: Date?
    var baseline: [String: StoreSnapshotItem]
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
        self.errorType = nil
        self.context = nil
        self.callStack = []
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

    var errorDescription: String? { message }

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
                ]
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
        guard let resumeHandler else {
            return false
        }
        self.resumeHandler = nil
        resumeHandler(value)
        return true
    }
}

private extension Data {
    static func fallbackWaitEncodingError() -> Data {
        Data("{\"status\":\"error\",\"message\":\"failed to encode wait payload\"}".utf8)
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
        case .invalidRequest(let message), .methodNotFound(let message), .invalidParams(let message), .internalError(let message):
            return message
        }
    }
}

private enum ISO8601 {
    static func makeFormatter(withFractionalSeconds: Bool) -> ISO8601DateFormatter {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = withFractionalSeconds
            ? [.withInternetDateTime, .withFractionalSeconds]
            : [.withInternetDateTime]
        return formatter
    }

    static func dateString(from date: Date) -> String {
        makeFormatter(withFractionalSeconds: true).string(from: date)
    }

    static func parse(_ raw: String) -> Date? {
        makeFormatter(withFractionalSeconds: true).date(from: raw)
            ?? makeFormatter(withFractionalSeconds: false).date(from: raw)
    }
}
