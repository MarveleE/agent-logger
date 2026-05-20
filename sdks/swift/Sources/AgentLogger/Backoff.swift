import Foundation

/// Exponential backoff with a hard ceiling.
///
/// Sequence: 1s, 2s, 4s, 8s, 16s, 30s (capped). Reset on success.
struct Backoff: Sendable {
    private(set) var attempts: Int = 0
    let base: TimeInterval = 1
    let cap: TimeInterval = 30

    mutating func next() -> TimeInterval {
        let raw = base * pow(2.0, Double(attempts))
        attempts += 1
        return min(raw, cap)
    }

    mutating func reset() {
        attempts = 0
    }
}

@inlinable
func sleepSeconds(_ seconds: TimeInterval) async {
    if seconds <= 0 { return }
    let ns = UInt64(seconds * 1_000_000_000)
    try? await Task.sleep(nanoseconds: ns)
}
