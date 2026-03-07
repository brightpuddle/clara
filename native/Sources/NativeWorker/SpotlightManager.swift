@preconcurrency import Foundation
import SwiftProtobuf

/// Manages Spotlight index searches via NSMetadataQuery.
@available(macOS 15.0, *)
@MainActor
final class SpotlightManager {

    /// Searches the Spotlight index for files matching the given query string.
    func search(query: String, maxResults: Int, scopes: [String]) async -> [Native_V1_SpotlightResult] {
        let mq = NSMetadataQuery()
        mq.predicate = NSPredicate(
            format: "kMDItemDisplayName CONTAINS[cd] %@ OR kMDItemTextContent CONTAINS[cd] %@",
            query, query
        )
        if scopes.isEmpty {
            mq.searchScopes = [NSMetadataQueryLocalComputerScope]
        } else {
            mq.searchScopes = scopes.map { URL(fileURLWithPath: $0) }
        }

        return await withCheckedContinuation { (continuation: CheckedContinuation<[Native_V1_SpotlightResult], Never>) in
            var observer: NSObjectProtocol?
            observer = NotificationCenter.default.addObserver(
                forName: .NSMetadataQueryDidFinishGathering,
                object: mq,
                queue: .main
            ) { [weak mq] _ in
                guard let mq else {
                    continuation.resume(returning: [])
                    return
                }
                mq.stop()
                if let obs = observer {
                    NotificationCenter.default.removeObserver(obs)
                }

                var results: [Native_V1_SpotlightResult] = []
                let limit = maxResults > 0 ? maxResults : 20
                let count = min(mq.resultCount, limit)
                for i in 0..<count {
                    guard let item = mq.result(at: i) as? NSMetadataItem else { continue }
                    var r = Native_V1_SpotlightResult()
                    r.path = (item.value(forAttribute: NSMetadataItemPathKey) as? String) ?? ""
                    r.displayName = (item.value(forAttribute: NSMetadataItemDisplayNameKey) as? String) ?? ""
                    r.contentType = (item.value(forAttribute: NSMetadataItemContentTypeKey) as? String) ?? ""
                    if let used = item.value(forAttribute: kMDItemLastUsedDate as String) as? Date {
                        r.lastUsed = Google_Protobuf_Timestamp(date: used)
                    }
                    results.append(r)
                }
                continuation.resume(returning: results)
            }
            mq.start()
        }
    }
}
