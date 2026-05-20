import SwiftUI
import AgentLogger

struct ContentView: View {
    @State private var burstCount: Int = 100
    @State private var lastAction: String = "(no logs sent yet)"

    private let network = AgentLogger.category("Network")
    private let ui = AgentLogger.category("UI")

    var body: some View {
        NavigationView {
            Form {
                Section(header: Text("Quick log")) {
                    Button("Info") { send(.info, "user tapped Info") }
                    Button("Debug") { send(.debug, "user tapped Debug") }
                    Button("Warning") { send(.warning, "user tapped Warning") }
                    Button("Error (with metadata)") {
                        AgentLogger.error(
                            "user triggered fake error",
                            metadata: ["component": "ContentView", "screen": "main"]
                        )
                        lastAction = "error logged"
                    }
                    Button("Critical") { send(.critical, "user tapped Critical") }
                }

                Section(header: Text("Category loggers")) {
                    Button("Network info") {
                        network.info("simulated GET /users", metadata: ["status": "200"])
                        lastAction = "network.info"
                    }
                    Button("UI debug") {
                        ui.debug("layout pass complete")
                        lastAction = "ui.debug"
                    }
                }

                Section(header: Text("Burst test")) {
                    Stepper("Count: \(burstCount)", value: $burstCount, in: 1...10_000, step: 100)
                    Button("Send \(burstCount) entries") {
                        for i in 1...burstCount {
                            AgentLogger.debug("burst entry \(i)/\(burstCount)")
                        }
                        lastAction = "burst of \(burstCount)"
                    }
                }

                Section(header: Text("Session")) {
                    Text("Session ID: \(AgentLogger.currentSessionID ?? "—")")
                        .font(.system(.caption, design: .monospaced))
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Text("Last action: \(lastAction)").font(.footnote)
                }
            }
            .navigationTitle("AgentLogger Demo")
        }
    }

    private func send(_ level: SendLevel, _ message: String) {
        switch level {
        case .trace: AgentLogger.trace(message)
        case .debug: AgentLogger.debug(message)
        case .info: AgentLogger.info(message)
        case .warning: AgentLogger.warning(message)
        case .critical: AgentLogger.critical(message)
        }
        lastAction = "\(level) logged"
    }
}

private enum SendLevel { case trace, debug, info, warning, critical }
