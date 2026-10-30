import SwiftUI

/// The app's mark: the same artwork as the Home Screen icon (asset `AppMark`, the
/// icon's tile without its white margin), so onboarding, sign-in and Home show
/// the brand the person just tapped.
struct AppMarkView: View {

    var size: CGFloat = 64

    var body: some View {
        Image("AppMark")
            .resizable()
            .interpolation(.high)
            .frame(width: size, height: size)
            .clipShape(RoundedRectangle(cornerRadius: size * 0.225, style: .continuous))
            .accessibilityHidden(true)
    }
}

#Preview {
    VStack(spacing: 20) {
        AppMarkView(size: 120)
        AppMarkView(size: 64)
        AppMarkView(size: 32)
    }
    .padding()
}
