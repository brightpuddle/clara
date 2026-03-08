import GRPCCore
import GRPCProtobuf
import GRPCNIOTransportHTTP2
import Foundation

/// Implements the NativeWorkerService gRPC service.
@available(macOS 15.0, *)
struct NativeWorkerServiceImpl: Native_V1_NativeWorkerService.SimpleServiceProtocol {
    private let reminders: RemindersManager
    private let spotlight: SpotlightManager

    init(reminders: RemindersManager, spotlight: SpotlightManager) {
        self.reminders = reminders
        self.spotlight = spotlight
    }

    func listReminders(
        request: Native_V1_ListRemindersRequest,
        context: GRPCCore.ServerContext
    ) async throws -> Native_V1_ListRemindersResponse {
        let all = try await reminders.listIncompleteReminders()
        var resp = Native_V1_ListRemindersResponse()
        resp.reminders = request.includeCompleted ? all : all.filter { !$0.completed }
        return resp
    }

    func markReminderDone(
        request: Native_V1_MarkReminderDoneRequest,
        context: GRPCCore.ServerContext
    ) async throws -> Native_V1_MarkReminderDoneResponse {
        var resp = Native_V1_MarkReminderDoneResponse()
        do {
            try reminders.markDone(id: request.id)
            resp.ok = true
        } catch {
            resp.ok = false
            resp.error = error.localizedDescription
        }
        return resp
    }

    func updateReminder(
        request: Native_V1_UpdateReminderRequest,
        context: GRPCCore.ServerContext
    ) async throws -> Native_V1_UpdateReminderResponse {
        var resp = Native_V1_UpdateReminderResponse()
        do {
            try await reminders.updateReminder(
                id: request.id,
                title: request.hasTitle ? request.title : nil,
                notes: request.hasNotes ? request.notes : nil,
                dueDate: request.hasDueDate ? request.dueDate.date : nil
            )
            resp.ok = true
        } catch {
            resp.ok = false
            resp.error = error.localizedDescription
        }
        return resp
    }

    func spotlightSearch(
        request: Native_V1_SpotlightSearchRequest,
        context: GRPCCore.ServerContext
    ) async throws -> Native_V1_SpotlightSearchResponse {
        let results = await spotlight.search(
            query: request.query,
            maxResults: Int(request.maxResults > 0 ? request.maxResults : 20),
            scopes: request.scopes
        )
        var resp = Native_V1_SpotlightSearchResponse()
        resp.results = results
        return resp
    }

    func getSystemTheme(
        request: Native_V1_GetSystemThemeRequest,
        context: GRPCCore.ServerContext
    ) async throws -> Native_V1_GetSystemThemeResponse {
        let isDark = UserDefaults.standard.string(forKey: "AppleInterfaceStyle") == "Dark"
        var resp = Native_V1_GetSystemThemeResponse()
        resp.dark = isDark
        return resp
    }
}
