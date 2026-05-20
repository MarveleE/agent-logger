import XCTest
@testable import AgentLogger

/// End-to-end test against a real `agentlog` server.
///
/// Skipped unless `AGENTLOGGER_E2E_ENDPOINT` is set (e.g. by the e2e shell
/// harness in `make test-client-e2e`).
final class EndToEndTests: XCTestCase {
    var endpoint: URL?

    override func setUp() {
        super.setUp()
        if let raw = ProcessInfo.processInfo.environment["AGENTLOGGER_E2E_ENDPOINT"],
           let url = URL(string: raw) {
            endpoint = url
        }
    }

    func testShipsLogsToRealServer() async throws {
        guard let endpoint else {
            throw XCTSkip("AGENTLOGGER_E2E_ENDPOINT not set")
        }

        // Bootstrap pointed at the live server.
        AgentLogger.bootstrap(.endpoint(endpoint))
        defer { Task { await AgentLogger.shutdown() } }

        let sessionID = try XCTUnwrap(AgentLogger.currentSessionID)

        // Emit assorted log entries.
        AgentLogger.info("integration-test:start", metadata: ["k": "v"])
        AgentLogger.debug("integration-test:debug")
        AgentLogger.error("integration-test:boom", metadata: ["where": "test"])

        // Give the batcher time to ship.
        try await Task.sleep(nanoseconds: 2_500_000_000)

        // Pull them back via the server's query endpoint.
        var comps = URLComponents(url: endpoint.appendingPathComponent("v1/logs"),
                                  resolvingAgainstBaseURL: false)!
        comps.queryItems = [
            URLQueryItem(name: "session", value: sessionID),
            URLQueryItem(name: "limit", value: "20"),
        ]
        let request = URLRequest(url: comps.url!)
        let (data, response) = try await fetch(request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)

        struct Resp: Decodable { let logs: [LogEntry] }
        let body = try JSONDecoder().decode(Resp.self, from: data)
        let messages = Set(body.logs.map { $0.message })

        if !messages.contains("integration-test:start") {
            print("DEBUG request URL: \(comps.url!.absoluteString)")
            print("DEBUG response body: \(String(data: data, encoding: .utf8) ?? "<binary>")")
            print("DEBUG session id used: \(sessionID)")
            // Curl the same URL to confirm whether it's a URLSession quirk
            let p = Process()
            p.executableURL = URL(fileURLWithPath: "/usr/bin/curl")
            p.arguments = ["-s", comps.url!.absoluteString]
            let pipe = Pipe(); p.standardOutput = pipe
            try? p.run(); p.waitUntilExit()
            let curlOut = String(data: pipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? "<n/a>"
            print("DEBUG curl response: \(curlOut)")
        }
        XCTAssertTrue(messages.contains("integration-test:start"))
        XCTAssertTrue(messages.contains("integration-test:debug"))
        XCTAssertTrue(messages.contains("integration-test:boom"))
    }

    private func fetch(_ request: URLRequest) async throws -> (Data, URLResponse) {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<(Data, URLResponse), Error>) in
            let task = URLSession.shared.dataTask(with: request) { data, response, error in
                if let error = error {
                    continuation.resume(throwing: error)
                    return
                }
                continuation.resume(returning: (data ?? Data(), response ?? URLResponse()))
            }
            task.resume()
        }
    }
}
