import XCTest
@testable import ClaraBridge

@MainActor
final class ClaraBridgeTests: XCTestCase {
    func testOneShotContinuationResumesOnlyOnce() {
        var values: [String] = []
        let continuation = OneShotContinuation<String> { value in
            values.append(value)
        }

        XCTAssertTrue(continuation.resume(returning: "first"))
        XCTAssertFalse(continuation.resume(returning: "second"))
        XCTAssertEqual(values, ["first"])
    }
}
