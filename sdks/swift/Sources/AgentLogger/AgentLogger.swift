import Foundation

/// AgentLogger is a zero-dependency log shipper for iOS/macOS that talks to a
/// local `agentlog` daemon over HTTP.
///
/// Typical usage:
///
///     import AgentLogger
///
///     // once at startup
///     AgentLogger.bootstrap()
///
///     // anywhere
///     AgentLogger.info("App launched")
///     AgentLogger.error("Decoding failed", error: err, metadata: ["url": url])
public enum AgentLogger {
    private static let lock = NSLock()
    private static var _core: Core?

    /// Configure the logger. Safe to call multiple times — subsequent calls
    /// are silently ignored once initialized.
    public static func bootstrap(_ configuration: AgentLoggerConfiguration = .auto) {
        lock.lock()
        defer { lock.unlock() }
        guard _core == nil else { return }
        let core = Core(configuration: configuration)
        core.start()
        _core = core
    }

    /// Tear down the logger. Useful for tests; rarely needed in app code.
    public static func shutdown() async {
        let core: Core? = {
            lock.lock(); defer { lock.unlock() }
            let c = _core; _core = nil; return c
        }()
        await core?.stop()
    }

    /// Returns the active session id, or nil if logger hasn't bootstrapped.
    public static var currentSessionID: String? {
        lock.lock(); defer { lock.unlock() }
        return _core?.session.id
    }

    /// Returns the active session descriptor (bundle id, device, etc.), or nil.
    public static var currentSession: SessionDescriptor? {
        lock.lock(); defer { lock.unlock() }
        return _core?.session
    }

    /// Returns a sub-logger that tags entries with the given category.
    public static func category(_ name: String) -> CategoryLogger {
        CategoryLogger(name: name)
    }

    // MARK: - Log methods (matching server LogLevel)

    public static func trace(
        _ message: @autoclosure () -> String,
        metadata: [String: String]? = nil,
        file: String = #file, line: Int = #line, function: String = #function
    ) {
        emit(.trace, message: message(), category: nil, metadata: metadata, file: file, line: line, function: function)
    }

    public static func debug(
        _ message: @autoclosure () -> String,
        metadata: [String: String]? = nil,
        file: String = #file, line: Int = #line, function: String = #function
    ) {
        emit(.debug, message: message(), category: nil, metadata: metadata, file: file, line: line, function: function)
    }

    public static func info(
        _ message: @autoclosure () -> String,
        metadata: [String: String]? = nil,
        file: String = #file, line: Int = #line, function: String = #function
    ) {
        emit(.info, message: message(), category: nil, metadata: metadata, file: file, line: line, function: function)
    }

    public static func notice(
        _ message: @autoclosure () -> String,
        metadata: [String: String]? = nil,
        file: String = #file, line: Int = #line, function: String = #function
    ) {
        emit(.notice, message: message(), category: nil, metadata: metadata, file: file, line: line, function: function)
    }

    public static func warning(
        _ message: @autoclosure () -> String,
        metadata: [String: String]? = nil,
        file: String = #file, line: Int = #line, function: String = #function
    ) {
        emit(.warning, message: message(), category: nil, metadata: metadata, file: file, line: line, function: function)
    }

    public static func error(
        _ message: @autoclosure () -> String,
        error: Error? = nil,
        metadata: [String: String]? = nil,
        file: String = #file, line: Int = #line, function: String = #function
    ) {
        var meta = metadata ?? [:]
        if let err = error {
            meta["errorType"] = String(describing: type(of: err))
            meta["errorDescription"] = (err as NSError).localizedDescription
        }
        emit(.error, message: message(), category: nil,
             metadata: meta.isEmpty ? nil : meta,
             file: file, line: line, function: function)
    }

    public static func critical(
        _ message: @autoclosure () -> String,
        metadata: [String: String]? = nil,
        file: String = #file, line: Int = #line, function: String = #function
    ) {
        emit(.critical, message: message(), category: nil, metadata: metadata, file: file, line: line, function: function)
    }

    // MARK: - Internal

    static func emit(
        _ level: LogLevel,
        message: String,
        category: String?,
        metadata: [String: String]?,
        file: String, line: Int, function: String
    ) {
        let core: Core? = {
            lock.lock(); defer { lock.unlock() }
            return _core
        }()
        core?.enqueue(level: level, message: message, category: category,
                      metadata: metadata, file: file, line: line, function: function)
    }
}

/// A sub-logger that tags entries with a category. Lifetime-cheap value type.
public struct CategoryLogger: Sendable {
    public let name: String

    public func trace(_ message: @autoclosure () -> String, metadata: [String: String]? = nil,
                      file: String = #file, line: Int = #line, function: String = #function) {
        AgentLogger.emit(.trace, message: message(), category: name, metadata: metadata, file: file, line: line, function: function)
    }
    public func debug(_ message: @autoclosure () -> String, metadata: [String: String]? = nil,
                      file: String = #file, line: Int = #line, function: String = #function) {
        AgentLogger.emit(.debug, message: message(), category: name, metadata: metadata, file: file, line: line, function: function)
    }
    public func info(_ message: @autoclosure () -> String, metadata: [String: String]? = nil,
                     file: String = #file, line: Int = #line, function: String = #function) {
        AgentLogger.emit(.info, message: message(), category: name, metadata: metadata, file: file, line: line, function: function)
    }
    public func notice(_ message: @autoclosure () -> String, metadata: [String: String]? = nil,
                       file: String = #file, line: Int = #line, function: String = #function) {
        AgentLogger.emit(.notice, message: message(), category: name, metadata: metadata, file: file, line: line, function: function)
    }
    public func warning(_ message: @autoclosure () -> String, metadata: [String: String]? = nil,
                        file: String = #file, line: Int = #line, function: String = #function) {
        AgentLogger.emit(.warning, message: message(), category: name, metadata: metadata, file: file, line: line, function: function)
    }
    public func error(_ message: @autoclosure () -> String, error: Error? = nil, metadata: [String: String]? = nil,
                      file: String = #file, line: Int = #line, function: String = #function) {
        var meta = metadata ?? [:]
        if let err = error {
            meta["errorType"] = String(describing: type(of: err))
            meta["errorDescription"] = (err as NSError).localizedDescription
        }
        AgentLogger.emit(.error, message: message(), category: name,
                         metadata: meta.isEmpty ? nil : meta,
                         file: file, line: line, function: function)
    }
    public func critical(_ message: @autoclosure () -> String, metadata: [String: String]? = nil,
                         file: String = #file, line: Int = #line, function: String = #function) {
        AgentLogger.emit(.critical, message: message(), category: name, metadata: metadata, file: file, line: line, function: function)
    }
}
