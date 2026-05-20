import Foundation

/// Severity for a log entry. Wire format matches the server: lowercase string.
public enum LogLevel: Int, Codable, Sendable, CaseIterable {
    case trace = 0
    case debug = 1
    case info = 2
    case notice = 3
    case warning = 4
    case error = 5
    case critical = 6

    public var name: String {
        switch self {
        case .trace: return "trace"
        case .debug: return "debug"
        case .info: return "info"
        case .notice: return "notice"
        case .warning: return "warning"
        case .error: return "error"
        case .critical: return "critical"
        }
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let raw = try? container.decode(Int.self), let level = LogLevel(rawValue: raw) {
            self = level
            return
        }
        let name = try container.decode(String.self).lowercased()
        switch name {
        case "trace": self = .trace
        case "debug": self = .debug
        case "info": self = .info
        case "notice": self = .notice
        case "warning", "warn": self = .warning
        case "error": self = .error
        case "critical", "fatal": self = .critical
        default:
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Unknown level: \(name)")
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(name)
    }
}

/// One log line. `seq` is per-session monotonic and provides idempotency on retries.
public struct LogEntry: Codable, Sendable {
    public let seq: Int64
    public let ts: Int64           // unix milliseconds
    public let level: LogLevel
    public let category: String?
    public let message: String
    public let metadata: [String: String]?
    public let file: String?
    public let line: Int?
    public let function: String?

    public init(
        seq: Int64,
        ts: Int64,
        level: LogLevel,
        category: String?,
        message: String,
        metadata: [String: String]?,
        file: String?,
        line: Int?,
        function: String?
    ) {
        self.seq = seq
        self.ts = ts
        self.level = level
        self.category = category
        self.message = message
        self.metadata = metadata
        self.file = file
        self.line = line
        self.function = function
    }

    private enum CodingKeys: String, CodingKey {
        case seq, ts, level, category, message, metadata, file, line
        case function = "func"
    }
}
