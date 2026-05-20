import Foundation

#if canImport(UIKit) && !os(watchOS)
import UIKit
#endif

/// Identifies a single launch of the application.
public struct SessionDescriptor: Codable, Sendable {
    public let id: String
    public let bundleId: String        // logical app id (iOS bundle id, Android applicationId, …)
    public let deviceId: String
    public let deviceName: String?
    public let deviceKind: String?     // open vocabulary: simulator | emulator | device | browser | desktop | server | container | embedded
    public let platform: String?       // ios | macos | tvos | watchos | android | web | node | linux | …
    public let osVersion: String?
    public let appVersion: String?     // semver-style (CFBundleShortVersionString)
    public let appBuild: String?       // build number (CFBundleVersion, Android versionCode, git SHA)
    public let startedAt: Int64        // unix ms
    public let metadata: [String: String]?

    public init(
        id: String,
        bundleId: String,
        deviceId: String,
        deviceName: String?,
        deviceKind: String?,
        platform: String?,
        osVersion: String?,
        appVersion: String?,
        appBuild: String?,
        startedAt: Int64,
        metadata: [String: String]?
    ) {
        self.id = id
        self.bundleId = bundleId
        self.deviceId = deviceId
        self.deviceName = deviceName
        self.deviceKind = deviceKind
        self.platform = platform
        self.osVersion = osVersion
        self.appVersion = appVersion
        self.appBuild = appBuild
        self.startedAt = startedAt
        self.metadata = metadata
    }
}

/// Snapshots host/app metadata at process start.
enum SessionFactory {
    static func makeDescriptor(extraMetadata: [String: String]) -> SessionDescriptor {
        let env = ProcessInfo.processInfo.environment
        let bundleId = Bundle.main.bundleIdentifier ?? "unknown.bundle"

        var metadata = extraMetadata
        if let buildConfig = Bundle.main.infoDictionary?["AGENTLOGGER_BUILD_CONFIG"] as? String {
            metadata["buildConfig"] = buildConfig
        }

        let isSimulator = env["SIMULATOR_UDID"] != nil
        let deviceId: String
        let deviceName: String?
        let osVersion: String?

        if isSimulator {
            deviceId = env["SIMULATOR_UDID"] ?? "simulator-unknown"
            deviceName = env["SIMULATOR_DEVICE_NAME"]
            osVersion = env["SIMULATOR_RUNTIME_VERSION"] ?? env["SIMULATOR_VERSION_INFO"]
        } else {
            #if canImport(UIKit) && !os(watchOS)
            deviceId = UIDevice.current.identifierForVendor?.uuidString ?? "device-unknown"
            deviceName = UIDevice.current.name
            osVersion = "\(UIDevice.current.systemName) \(UIDevice.current.systemVersion)"
            #else
            deviceId = ProcessInfo.processInfo.globallyUniqueString
            deviceName = Host.current().localizedName
            let v = ProcessInfo.processInfo.operatingSystemVersion
            osVersion = "macOS \(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
            #endif
        }

        let info = Bundle.main.infoDictionary
        let appVersion = info?["CFBundleShortVersionString"] as? String
        let appBuild = info?["CFBundleVersion"] as? String

        return SessionDescriptor(
            id: UUID().uuidString,
            bundleId: bundleId,
            deviceId: deviceId,
            deviceName: deviceName,
            deviceKind: isSimulator ? "simulator" : "device",
            platform: platformName(),
            osVersion: osVersion,
            appVersion: appVersion,
            appBuild: appBuild,
            startedAt: nowMillis(),
            metadata: metadata.isEmpty ? nil : metadata
        )
    }
}

@inlinable
func nowMillis() -> Int64 {
    Int64(Date().timeIntervalSince1970 * 1000)
}

/// Returns the value the SDK reports as `platform` on the wire. Determined at
/// compile time so it never lies about the actual runtime target.
@inlinable
func platformName() -> String {
    #if os(iOS)
    return "ios"
    #elseif os(macOS)
    return "macos"
    #elseif os(tvOS)
    return "tvos"
    #elseif os(watchOS)
    return "watchos"
    #elseif os(visionOS)
    return "visionos"
    #elseif os(Linux)
    return "linux"
    #else
    return "unknown"
    #endif
}
