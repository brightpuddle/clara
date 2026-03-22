import XCTest
import Photos
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

    func testAssetMediaTypeNameMapsKnownTypes() {
        let tools = BridgeTools()

        XCTAssertEqual(tools.assetMediaTypeName(PHAssetMediaType.image), "image")
        XCTAssertEqual(tools.assetMediaTypeName(PHAssetMediaType.video), "video")
        XCTAssertEqual(tools.assetMediaTypeName(PHAssetMediaType.audio), "audio")
        XCTAssertEqual(tools.assetMediaTypeName(PHAssetMediaType.unknown), "unknown")
    }

    func testExpandPathExpandsTildeAndEnvironmentVariables() {
        let tools = BridgeTools()
        let home = FileManager.default.homeDirectoryForCurrentUser.path

        XCTAssertEqual(tools.expandPath("~/tmp"), home + "/tmp")
        XCTAssertEqual(tools.expandPath("${HOME}/tmp"), home + "/tmp")
    }
}
