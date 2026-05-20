import Foundation

/// Configuration for `AgentLogger.bootstrap`.
///
/// Defaults are tuned for development use; all knobs can be overridden if you
/// need different batch sizes or backoff behavior.
public struct AgentLoggerConfiguration: Sendable {
    /// How to determine the server endpoint.
    public enum Mode: Sendable {
        /// Resolve at runtime: Info.plist `AgentLoggerEndpoint` → env var
        /// `AGENTLOGGER_ENDPOINT`. If neither is set (or both are unreachable),
        /// the logger silently disables transport until the next discovery
        /// attempt. There is no implicit localhost fallback — you must
        /// configure one of the two sources for the SDK to ship anything.
        case auto

        /// Use a specific endpoint, e.g. `http://localhost:8765`. Bypasses
        /// the auto chain entirely.
        case endpoint(URL)

        /// Disable everything. Useful for App Store / production builds.
        case disabled
    }

    public var mode: Mode
    public var queueCapacity: Int
    public var batchSize: Int
    public var batchInterval: TimeInterval
    public var httpTimeout: TimeInterval
    public var minimumLevel: LogLevel
    public var defaultCategory: String?
    public var sessionMetadata: [String: String]
    public var discoveryTimeout: TimeInterval

    public init(
        mode: Mode = .auto,
        queueCapacity: Int = 2048,
        batchSize: Int = 64,
        batchInterval: TimeInterval = 0.25,
        httpTimeout: TimeInterval = 10,
        minimumLevel: LogLevel = .trace,
        defaultCategory: String? = nil,
        sessionMetadata: [String: String] = [:],
        discoveryTimeout: TimeInterval = 3
    ) {
        self.mode = mode
        self.queueCapacity = queueCapacity
        self.batchSize = batchSize
        self.batchInterval = batchInterval
        self.httpTimeout = httpTimeout
        self.minimumLevel = minimumLevel
        self.defaultCategory = defaultCategory
        self.sessionMetadata = sessionMetadata
        self.discoveryTimeout = discoveryTimeout
    }

    public static let auto = AgentLoggerConfiguration(mode: .auto)
    public static let disabled = AgentLoggerConfiguration(mode: .disabled)

    public static func endpoint(_ url: URL) -> AgentLoggerConfiguration {
        AgentLoggerConfiguration(mode: .endpoint(url))
    }
}
