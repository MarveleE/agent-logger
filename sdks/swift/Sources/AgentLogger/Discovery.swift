import Foundation

/// Resolves the agentlog server endpoint at runtime.
///
/// Three explicit sources, no hidden defaults. The chain (highest precedence first):
///   1. **`.endpoint(URL)`** mode — the URL passed to `bootstrap`. Wins
///      unconditionally; never falls through to the auto chain.
///   2. **Info.plist** `AgentLoggerEndpoint` — compile-time pinning, the
///      canonical Xcode pattern (substituted from the build setting of the
///      same name).
///   3. **`AGENTLOGGER_ENDPOINT`** environment variable — runtime injection,
///      used by agents (via `SIMCTL_CHILD_AGENTLOGGER_ENDPOINT`) and CI.
///
/// If none of the above resolves, the SDK is **disabled** for this run: the
/// queue still accepts log calls but nothing is shipped. Discovery is retried
/// on a backoff; if the user later sets an endpoint at runtime they need to
/// call `AgentLogger.shutdown()` and `bootstrap()` again.
///
/// `resolve` probes `/v1/health` to confirm reachability before returning a URL.
enum Discovery {
    static let plistKey = "AgentLoggerEndpoint"
    static let envKey = "AGENTLOGGER_ENDPOINT"

    static func resolve(
        configuration: AgentLoggerConfiguration,
        timeout: TimeInterval
    ) async -> URL? {
        switch configuration.mode {
        case .disabled:
            return nil
        case .endpoint(let url):
            return await probe(url, timeout: timeout) ? url : nil
        case .auto:
            // 1. Info.plist (pinned at build time)
            if let plistURL = endpointFromInfoPlist() {
                if await probe(plistURL, timeout: timeout) {
                    return plistURL
                }
            }
            // 2. env var (runtime injection)
            if let envURL = endpointFromEnv() {
                if await probe(envURL, timeout: timeout) {
                    return envURL
                }
            }
            // Nothing configured → SDK is disabled for this attempt.
            return nil
        }
    }

    static func endpointFromInfoPlist() -> URL? {
        guard let raw = Bundle.main.infoDictionary?[plistKey] as? String, !raw.isEmpty else {
            return nil
        }
        return URL(string: raw)
    }

    static func endpointFromEnv() -> URL? {
        guard let raw = ProcessInfo.processInfo.environment[envKey], !raw.isEmpty else {
            return nil
        }
        return URL(string: raw)
    }

    /// One-shot reachability probe — `/v1/health` should return 200 quickly.
    static func probe(_ base: URL, timeout: TimeInterval) async -> Bool {
        let url = base.appendingPathComponent("v1/health")
        var request = URLRequest(url: url)
        request.timeoutInterval = timeout
        return await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
            let task = URLSession.shared.dataTask(with: request) { _, response, _ in
                let ok = (response as? HTTPURLResponse)?.statusCode == 200
                continuation.resume(returning: ok)
            }
            task.resume()
        }
    }
}
