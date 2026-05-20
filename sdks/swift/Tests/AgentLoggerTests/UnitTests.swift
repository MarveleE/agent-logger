import XCTest
@testable import AgentLogger

final class LogLevelTests: XCTestCase {
    func testEncodingMatchesServer() throws {
        let json = try JSONEncoder().encode(LogLevel.error)
        XCTAssertEqual(String(data: json, encoding: .utf8), #""error""#)
    }

    func testDecodesNamedAndNumericForms() throws {
        XCTAssertEqual(try JSONDecoder().decode(LogLevel.self, from: Data(#""warning""#.utf8)), .warning)
        XCTAssertEqual(try JSONDecoder().decode(LogLevel.self, from: Data(#""warn""#.utf8)), .warning)
        XCTAssertEqual(try JSONDecoder().decode(LogLevel.self, from: Data("5".utf8)), .error)
    }

    func testOrderingMatchesSeverity() {
        XCTAssertLessThan(LogLevel.trace.rawValue, LogLevel.error.rawValue)
        XCTAssertGreaterThan(LogLevel.critical.rawValue, LogLevel.info.rawValue)
    }
}

final class LogEntryTests: XCTestCase {
    func testEncodesFunctionAsFuncKey() throws {
        let entry = LogEntry(
            seq: 1, ts: 100, level: .info,
            category: "Net", message: "hi",
            metadata: ["k": "v"], file: "Foo.swift", line: 12, function: "doThing()"
        )
        let data = try JSONEncoder().encode(entry)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(json["func"] as? String, "doThing()")
        XCTAssertEqual(json["category"] as? String, "Net")
        XCTAssertEqual(json["seq"] as? Int, 1)
    }
}

final class BackoffTests: XCTestCase {
    func testExponentialUntilCap() {
        var b = Backoff()
        XCTAssertEqual(b.next(), 1)
        XCTAssertEqual(b.next(), 2)
        XCTAssertEqual(b.next(), 4)
        XCTAssertEqual(b.next(), 8)
        XCTAssertEqual(b.next(), 16)
        XCTAssertEqual(b.next(), 30)  // capped
        XCTAssertEqual(b.next(), 30)
    }

    func testResetGoesBackToOne() {
        var b = Backoff()
        _ = b.next(); _ = b.next()
        b.reset()
        XCTAssertEqual(b.next(), 1)
    }
}

final class LogQueueTests: XCTestCase {
    func testRespectsCapacity() async {
        let q = LogQueue(capacity: 3)
        for i in 1...5 {
            q.send(LogEntry(seq: Int64(i), ts: 0, level: .info, category: nil,
                            message: "m\(i)", metadata: nil, file: nil, line: nil, function: nil))
        }
        // 5 sent, capacity 3 → 2 dropped, 3 buffered (the newest)
        XCTAssertEqual(q.consumeDroppedCount(), 2)

        var collected: [LogEntry] = []
        var iter = q.stream.makeAsyncIterator()
        while let next = await iter.next() {
            collected.append(next)
            if collected.count == 3 { break }
        }
        XCTAssertEqual(collected.map { $0.seq }, [3, 4, 5])
    }
}

final class DiscoveryTests: XCTestCase {
    func testEndpointFromEnvReadsURL() throws {
        setenv("AGENTLOGGER_ENDPOINT", "http://test.local:9999", 1)
        defer { unsetenv("AGENTLOGGER_ENDPOINT") }
        XCTAssertEqual(Discovery.endpointFromEnv(), URL(string: "http://test.local:9999"))
    }

    func testEndpointFromEnvNilWhenUnset() {
        unsetenv("AGENTLOGGER_ENDPOINT")
        XCTAssertNil(Discovery.endpointFromEnv())
    }
}

final class SessionFactoryTests: XCTestCase {
    func testBundleIDIsCaptured() {
        let s = SessionFactory.makeDescriptor(extraMetadata: ["k": "v"])
        XCTAssertFalse(s.bundleId.isEmpty)
        XCTAssertFalse(s.deviceId.isEmpty)
        XCTAssertEqual(s.metadata?["k"], "v")
    }

    func testPlatformReportsExpectedValue() {
        let s = SessionFactory.makeDescriptor(extraMetadata: [:])
        #if os(macOS)
        XCTAssertEqual(s.platform, "macos")
        #elseif os(iOS)
        XCTAssertEqual(s.platform, "ios")
        #elseif os(tvOS)
        XCTAssertEqual(s.platform, "tvos")
        #elseif os(watchOS)
        XCTAssertEqual(s.platform, "watchos")
        #endif
    }

    func testSessionEncodesOptionalFieldsCorrectly() throws {
        let sess = SessionDescriptor(
            id: "id", bundleId: "b", deviceId: "d", deviceName: nil,
            deviceKind: "device", platform: "ios",
            osVersion: nil, appVersion: "1.2.3", appBuild: "456",
            startedAt: 0, metadata: nil
        )
        let data = try JSONEncoder().encode(sess)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(json["platform"] as? String, "ios")
        XCTAssertEqual(json["appBuild"] as? String, "456")
        XCTAssertEqual(json["appVersion"] as? String, "1.2.3")
        // Optional nils are omitted entirely.
        XCTAssertNil(json["deviceName"])
        XCTAssertNil(json["osVersion"])
    }
}

final class AgentLoggerFacadeTests: XCTestCase {
    func testDisabledModeIsNoop() async {
        AgentLogger.bootstrap(.disabled)
        AgentLogger.info("nobody hears me")
        await AgentLogger.shutdown()
    }
}
