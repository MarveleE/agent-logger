import Foundation

/// Thin async wrapper around URLSession that works on iOS 13+.
/// We can't use `URLSession.data(for:)` directly because it is iOS 15+ and is
/// NOT back-deployed via the Swift Concurrency shim.
struct HTTPTransport: Sendable {
    let session: URLSession
    let timeout: TimeInterval

    init(timeout: TimeInterval) {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.timeoutIntervalForRequest = timeout
        cfg.timeoutIntervalForResource = timeout
        cfg.waitsForConnectivity = false
        cfg.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        self.session = URLSession(configuration: cfg)
        self.timeout = timeout
    }

    enum TransportError: Error {
        case nonSuccess(status: Int, body: String)
        case noResponse
        case underlying(Error)
    }

    @discardableResult
    func post<Body: Encodable>(_ url: URL, body: Body) async throws -> (Data, HTTPURLResponse) {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder.agentlog.encode(body)
        request.timeoutInterval = timeout
        return try await perform(request: request)
    }

    @discardableResult
    func put(_ url: URL) async throws -> (Data, HTTPURLResponse) {
        var request = URLRequest(url: url)
        request.httpMethod = "PUT"
        request.timeoutInterval = timeout
        return try await perform(request: request)
    }

    @discardableResult
    func get(_ url: URL) async throws -> (Data, HTTPURLResponse) {
        var request = URLRequest(url: url)
        request.timeoutInterval = timeout
        return try await perform(request: request)
    }

    private func perform(request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<(Data, HTTPURLResponse), Error>) in
            let task = session.dataTask(with: request) { data, response, error in
                if let error = error {
                    continuation.resume(throwing: TransportError.underlying(error))
                    return
                }
                guard let http = response as? HTTPURLResponse else {
                    continuation.resume(throwing: TransportError.noResponse)
                    return
                }
                let body = data ?? Data()
                if !(200..<300).contains(http.statusCode) {
                    let text = String(data: body, encoding: .utf8) ?? ""
                    continuation.resume(throwing: TransportError.nonSuccess(status: http.statusCode, body: text))
                    return
                }
                continuation.resume(returning: (body, http))
            }
            task.resume()
        }
    }
}

extension JSONEncoder {
    /// Shared encoder used across the SDK.
    static let agentlog: JSONEncoder = {
        let e = JSONEncoder()
        return e
    }()
}

extension JSONDecoder {
    static let agentlog: JSONDecoder = {
        let d = JSONDecoder()
        return d
    }()
}
