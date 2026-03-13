// ClaraBridge — Swift native macOS MCP server.
//
// This executable speaks MCP over stdio and exposes native macOS capabilities
// such as EventKit-backed reminders, calendar events, and user notifications.

@preconcurrency import EventKit
import Foundation
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
                try writeResult(id: requestID, result: MCPToolError(error.localizedDescription).resultPayload)
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

    private func logError(_ message: String) {
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
    private var notificationsAuthorized = false
    private var eventStoreObserver: NSObjectProtocol?
    private var pendingInteractiveNotifications: [String: PendingInteractiveNotification] = [:]
    private var notificationCategories: [String: UNNotificationCategory] = [:]

    override init() {
        super.init()
        eventStoreObserver = NotificationCenter.default.addObserver(
            forName: .EKEventStoreChanged,
            object: eventStore,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.handleEventStoreChanged()
            }
        }
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
                name: "notify_send",
                description: "Send a standard macOS user notification banner.",
                properties: [
                    stringProperty("title", "Notification title."),
                    stringProperty("body", "Notification body."),
                    stringProperty("subtitle", "Optional subtitle."),
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
                    actionsProperty(),
                    boolProperty("wait_for_response", "When true, block until the user responds or the timeout elapses."),
                    numberProperty("timeout_seconds", "Timeout in seconds for wait_for_response mode. Defaults to 60."),
                    stringProperty("notification_id", "Optional stable notification identifier. Defaults to a generated UUID."),
                ],
                required: ["title", "body", "actions"]
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
        case "notify_send":
            return try toolResult(try await sendNotification(arguments))
        case "notify_send_interactive":
            return try toolResult(try await sendInteractiveNotification(arguments))
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
    private func handleEventStoreChanged() {
        server?.sendNotification(method: "clara/eventkit_changed", params: [
            "changed_at": ISO8601.dateString(from: Date()),
        ])
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
        let content = UNMutableNotificationContent()
        content.title = try requiredString(args, "title")
        content.body = try requiredString(args, "body")
        if let subtitle = optionalString(args, "subtitle") {
            content.subtitle = subtitle
        }
        content.sound = .default

        try await scheduleNotification(identifier: identifier, content: content)
        return ["notification_id": identifier, "status": "sent"]
    }

    private func sendInteractiveNotification(_ args: [String: Any]) async throws -> [String: Any] {
        try await ensureNotificationsAccess()
        let identifier = optionalString(args, "notification_id") ?? UUID().uuidString
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
            pendingInteractiveNotifications[identifier] = PendingInteractiveNotification(continuation: nil, timeoutTask: nil)
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
                timeoutTask: timeoutTask
            )
        }
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

    private func optionalString(_ args: [String: Any], _ key: String) -> String? {
        args[key] as? String
    }

    private func optionalStringPresence(_ args: [String: Any], _ key: String) -> String? {
        guard args.keys.contains(key) else {
            return nil
        }
        return args[key] as? String ?? ""
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
}

private struct PendingInteractiveNotification {
    let continuation: CheckedContinuation<[String: Any], Never>?
    let timeoutTask: Task<Void, Never>?
}

private struct MCPToolError: LocalizedError {
    let message: String

    init(_ message: String) {
        self.message = message
    }

    var errorDescription: String? { message }

    var resultPayload: [String: Any] {
        [
            "content": [["type": "text", "text": message]],
            "isError": true,
        ]
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
