import Foundation

extension KeyProximity {

    /// The letter keys of `language`'s layout, exactly as the keyboard draws
    /// them - the Kazakh top row and the long-press letters included.
    init(layout language: KeyboardLanguage) {
        self.init(rows: KeyboardLayout.letters(for: language)) { key in
            KeyboardLayout.alternates(for: key, language: language)
        }
    }
}
