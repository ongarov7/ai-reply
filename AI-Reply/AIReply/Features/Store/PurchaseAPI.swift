import Foundation

/// The server's side of a purchase (contract §3.8): what it sells, and the
/// App Store transaction it grants.
///
/// Сервер не сатады: өнім тізімі және Apple транзакциясын растау.
///
/// The server knows the products and how many replies each brings; it never
/// knows a price. Prices come from the App Store alone.
enum PurchaseAPI {

    /// A product an administrator configured and enabled for iOS.
    struct Product: Decodable, Equatable, Sendable {
        let code: String
        /// "pack" or "pass"; anything else is not offered by this build.
        let kind: String
        /// Replies it brings (a pass: within `durationHours`).
        let units: Int
        let durationHours: Int
        let storeProductID: String
        /// In the account's language.
        let name: String

        enum CodingKeys: String, CodingKey {
            case code, kind, units, name
            case durationHours = "duration_hours"
            case storeProductID = "store_product_id"
        }

        init(code: String, kind: String, units: Int, durationHours: Int, storeProductID: String, name: String) {
            self.code = code
            self.kind = kind
            self.units = units
            self.durationHours = durationHours
            self.storeProductID = storeProductID
            self.name = name
        }

        init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            code = try container.decode(String.self, forKey: .code)
            kind = try container.decode(String.self, forKey: .kind)
            units = try container.decode(Int.self, forKey: .units)
            durationHours = (try? container.decodeIfPresent(Int.self, forKey: .durationHours)) ?? 0
            storeProductID = try container.decodeIfPresent(String.self, forKey: .storeProductID) ?? ""
            name = (try? container.decodeIfPresent(String.self, forKey: .name)) ?? ""
        }
    }

    /// `GET /api/v1/iap/products?platform=ios`.
    struct Catalogue: Decodable, Equatable, Sendable {
        let products: [Product]
        /// The account's stable UUID, given to StoreKit as `appAccountToken`
        /// so the server can tell whose purchase it is.
        let accountToken: String

        enum CodingKeys: String, CodingKey {
            case products
            case accountToken = "account_token"
        }
    }

    /// `POST /api/v1/iap/apple/transactions`.
    struct Verdict: Decodable, Equatable, Sendable {
        /// granted | already_granted (Google's pending never comes for Apple).
        let status: String
        /// The new balance, when the server could read it.
        let usage: AccountAPI.Usage?

        var isGranted: Bool { status == "granted" || status == "already_granted" }
    }
}

/// What `PurchaseStore` needs from the server. Throws `APIError`.
protocol PurchaseBackend: Sendable {
    func catalogue() async throws -> PurchaseAPI.Catalogue
    func submit(signedTransaction: String) async throws -> PurchaseAPI.Verdict
}

/// The endpoints, with the session's usual refresh-once-and-retry.
struct BackendPurchaseService: PurchaseBackend {

    private let session: AccountSession

    init(session: AccountSession = .shared) {
        self.session = session
    }

    private static func client() throws -> APIClient {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else { throw APIError.invalidRequest }
        return APIClient(baseURL: baseURL)
    }

    func catalogue() async throws -> PurchaseAPI.Catalogue {
        try await session.authenticated { token in
            try await Self.client().get("api/v1/iap/products", query: [URLQueryItem(name: "platform", value: "ios")],
                                        token: token)
        }
    }

    func submit(signedTransaction: String) async throws -> PurchaseAPI.Verdict {
        struct Request: Encodable, Sendable { let signed_transaction: String }
        let request = Request(signed_transaction: signedTransaction)
        return try await session.authenticated { token in
            try await Self.client().post("api/v1/iap/apple/transactions", body: request, token: token)
        }
    }
}
