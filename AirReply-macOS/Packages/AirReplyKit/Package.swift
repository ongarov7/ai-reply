// swift-tools-version: 6.0
import PackageDescription

// Everything testable lives here; the Xcode app target only wires it up.
// AirReplyCore   — API contract, auth session, keychain, limits. No UI.
// AirReplyVoice  — microphone capture and server transcription.
// AirReplySystem — global hotkey, clipboard, paste, login item.
// AirReplyUI     — the quick bar, panel, menu bar item and settings.
let package = Package(
    name: "AirReplyKit",
    defaultLocalization: "ru",
    platforms: [.macOS(.v14)],
    products: [
        .library(name: "AirReplyKit", targets: ["AirReplyCore", "AirReplyVoice", "AirReplySystem", "AirReplyUI"]),
    ],
    targets: [
        .target(name: "AirReplyCore"),
        .target(name: "AirReplyVoice", dependencies: ["AirReplyCore"]),
        .target(
            name: "AirReplySystem",
            dependencies: ["AirReplyCore"],
            linkerSettings: [.linkedFramework("Carbon"), .linkedFramework("ServiceManagement")]
        ),
        .target(name: "AirReplyUI", dependencies: ["AirReplyCore", "AirReplyVoice", "AirReplySystem"]),
        .testTarget(name: "AirReplyCoreTests", dependencies: ["AirReplyCore"]),
        .testTarget(name: "AirReplyVoiceTests", dependencies: ["AirReplyVoice"]),
        .testTarget(name: "AirReplySystemTests", dependencies: ["AirReplySystem"]),
        .testTarget(name: "AirReplyUITests", dependencies: ["AirReplyUI"]),
    ],
    swiftLanguageModes: [.v6]
)
