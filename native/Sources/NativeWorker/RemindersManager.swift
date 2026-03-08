@preconcurrency import EventKit
import Foundation
import SwiftProtobuf

/// Manages access to Apple Reminders via EventKit.
@available(macOS 15.0, *)
final class RemindersManager: @unchecked Sendable {
    private let store = EKEventStore()

    /// Requests EventKit authorization if not already granted.
    func requestAccess() async throws {
        let status = EKEventStore.authorizationStatus(for: .reminder)
        switch status {
        case .authorized, .fullAccess:
            return
        case .notDetermined:
            let granted = try await store.requestFullAccessToReminders()
            if !granted {
                throw RemindersError.accessDenied
            }
        default:
            throw RemindersError.accessDenied
        }
    }

    /// Returns all incomplete reminders across all reminder calendars.
    func listIncompleteReminders() async throws -> [Native_V1_Reminder] {
        let calendars = store.calendars(for: .reminder)
        let predicate = store.predicateForIncompleteReminders(
            withDueDateStarting: nil,
            ending: nil,
            calendars: calendars.isEmpty ? nil : calendars
        )

        return try await withCheckedThrowingContinuation { continuation in
            store.fetchReminders(matching: predicate) { ekReminders in
                guard let ekReminders else {
                    continuation.resume(returning: [])
                    return
                }
                let reminders = ekReminders.compactMap { ek -> Native_V1_Reminder? in
                    guard let calItemId = ek.calendarItemIdentifier as String? else { return nil }
                    var r = Native_V1_Reminder()
                    r.id = calItemId
                    r.title = ek.title ?? ""
                    r.notes = ek.notes ?? ""
                    r.listName = ek.calendar?.title ?? ""
                    r.completed = ek.isCompleted
                    r.priority = Int32(ek.priority)

                    if let created = ek.creationDate {
                        r.createdAt = Google_Protobuf_Timestamp(date: created)
                    }
                    if let modified = ek.lastModifiedDate {
                        r.modifiedAt = Google_Protobuf_Timestamp(date: modified)
                    }
                    if let due = ek.dueDateComponents?.date {
                        r.dueDate = Google_Protobuf_Timestamp(date: due)
                    }
                    return r
                }
                continuation.resume(returning: reminders)
            }
        }
    }

    /// Marks the reminder with the given EventKit calendar item identifier as complete.
    func markDone(id: String) throws {
        guard let reminder = store.calendarItem(withIdentifier: id) as? EKReminder else {
            throw RemindersError.notFound(id)
        }
        reminder.isCompleted = true
        try store.save(reminder, commit: true)
    }

    /// Updates the title, notes, and/or due date of a reminder.
    func updateReminder(id: String, title: String?, notes: String?, dueDate: Date?) async throws {
        guard let reminder = store.calendarItem(withIdentifier: id) as? EKReminder else {
            throw RemindersError.notFound(id)
        }
        if let title = title {
            reminder.title = title
        }
        if let notes = notes {
            reminder.notes = notes
        }
        if let dueDate = dueDate {
            var components = Calendar.current.dateComponents([.year, .month, .day, .hour, .minute, .second], from: dueDate)
            components.timeZone = TimeZone.current
            reminder.dueDateComponents = components
        }
        try store.save(reminder, commit: true)
    }
}

enum RemindersError: Error {
    case accessDenied
    case notFound(String)
}
