import Foundation
import Security

/// Small secrets (session tokens) by key.
public protocol SecureStore: Sendable {
    func read(_ key: String) throws -> Data?
    func write(_ data: Data, for key: String) throws
    func delete(_ key: String) throws
}

public struct SecureStoreError: Error, Equatable, Sendable {
    public let status: OSStatus
    public init(status: OSStatus) { self.status = status }
}

/// The login keychain, as generic passwords of this app only. Items stay on
/// this Mac (no iCloud sync) and are readable after the first unlock.
public struct KeychainStore: SecureStore {
    private let service: String

    public init(service: String) {
        self.service = service
    }

    private func query(_ key: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecAttrSynchronizable as String: false,
        ]
    }

    public func read(_ key: String) throws -> Data? {
        var request = query(key)
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(request as CFDictionary, &result)
        switch status {
        case errSecSuccess: return result as? Data
        case errSecItemNotFound: return nil
        default: throw SecureStoreError(status: status)
        }
    }

    public func write(_ data: Data, for key: String) throws {
        let update = SecItemUpdate(query(key) as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if update == errSecSuccess { return }
        guard update == errSecItemNotFound else { throw SecureStoreError(status: update) }
        var item = query(key)
        item[kSecValueData as String] = data
        item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let status = SecItemAdd(item as CFDictionary, nil)
        guard status == errSecSuccess else { throw SecureStoreError(status: status) }
    }

    public func delete(_ key: String) throws {
        let status = SecItemDelete(query(key) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw SecureStoreError(status: status) }
    }
}

/// For tests and previews.
public final class InMemorySecureStore: SecureStore, @unchecked Sendable {
    private let lock = NSLock()
    private var items: [String: Data] = [:]

    public init() {}

    public func read(_ key: String) throws -> Data? {
        lock.withLock { items[key] }
    }

    public func write(_ data: Data, for key: String) throws {
        lock.withLock { items[key] = data }
    }

    public func delete(_ key: String) throws {
        _ = lock.withLock { items.removeValue(forKey: key) }
    }
}
