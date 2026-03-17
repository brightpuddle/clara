// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ClaraBridge",
    platforms: [.macOS(.v15)],
    products: [
        .executable(name: "ClaraBridge", targets: ["ClaraBridge"]),
    ],
    targets: [
        .executableTarget(
            name: "ClaraBridge",
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
