// swift-tools-version:5.5
// Root-level manifest so the SDK is resolvable via the monorepo's git URL:
//
//   .package(url: "https://github.com/MarveleE/agent-logger.git", from: "0.1.0")
//
// Sources/Tests live under sdks/swift/. The companion manifest at
// sdks/swift/Package.swift remains the entry point for local-path consumers
// (demo app, Makefile, "Add Local…" in Xcode).

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
            path: "sdks/swift/Sources/AgentLogger"
        ),
        .testTarget(
            name: "AgentLoggerTests",
            dependencies: ["AgentLogger"],
            path: "sdks/swift/Tests/AgentLoggerTests"
        ),
    ]
)
