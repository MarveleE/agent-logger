import Foundation

/// Bounded, drop-oldest channel for log entries.
///
/// Built on `AsyncStream` so callers can `yield(_:)` synchronously from any
/// thread without awaiting. The consumer pulls one entry at a time from the
/// underlying stream and batches them up downstream.
final class LogQueue: @unchecked Sendable {
    private let continuation: AsyncStream<LogEntry>.Continuation
    let stream: AsyncStream<LogEntry>
    private let drops = DropCounter()

    init(capacity: Int) {
        var cont: AsyncStream<LogEntry>.Continuation!
        // `.bufferingNewest(n)` keeps the newest N when the buffer is full —
        // i.e. drops the oldest on overflow, which is what we want for logs.
        let stream = AsyncStream<LogEntry>(
            bufferingPolicy: .bufferingNewest(max(1, capacity))
        ) { c in
            cont = c
        }
        self.stream = stream
        self.continuation = cont
    }

    /// Append an entry. Synchronous and safe to call from any thread.
    /// When the buffer is full the oldest entry is dropped silently and the
    /// drop counter is incremented.
    func send(_ entry: LogEntry) {
        let result = continuation.yield(entry)
        switch result {
        case .dropped:
            drops.bump()
        case .enqueued, .terminated:
            break
        @unknown default:
            break
        }
    }

    /// Signal end-of-stream — used on shutdown to release the consumer.
    func finish() {
        continuation.finish()
    }

    /// Return and reset the drop counter.
    func consumeDroppedCount() -> Int {
        drops.consumeAll()
    }
}

/// Thread-safe counter for dropped log entries.
final class DropCounter: @unchecked Sendable {
    private var value: Int = 0
    private let lock = NSLock()

    func bump() {
        lock.lock()
        value += 1
        lock.unlock()
    }

    func consumeAll() -> Int {
        lock.lock()
        let v = value
        value = 0
        lock.unlock()
        return v
    }
}
