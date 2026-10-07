import SwiftUI

/// «Рад» or «рада»: how replies written in the user's name speak for them in
/// Russian. Asked once, never guessed, and skippable - Skip means neutral
/// wording.
struct OnboardingGenderStep: View {

    @Binding var selection: GrammaticalGender?

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            OnboardingStepTitle("onboarding.gender.title", "onboarding.gender.body")

            VStack(spacing: DS.Spacing.s) {
                GenderChoiceCard(
                    title: "onboarding.gender.male",
                    example: "onboarding.gender.male.example",
                    isSelected: selection == .male
                ) { selection = .male }
                GenderChoiceCard(
                    title: "onboarding.gender.female",
                    example: "onboarding.gender.female.example",
                    isSelected: selection == .female
                ) { selection = .female }
            }

            Text("onboarding.gender.footer")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }
}

private struct GenderChoiceCard: View {
    let title: LocalizedStringKey
    let example: LocalizedStringKey
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: DS.Spacing.s) {
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Text(title)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.primary)
                    Text(example)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
                Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                    .font(.title3)
                    .foregroundStyle(isSelected ? Color.accentColor : Color.secondary)
                    .accessibilityHidden(true)
            }
            .padding(DS.Spacing.m)
            .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .fill(isSelected ? Color.accentColor.opacity(0.12) : Color.dsSurface)
            )
            .overlay(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .stroke(isSelected ? Color.accentColor : Color.clear, lineWidth: 1.5)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }
}
