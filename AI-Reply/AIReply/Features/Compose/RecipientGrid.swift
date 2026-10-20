import SwiftUI

/// «Кому отвечаем?»: the built-in recipients as a two-by-two grid of cards,
/// each with its SF Symbol and its name in the dative (Другу, Клиенту,
/// Коллеге, Партнёру).
///
/// «Кімге жауап береміз?»: төрт карточка, белгішесімен; таңдалғаны көк.
///
/// The selected card is filled with the accent and its text turns white;
/// the others are plain white cards. One column at accessibility text sizes,
/// so a long name never squeezes.
struct RecipientGrid: View {

    struct Item: Identifiable, Equatable {
        let id: String
        let symbol: String
        let name: String
    }

    let items: [Item]
    let selection: String?
    let onSelect: (String) -> Void

    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        LazyVGrid(columns: columns, spacing: DS.Spacing.s) {
            ForEach(items) { item in
                RecipientCard(item: item, isSelected: item.id == selection) { onSelect(item.id) }
            }
        }
    }

    private var columns: [GridItem] {
        let column = GridItem(.flexible(), spacing: DS.Spacing.s)
        return typeSize.isAccessibilitySize ? [column] : [column, column]
    }

    /// The built-ins among `templates`, in their order, as the grid shows
    /// them. A renamed built-in shows the user's own name, undeclined.
    static func items(for templates: [ReplyTemplate], language: AppLanguage) -> [Item] {
        templates.filter(\.isBuiltIn).map { template in
            Item(id: template.id, symbol: template.relationship.symbolName,
                 name: template.recipientName(appLanguage: language))
        }
    }

    /// The four built-in kinds in `order`, with their own names: the
    /// tutorial's grid, which never shows a user's renaming.
    static func items(kinds order: [RelationshipKind], language: AppLanguage) -> [Item] {
        order.map { kind in
            Item(id: kind.rawValue, symbol: kind.symbolName,
                 name: ReplyTemplate.builtIn(kind, sortIndex: 0).recipientName(appLanguage: language))
        }
    }
}

private struct RecipientCard: View {
    let item: RecipientGrid.Item
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: DS.Spacing.s) {
                Image(systemName: item.symbol)
                    .font(.body.weight(.medium))
                    .foregroundStyle(isSelected ? Color.white : Color.accentColor)
                    .frame(width: 24)
                    .accessibilityHidden(true)
                Text(verbatim: item.name)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(isSelected ? Color.white : Color.primary)
                    .lineLimit(2)
                    .minimumScaleFactor(0.85)
                    .multilineTextAlignment(.leading)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, DS.Spacing.m)
            .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                    .fill(isSelected ? Color.accentColor : Color.dsSurface)
            )
            .contentShape(RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? [.isSelected] : [])
    }
}
