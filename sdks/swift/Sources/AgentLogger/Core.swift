import Foundation

#if canImport(UIKit) && !os(watchOS)
import UIKit
#endif

/// Wire format for `POST /v1/sessions/{id}/logs`.
private struct LogBatchWire: Encodable {
    let batch: [LogEntry]
}

/// Internal buffer drained by the periodic flusher. Separate from `LogQueue`
/// so that the reader task can keep pace with the AsyncStream while the
/// flusher operates on a snapshot.
actor BatchBuffer {
    private var pending: [LogEntry] = []

    func append(_ entry: LogEntry) {
        pending.append(entry)
    }

    func prepend(_ entries: [LogEntry]) {
        pending.insert(contentsOf: entries, at: 0)
    }

    func drain(maxSize: Int) -> [LogEntry] {
        let n = min(maxSize, pending.count)
        guard n > 0 else { return [] }
        let out = Array(pending.prefix(n))
        pending.removeFirst(n)
        return out
    }

    func count() -> Int { pending.count }
}

/// Coordinates the queue, discovery, session registration, and the batch
/// sender loop. Created once by `AgentLogger.bootstrap`.
final class Core: @unchecked Sendable {
    let configuration: AgentLoggerConfiguration
    let queue: LogQueue
    let session: SessionDescriptor
    private let transport: HTTPTransport
    private let seqState = AtomicCounter()
    private var senderTask: Task<Void, Never>?
    private var lifecycleObservers: [NSObjectProtocol] = []

    init(configuration: AgentLoggerConfiguration) {
        self.configuration = configuration
        self.queue = LogQueue(capacity: configuration.queueCapacity)
        self.transport = HTTPTransport(timeout: configuration.httpTimeout)
        self.session = SessionFactory.makeDescriptor(extraMetadata: configuration.sessionMetadata)
    }

    func start() {
        if case .disabled = configuration.mode { return }
        senderTask = Task.detached(priority: .utility) { [weak self] in
            await self?.runSenderLoop()
        }
        installLifecycleHooks()
    }

    func stop() async {
        senderTask?.cancel()
        senderTask = nil
        queue.finish()
        removeLifecycleHooks()
    }

    /// Enqueue an entry. Synchronous, non-blocking.
    func enqueue(level: LogLevel, message: @autoclosure () -> String, category: String?,
                 metadata: [String: String]?, file: String, line: Int, function: String) {
        guard level.rawValue >= configuration.minimumLevel.rawValue else { return }
        let entry = LogEntry(
            seq: seqState.increment(),
            ts: nowMillis(),
            level: level,
            category: category ?? configuration.defaultCategory,
            message: message(),
            metadata: metadata,
            file: shortFile(file),
            line: line,
            function: function
        )
        queue.send(entry)
    }

    // MARK: - Sender loop

    private func runSenderLoop() async {
        let buffer = BatchBuffer()

        // Reader task: drain the stream into the buffer as fast as it arrives.
        let stream = queue.stream
        let readerTask = Task.detached(priority: .utility) {
            for await entry in stream {
                if Task.isCancelled { break }
                await buffer.append(entry)
            }
        }
        defer { readerTask.cancel() }

        var endpoint: URL? = nil
        var registered = false
        var backoff = Backoff()

        while !Task.isCancelled {
            // 1. Resolve endpoint
            if endpoint == nil {
                endpoint = await Discovery.resolve(
                    configuration: configuration,
                    timeout: configuration.discoveryTimeout
                )
                if endpoint == nil {
                    await sleepSeconds(backoff.next())
                    continue
                }
                backoff.reset()
                registered = false
            }

            // 2. Register session
            if !registered, let url = endpoint {
                do {
                    try await registerSession(at: url)
                    registered = true
                    backoff.reset()
                } catch {
                    endpoint = nil
                    await sleepSeconds(backoff.next())
                    continue
                }
            }

            // 3. Wait for the flush tick (or short-circuit if buffer is already large)
            await sleepSeconds(configuration.batchInterval)
            let bufferDepth = await buffer.count()
            if bufferDepth == 0 { continue }

            // 4. Ship one batch
            var batch = await buffer.drain(maxSize: configuration.batchSize)
            let dropped = queue.consumeDroppedCount()
            if dropped > 0, !batch.isEmpty {
                let first = batch[0]
                var meta = first.metadata ?? [:]
                meta["_dropped"] = String(dropped)
                batch[0] = LogEntry(
                    seq: first.seq, ts: first.ts, level: first.level,
                    category: first.category, message: first.message,
                    metadata: meta, file: first.file, line: first.line, function: first.function
                )
            }

            guard let url = endpoint else { continue }
            let target = url.appendingPathComponent("v1/sessions/\(session.id)/logs")
            do {
                try await transport.post(target, body: LogBatchWire(batch: batch))
                backoff.reset()
            } catch {
                // Put the batch back at the front so we retry it before newer entries.
                await buffer.prepend(batch)
                endpoint = nil
                registered = false
                await sleepSeconds(backoff.next())
            }
        }

        // Final flush on cancellation.
        if registered, let url = endpoint {
            var leftover = await buffer.drain(maxSize: configuration.batchSize * 16)
            while !leftover.isEmpty {
                let chunk = Array(leftover.prefix(configuration.batchSize))
                leftover.removeFirst(chunk.count)
                let target = url.appendingPathComponent("v1/sessions/\(session.id)/logs")
                try? await transport.post(target, body: LogBatchWire(batch: chunk))
            }
        }
    }

    private func registerSession(at endpoint: URL) async throws {
        let url = endpoint.appendingPathComponent("v1/sessions")
        try await transport.post(url, body: session)
    }

    // MARK: - Lifecycle hooks

    private func installLifecycleHooks() {
        #if canImport(UIKit) && !os(watchOS)
        let center = NotificationCenter.default
        let willResign = center.addObserver(forName: UIApplication.willResignActiveNotification, object: nil, queue: nil) { [weak self] _ in
            _ = self // batch sender will catch up on its next tick
        }
        lifecycleObservers.append(willResign)
        #endif
    }

    private func removeLifecycleHooks() {
        let center = NotificationCenter.default
        for obs in lifecycleObservers { center.removeObserver(obs) }
        lifecycleObservers.removeAll()
    }
}

// MARK: - Helpers

private func shortFile(_ path: String) -> String {
    if let slash = path.lastIndex(of: "/") {
        return String(path[path.index(after: slash)...])
    }
    return path
}

/// Lock-protected counter for sequence numbers.
final class AtomicCounter: @unchecked Sendable {
    private var value: Int64 = 0
    private let lock = NSLock()

    func increment() -> Int64 {
        lock.lock()
        value &+= 1
        let v = value
        lock.unlock()
        return v
    }
}
