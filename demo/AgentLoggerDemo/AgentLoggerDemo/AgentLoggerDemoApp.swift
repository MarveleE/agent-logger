import SwiftUI
import AgentLogger

@main
struct AgentLoggerDemoApp: App {
    init() {
        // One-line bootstrap. Auto-discovers:
        //   1. AGENTLOGGER_ENDPOINT env var (set via SIMCTL_CHILD_*)
        //   2. http://127.0.0.1:8765 if running in the simulator
        //   3. Bonjour _agentlogger._tcp on real devices (Phase 5)
        AgentLogger.bootstrap()
        AgentLogger.info("AgentLoggerDemo launched")
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
