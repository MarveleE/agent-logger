// swift-tools-version:5.5
// AgentLogger client SDK — zero external dependencies, modern Swift Concurrency
// back-deployed to iOS 13 / macOS 10.15 via Xcode 13.2+.

import PackageDescription

let package = Package(
    name: "AgentLogger",
    platforms: [
        .iOS(.v13),
        .macOS(.v10_15),
        .tvOS(.v13),
        .watchOS(.v6),
    ],
    products: [
        .library(name: "AgentLogger", targets: ["AgentLogger"]),
    ],
    dependencies: [],
    targets: [
        .target(
            name: "AgentLogger",
            path: "Sources/AgentLogger"
        ),
        .testTarget(
            name: "AgentLoggerTests",
            dependencies: ["AgentLogger"],
            path: "Tests/AgentLoggerTests"
        ),
    ]
)
