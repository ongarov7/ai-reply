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
        var planID: String? = nil
        var billingPeriod: String? = nil
        var entitlementKind: String? = nil
        var dailyLimit = 0
        var price: Int? = nil
        var currency: String? = nil

        enum CodingKeys: String, CodingKey {
            case code, kind, units, name
            case durationHours = "duration_hours"
            case storeProductID = "store_product_id"
            case planID = "plan_id"
            case billingPeriod = "billing_period"
            case entitlementKind = "entitlement_kind"
            case dailyLimit = "daily_limit"
            case price, currency
        }

        init(code: String, kind: String, units: Int, durationHours: Int, storeProductID: String, name: String,
             planID: String? = nil, billingPeriod: String? = nil, entitlementKind: String? = nil,
             dailyLimit: Int = 0, price: Int? = nil, currency: String? = nil) {
            self.code = code
            self.kind = kind
            self.units = units
            self.durationHours = durationHours
            self.storeProductID = storeProductID
            self.name = name
            self.planID = planID
            self.billingPeriod = billingPeriod
            self.entitlementKind = entitlementKind
            self.dailyLimit = dailyLimit
            self.price = price
            self.currency = currency
        }

        init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            code = try container.decode(String.self, forKey: .code)
            kind = try container.decode(String.self, forKey: .kind)
            units = try container.decode(Int.self, forKey: .units)
            durationHours = (try? container.decodeIfPresent(Int.self, forKey: .durationHours)) ?? 0
            storeProductID = try container.decodeIfPresent(String.self, forKey: .storeProductID) ?? ""
            name = (try? container.decodeIfPresent(String.self, forKey: .name)) ?? ""
            planID = try? container.decodeIfPresent(String.self, forKey: .planID)
            billingPeriod = try? container.decodeIfPresent(String.self, forKey: .billingPeriod)
            entitlementKind = try? container.decodeIfPresent(String.self, forKey: .entitlementKind)
            dailyLimit = (try? container.decodeIfPresent(Int.self, forKey: .dailyLimit)) ?? 0
            price = try? container.decodeIfPresent(Int.self, forKey: .price)
            currency = try? container.decodeIfPresent(String.self, forKey: .currency)
        }
    }

    /// `GET /api/v1/iap/products?platform=ios`.
    struct Catalogue: Decodable, Equatable, Sendable {
        let products: [Product]
        /// The account's stable UUID, given to StoreKit as `appAccountToken`
        /// so the server can tell whose purchase it is.
        let accountToken: String
        var recommendation: Recommendation? = nil

        enum CodingKeys: String, CodingKey {
            case products
            case accountToken = "account_token"
            case recommendation
        }
    }

    struct Recommendation: Decodable, Equatable, Sendable {
        let id: String
        let purchaseCount: Int
        let spentMinor: Int
        let currency: String
        let basePriceMinor: Int
        let baseStoreProductID: String

        enum CodingKeys: String, CodingKey {
            case id, currency
            case purchaseCount = "purchase_count"
            case spentMinor = "spent_minor"
            case basePriceMinor = "base_price_minor"
            case baseStoreProductID = "base_store_product_id"
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

extension PurchaseAPI.Catalogue {
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        products = try container.decode([PurchaseAPI.Product].self, forKey: .products)
        accountToken = try container.decode(String.self, forKey: .accountToken)
        recommendation = try? container.decodeIfPresent(PurchaseAPI.Recommendation.self, forKey: .recommendation)
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
