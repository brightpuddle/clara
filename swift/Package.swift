// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ClaraBridge",
    platforms: [.macOS(.v15)],
    products: [
        .executable(name: "ClaraBridge", targets: ["ClaraBridge"]),
    ],
    dependencies: [
        .package(url: "https://github.com/grpc/grpc-swift.git", from: "1.24.0"),
        .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.28.2"),
    ],
    targets: [
        .executableTarget(
            name: "ClaraBridge",
            dependencies: [
                .product(name: "GRPC", package: "grpc-swift"),
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            path: "Sources/ClaraBridge",
            exclude: ["Info.plist"],
            linkerSettings: [
                .unsafeFlags([
                    "-Xlinker", "-sectcreate",
                    "-Xlinker", "__TEXT",
                    "-Xlinker", "__info_plist",
                    "-Xlinker", "Sources/ClaraBridge/Info.plist",
                ])
            ]
        ),
        .testTarget(
            name: "ClaraBridgeTests",
            dependencies: ["ClaraBridge"],
            path: "Tests/ClaraBridgeTests"
        ),
    ]
)
