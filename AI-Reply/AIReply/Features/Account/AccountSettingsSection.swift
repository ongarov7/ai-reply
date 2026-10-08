import SwiftUI

/// The account block inside Settings.
///
/// Баптаулардағы тіркелгі бөлімі: тариф, квота, шығу.
///
/// Shows what the user needs to recognise their account and nothing more: the
/// address (or provider) it signs in with, the current plan, what is left
/// today, and the ways out - signing out, or deleting the account. An account
/// opened with a phone number is offered to add an e-mail, since phone
/// sign-in is no longer available.
struct AccountSettingsSection: View {

    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account

    @State private var isAddingEmail = false

    var body: some View {
        Section {
            if account.isSignedIn {
                LabeledContent {
                    Text(verbatim: account.displayIdentifier)
                        .foregroundStyle(.secondary)
                } label: {
                    Label("settings.account.identifier", systemImage: "person.crop.circle")
                }

                if account.user?.needsEmail == true {
                    Button {
                        isAddingEmail = true
                    } label: {
                        Label("settings.account.addEmail", systemImage: "envelope.badge")
                    }
                    .sheet(isPresented: $isAddingEmail) { LinkEmailView() }
                }

                NavigationLink {
                    SubscriptionView()
                } label: {
                    LabeledContent {
                        Text(verbatim: planSummary)
                            .foregroundStyle(.secondary)
                    } label: {
                        Label("settings.account.plan", systemImage: "gauge.with.dots.needle.bottom.50percent")
                    }
                }

                AccountExitActions()
            } else {
                Label("settings.account.signedOut", systemImage: "person.crop.circle.badge.questionmark")
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("settings.account")
        } footer: {
            if account.isSignedIn, account.user?.needsEmail == true {
                Text("settings.account.addEmail.footer")
            } else {
                Text("settings.account.footer")
            }
        }
        .task { await account.refresh() }
    }

    private var planSummary: String {
        guard let plan = account.subscription?.plan else { return "" }
        let name = plan.localizedName(settings.effectiveLanguage.rawValue)
        guard account.usage.dailyLimit > 0 else { return name }
        return "\(name) · \(account.usage.remainingToday)/\(account.usage.dailyLimit)"
    }
}

/// Sign out and Delete account, each behind its confirmation.
///
/// Шығу және тіркелгіні жою: баптауларда да, келісім экранында да.
///
/// Settings shows them in the account section. The consent screen shows them
/// to a signed-in user too, so nobody has to agree to anything to sign out
/// or to delete the account.
struct AccountExitActions: View {

    @Environment(AccountModel.self) private var account

    @State private var isConfirmingSignOut = false
    @State private var isConfirmingDeletion = false
    @State private var isDeleting = false
    /// Why the deletion did not happen, shown in an alert.
    @State private var deletionFailureKey: String?

    var body: some View {
        Button(role: .destructive) {
            isConfirmingSignOut = true
        } label: {
            Label("settings.account.signOut", systemImage: "rectangle.portrait.and.arrow.right")
        }
        .confirmationDialog("settings.account.signOut.confirm",
                            isPresented: $isConfirmingSignOut, titleVisibility: .visible) {
            Button("settings.account.signOut", role: .destructive) {
                Task { await account.signOut() }
            }
            Button("common.cancel", role: .cancel) {}
        }

        Button(role: .destructive) {
            isConfirmingDeletion = true
        } label: {
            HStack {
                Label("settings.account.delete", systemImage: "trash")
                if isDeleting {
                    Spacer()
                    ProgressView()
                }
            }
        }
        .disabled(isDeleting)
        .confirmationDialog("settings.account.delete.confirm",
                            isPresented: $isConfirmingDeletion, titleVisibility: .visible) {
            Button("settings.account.delete", role: .destructive) {
                Task {
                    isDeleting = true
                    let outcome = await account.deleteAccount()
                    isDeleting = false
                    if case let .failed(key) = outcome { deletionFailureKey = key }
                }
            }
            Button("common.cancel", role: .cancel) {}
        } message: {
            Text("settings.account.delete.body")
        }
        .alert(Text(LocalizedStringKey(deletionFailureKey ?? "account.delete.failed")),
               isPresented: deletionFailureBinding) {
            Button("common.done", role: .cancel) {}
        }
    }

    private var deletionFailureBinding: Binding<Bool> {
        Binding(get: { deletionFailureKey != nil }, set: { if !$0 { deletionFailureKey = nil } })
    }
}
