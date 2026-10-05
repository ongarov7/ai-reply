import Foundation

/// How a reply written in the user's name refers to the user, in a language
/// whose words change with the speaker's gender - in practice Russian:
/// «рад» or «рада», «сделал» or «сделала».
///
/// Жынысты тек пайдаланушы өзі таңдайды: аты, поштасы, аккаунты немесе
/// хат алмасуы бойынша ешқашан болжанбайды.
///
/// On `UserProfile` the value is optional: `nil` means the user was never
/// asked, `.unspecified` means they skipped or declined, and replies then avoid
/// gendered self-forms altogether. The raw values are the wire values of
/// `grammatical_gender`.
enum GrammaticalGender: String, Codable, CaseIterable, Sendable {
    case male
    case female
    case unspecified
}
