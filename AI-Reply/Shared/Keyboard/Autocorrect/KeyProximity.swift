import Foundation

/// Which letter keys touch each other on a layout - a slip of the finger onto
/// a neighbour is the cheapest typo there is.
///
/// Built from plain rows of key captions so the engine does not depend on
/// UIKit; `KeyProximity(layout:)` feeds it the keyboard's own rows. Every key
/// is one unit wide and each row is centred on the widest one, which is how
/// `KeyboardLayout` lays them out: QWERTY's middle row sits half a key in, its
/// bottom row a key and a half; the Cyrillic bottom row and the Kazakh top row
/// sit one key in on the eleven-column grid.
///
/// Two keys are neighbours when they are next to each other on a row, or on
/// adjacent rows with their centres at most one key apart (they overlap or
/// share an edge). A long-press letter (`ё` behind `е`, `ъ` behind `ь`) sits
/// on its key, exactly as on the Android keyboard.
struct KeyProximity: Sendable {

    private let neighbours: [String: Set<String>]

    /// - Parameter alternates: the long-press characters of a key; the
    ///   single letters among them share the key's place.
    init(rows: [[String]], alternates: (String) -> [String] = { _ in [] }) {
        let width = Double(rows.map(\.count).max() ?? 0)
        var centres: [(key: String, row: Int, x: Double)] = []
        for (row, keys) in rows.enumerated() {
            let inset = (width - Double(keys.count)) / 2
            for (index, key) in keys.enumerated() {
                centres.append((key, row, inset + Double(index)))
            }
        }
        let placed = Set(centres.map(\.key))
        for centre in centres {
            for alternate in alternates(centre.key) where alternate.count == 1 && alternate.first?.isLetter == true && !placed.contains(alternate) {
                centres.append((alternate, centre.row, centre.x))
            }
        }
        var neighbours: [String: Set<String>] = [:]
        for a in centres {
            for b in centres where a.key != b.key && abs(a.row - b.row) <= 1 && abs(a.x - b.x) <= 1 {
                neighbours[a.key, default: []].insert(b.key)
            }
        }
        self.neighbours = neighbours
    }

    func areAdjacent(_ a: String, _ b: String) -> Bool {
        neighbours[a]?.contains(b) ?? false
    }
}
